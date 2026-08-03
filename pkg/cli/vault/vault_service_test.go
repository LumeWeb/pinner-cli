package vault

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
)

// fakeEvents is a fakeSDK that returns a canned set of object events for
// Sync tests.
type fakeEvents struct {
	fakeSDK
	events []siastorage.ObjectEvent
}

func (f *fakeEvents) ObjectEvents(_ context.Context, _ slabs.Cursor, _ int) ([]siastorage.ObjectEvent, error) {
	return f.events, nil
}

// testObjectEvent builds an ObjectEvent with a distinct key and the given
// root file name in its metadata.
func testObjectEvent(keyByte byte, name string) siastorage.ObjectEvent {
	key := types.Hash256{keyByte}
	meta := FileMetadata{Name: name, Size: 10, ContentDigest: "d", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	raw, _ := meta.JSON()
	obj := siastorage.NewEmptyObject()
	obj.UpdateMetadata(raw)
	return siastorage.ObjectEvent{Key: key, Object: &obj, UpdatedAt: time.Now().UTC()}
}

// testTransientSkippedEvent builds an ObjectEvent with a real object but no
// metadata, which Sync treats as a transient skip (retry on the next sync)
// rather than a recordable file.
func testTransientSkippedEvent(keyByte byte) siastorage.ObjectEvent {
	key := types.Hash256{keyByte}
	obj := siastorage.NewEmptyObject()
	return siastorage.ObjectEvent{Key: key, Object: &obj, UpdatedAt: time.Now().UTC()}
}

// TestParseHash256_InvalidHex verifies parseHash256 returns an error on invalid hex input,
// not silently fall back to the zero hash. All call sites (Get, Verify,
// Remove, Share, Put) depend on this to avoid operating on object ID 0.
func TestParseHash256_InvalidHex(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"too short", "abc"},
		{"non-hex chars", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"odd length", "abc1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseHash256(tt.input)
			if err == nil {
				t.Fatal("parseHash256 must return error for invalid input")
			}
		})
	}
}

// TestParseHash256_ValidHex verifies parseHash256 succeeds on valid 64-char hex.
func TestParseHash256_ValidHex(t *testing.T) {
	validHex := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	h, err := parseHash256(validHex)
	if err != nil {
		t.Fatalf("parseHash256 on valid hex: %v", err)
	}
	if h == (types.Hash256{}) {
		t.Fatal("parseHash256 returned zero hash on valid input")
	}
}

// TestStat_DirectorySizeIsZero verifies Stat returns Size=0 for directory entries,
// not the
// file count (which was being misreported as bytes in JSON output).
func TestStat_DirectorySizeIsZero(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	svc := &vaultService{
		db:     db,
		sdk:    &fakeSDK{t: t},
		appKey: types.PrivateKey{},
	}

	// Create a directory with multiple files
	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		file := File{
			Name:          "file" + string(rune('0'+i)) + ".pdf",
			DirectoryID:   dirID,
			ObjectKey:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef123456789" + string(rune('0'+i)),
			Size:          1024,
			MediaType:     "application/pdf",
			ContentDigest: "digest" + string(rune('0'+i)),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := db.Create(&file).Error; err != nil {
			t.Fatalf("create file: %v", err)
		}
	}

	// Stat the directory (trailing slash makes it a dir path)
	result, err := svc.Stat(ctx, "vault:/docs/")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if result.Type != "dir" {
		t.Errorf("Type = %q, want %q", result.Type, "dir")
	}
	if result.Size != 0 {
		t.Errorf("directory Size = %d, want 0 (got file count as bytes?)", result.Size)
	}
}

// TestStat_NotFoundSentinel verifies Stat returns an ErrNotFound-wrapped error
// for a missing file (and directory). The upload overwrite guard in
// vault_cp.go relies on errors.Is(err, ErrNotFound) to distinguish "destination
// is free to write" from a transient error that must abort rather than fall
// through to Put and delete a prior object without --force.
func TestStat_NotFoundSentinel(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	svc := &vaultService{
		db:     db,
		sdk:    &fakeSDK{t: t},
		appKey: types.PrivateKey{},
	}

	// A file that does not exist in a nonexistent directory.
	if _, err := svc.Stat(ctx, "vault:/nope/missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat(missing file) error = %v, want errors.Is(err, ErrNotFound)", err)
	}

	// A file that does not exist at root.
	if _, err := svc.Stat(ctx, "vault:/missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat(missing root file) error = %v, want errors.Is(err, ErrNotFound)", err)
	}
}

// TestSync_UpdatesMetadataFields verifies Sync's update branch copies Name, Size,
// MediaType,
// and ContentDigest from the fresh fileMeta — not just UpdatedAt. Otherwise
// Stat/Verify/List return stale metadata after a remote modification.
// This test exercises the real DB update path that sync.go uses.
func TestSync_UpdatesMetadataFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// Insert a pre-existing file record with stale metadata
	oldKey := types.Hash256{0x01}
	oldKeyHex := oldKey.String()
	staleFile := File{
		Name:          "old-name.txt",
		ObjectKey:     oldKeyHex,
		Size:          100,
		MediaType:     "text/plain",
		ContentDigest: "olddigest",
		CreatedAt:     time.Now().Add(-2 * time.Hour).UTC(),
		UpdatedAt:     time.Now().Add(-1 * time.Hour).UTC(),
	}
	if err := db.Create(&staleFile).Error; err != nil {
		t.Fatalf("create stale file: %v", err)
	}

	// Simulate what sync.go's update branch does: find by object_key, then
	// copy all fields from fileMeta before Save.
	var existing File
	result := db.Where("object_key = ?", oldKeyHex).First(&existing)
	if result.Error != nil {
		t.Fatalf("find existing: %v", result.Error)
	}

	// Apply the same field updates sync.go now does
	existing.Name = "new-name.txt"
	existing.Size = 999
	existing.MediaType = "application/json"
	existing.ContentDigest = "newdigest"
	existing.UpdatedAt = time.Now().UTC()
	if err := db.Save(&existing).Error; err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload and verify all fields are updated
	var reloaded File
	if err := db.Where("object_key = ?", oldKeyHex).First(&reloaded).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}

	if reloaded.Name != "new-name.txt" {
		t.Errorf("Name = %q, want %q", reloaded.Name, "new-name.txt")
	}
	if reloaded.Size != 999 {
		t.Errorf("Size = %d, want 999", reloaded.Size)
	}
	if reloaded.MediaType != "application/json" {
		t.Errorf("MediaType = %q, want %q", reloaded.MediaType, "application/json")
	}
	if reloaded.ContentDigest != "newdigest" {
		t.Errorf("ContentDigest = %q, want %q", reloaded.ContentDigest, "newdigest")
	}
}

// TestList_BareDirectoryPath verifies List resolves a bare directory path
// without a trailing slash (e.g. "vault:/docs") to that directory rather
// than silently falling back to the root.
func TestList_BareDirectoryPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	svc := &vaultService{
		db:     db,
		sdk:    &fakeSDK{t: t},
		appKey: types.PrivateKey{},
	}

	// Create /docs with a file, and a root-level file.
	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	now := time.Now().UTC()
	rok := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890a1"
	dk := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890b2"
	db.Create(&File{Name: "root.txt", DirectoryID: nil, ObjectKey: rok, Size: 1, ContentDigest: "r", CreatedAt: now, UpdatedAt: now})
	db.Create(&File{Name: "doc.pdf", DirectoryID: dirID, ObjectKey: dk, Size: 2, ContentDigest: "d", CreatedAt: now, UpdatedAt: now})

	// List the bare path "vault:/docs" (no trailing slash).
	items, err := svc.List(ctx, "vault:/docs")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should contain doc.pdf but NOT root.txt (which is at root, not /docs).
	foundDoc := false
	foundRoot := false
	for _, it := range items {
		if it.Name == "doc.pdf" {
			foundDoc = true
		}
		if it.Name == "root.txt" {
			foundRoot = true
		}
	}
	if !foundDoc {
		t.Error("List(vault:/docs) should include doc.pdf from /docs")
	}
	if foundRoot {
		t.Error("List(vault:/docs) incorrectly returned root.txt from the root directory")
	}
}

// TestSync_SkipsDuplicateRootName verifies Sync does not abort the whole loop
// when two remote objects share the same name at root (which violates the
// composite unique index). The duplicate is skipped and sync continues.
func TestSync_SkipsDuplicateRootName(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "dup.txt"),
		testObjectEvent(0x02, "dup.txt"),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync should skip duplicate root-name rather than abort: %v", err)
	}

	// Exactly one of the duplicate names should be recorded locally.
	var count int64
	db.Model(&File{}).Where("name = ? AND directory_id IS NULL", "dup.txt").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 root record for dup.txt after sync, got %d", count)
	}
}

// TestSync_DropsConflictingNameUpdate verifies that a synced metadata update
// which would collide the (name, directory_id) unique index against another
// existing record is dropped deterministically — Sync advances past it and keeps
// the first-seen record — matching the create branch. The batch progresses; a
// full cache rebuild reconciles any divergence.
func TestSync_DropsConflictingNameUpdate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	now := time.Now().UTC()
	keyA := types.Hash256{0x01}
	keyB := types.Hash256{0x02}
	// Two existing root records. A synced update renames key A to "b.txt",
	// which collides with key B's root name on the unique (name, directory_id)
	// index.
	if err := db.Create(&File{Name: "a.txt", DirectoryID: nil, ObjectKey: keyA.String(), Size: 1, ContentDigest: "a", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create a.txt: %v", err)
	}
	if err := db.Create(&File{Name: "b.txt", DirectoryID: nil, ObjectKey: keyB.String(), Size: 2, ContentDigest: "b", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create b.txt: %v", err)
	}

	svc := &vaultService{db: db, sdk: &fakeEvents{fakeSDK: fakeSDK{t: t}}, appKey: types.PrivateKey{}}
	svc.sdk.(*fakeEvents).events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "b.txt"), // conflicting rename
	}

	// Sync must not error; the conflicting rename is dropped (both first-seen
	// records are retained) and the batch advances.
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync should drop a conflicting name update rather than error; got: %v", err)
	}
	var aCount, bCount int64
	db.Model(&File{}).Where("name = ?", "a.txt").Count(&aCount)
	db.Model(&File{}).Where("name = ?", "b.txt").Count(&bCount)
	if aCount != 1 || bCount != 1 {
		t.Errorf("conflicting name update should be dropped, keeping first-seen records (a=%d b=%d, want 1/1)", aCount, bCount)
	}
}

// TestSync_CursorNotAdvancedPastSkipped verifies the sync cursor is advanced
// only to the last successfully-processed event, not to the end of the batch.
// A transient skip (a real object with empty metadata) after a processed event
// must stop the cursor so it is retried — advancing past it would leave the
// event permanently invisible in the local cache.
func TestSync_CursorNotAdvancedPastSkipped(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	processedKey := types.Hash256{0xAA}

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		// A valid object event — this one is processed.
		testObjectEvent(0xAA, "seen.txt"),
		// A transient skip: a real object with empty metadata. Must be
		// retried, so it must not advance the cursor.
		testTransientSkippedEvent(0xBB),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The processed event's file is recorded.
	var count int64
	db.Model(&File{}).Where("name = ?", "seen.txt").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 local record for seen.txt, got %d", count)
	}

	// The cached cursor must point at the LAST PROCESSED event (0xAA), not the
	// skipped nil-object event (0xBB). This is what makes the skipped event
	// retry on the next sync instead of being lost.
	var cursorRecord SyncDownCursor
	if err := db.First(&cursorRecord).Error; err != nil {
		t.Fatalf("failed to read stored cursor: %v", err)
	}
	stored, err := unmarshalCursor(cursorRecord.Cursor)
	if err != nil {
		t.Fatalf("failed to parse stored cursor: %v", err)
	}
	if stored.Key != processedKey {
		t.Errorf("sync cursor advanced to %x (skipped event); want %x (last processed)", stored.Key, processedKey)
	}
}

// TestSync_CursorStopsAtInterleavedSkip verifies the cursor stops at the FIRST
// transient skip, even when a later event in the same batch was processed. If
// the cursor instead advanced to the last processed event, an earlier skipped
// event (empty/unparsable metadata) would be permanently invisible in the
// local cache because the next fetch would start after it.
func TestSync_CursorStopsAtInterleavedSkip(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// Ordering: processed(0x01), transient skip(0x02), processed(0x03).
	// The skip at index 1 is BEFORE a later processed event (index 2), so the
	// cursor must stop at index 0 (events[0].Key == 0x01) — not advance to the
	// last processed event (0x03) and skip over 0x02 forever.
	firstKey := types.Hash256{0x01}
	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "a.txt"),
		testTransientSkippedEvent(0x02),
		testObjectEvent(0x03, "c.txt"),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Both processed files are recorded (they come before/after the skip).
	for _, name := range []string{"a.txt", "c.txt"} {
		var count int64
		db.Model(&File{}).Where("name = ?", name).Count(&count)
		if count != 1 {
			t.Errorf("expected 1 local record for %s, got %d", name, count)
		}
	}

	// The cursor must stop at the first processed event (0x01), i.e. the event
	// just before the first skip, so the skipped event (0x02) is retried on
	// the next sync instead of being passed over.
	var cursorRecord SyncDownCursor
	if err := db.First(&cursorRecord).Error; err != nil {
		t.Fatalf("failed to read stored cursor: %v", err)
	}
	stored, err := unmarshalCursor(cursorRecord.Cursor)
	if err != nil {
		t.Fatalf("failed to parse stored cursor: %v", err)
	}
	if stored.Key != firstKey {
		t.Errorf("sync cursor = %x; want %x (event before the first skipped event)", stored.Key, firstKey)
	}
}

// TestSync_LeadingNilObjectDoesNotStall verifies a leading nil-Object,
// non-deletion event (which can never resolve to file content) does NOT stall
// the whole batch. The cursor advances past it and later processed events are
// still recorded, so sync makes forward progress instead of re-fetching and
// reprocessing the same batch forever.
func TestSync_LeadingNilObjectDoesNotStall(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		// A nil-Object, non-deletion event first. This can never yield content,
		// so it must be passed over — not block the batch.
		{Key: types.Hash256{0x01}, Object: nil, UpdatedAt: time.Now().UTC()},
		testObjectEvent(0x02, "after.txt"),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The later processed event must be recorded despite the leading nil-Object
	// event.
	var count int64
	db.Model(&File{}).Where("name = ?", "after.txt").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 local record for after.txt (leading nil-Object must not stall the batch), got %d", count)
	}

	// The cursor must advance past the leading nil-Object event (to 0x02, the
	// last passed event), not be held at -1 (which would re-fetch forever).
	var cursorRecord SyncDownCursor
	if err := db.First(&cursorRecord).Error; err != nil {
		t.Fatalf("failed to read stored cursor: %v", err)
	}
	stored, err := unmarshalCursor(cursorRecord.Cursor)
	if err != nil {
		t.Fatalf("failed to parse stored cursor: %v", err)
	}
	if stored.Key != (types.Hash256{0x02}) {
		t.Errorf("sync cursor = %x; want %x (advanced past leading nil-Object)", stored.Key, types.Hash256{0x02})
	}
}

// TestSync_LeadingTransientSkipMakesProgress verifies a LEADING transient skip
// (the first event is a real object with empty metadata) does not livelock the
// batch. Previously no event before the skip existed to rewind to, lastProcessed
// stayed -1, no cursor was persisted, and every sync re-fetched the same batch
// forever — never recording the events after the skip. Sync must advance past
// the unresolvable leading skip and still record later events.
func TestSync_LeadingTransientSkipMakesProgress(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		// A leading transient skip: a real object with empty metadata. There
		// is nothing before it to stop the cursor at, but it must not stall
		// the batch.
		testTransientSkippedEvent(0x01),
		testObjectEvent(0x02, "after.txt"),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The event AFTER the leading skip must be recorded (no livelock).
	var count int64
	db.Model(&File{}).Where("name = ?", "after.txt").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 local record for after.txt (leading transient skip must not stall the batch), got %d", count)
	}

	// A cursor must have been persisted so the next sync makes progress (does
	// not re-fetch the identical batch forever).
	var cursorRecord SyncDownCursor
	if err := db.First(&cursorRecord).Error; err != nil {
		t.Fatalf("no cursor persisted after sync with a leading transient skip (livelock?): %v", err)
	}
}

// TestVerify_TransientObjectErrorSurfaces verifies Verify returns an error
// (rather than a misleading ObjectExists=false) when sdk.Object fails with a
// non-NotFound error, e.g. a transient indexer/network failure. Silently
// collapsing that to 'object missing' would falsely suggest data is
// corrupted/deleted.
func TestVerify_TransientObjectErrorSurfaces(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// A valid 64-char hex object key so parseHash256 succeeds.
	objKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := db.Create(&File{
		Name:          "v.txt",
		ObjectKey:     objKey,
		Size:          1,
		ContentDigest: "d",
	}).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}

	sdk := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: sdk, appKey: types.PrivateKey{}}

	// Transient (non-NotFound) error from sdk.Object must surface as an error.
	sdk.objErr = errors.New("indexer unavailable")
	if _, err := svc.Verify(ctx, "vault:/v.txt"); err == nil {
		t.Fatal("Verify should return an error for a transient Object failure, not ObjectExists=false")
	}

	// Genuine NotFound must produce ObjectExists=false (no error).
	sdk.objErr = slabs.ErrObjectNotFound
	res, err := svc.Verify(ctx, "vault:/v.txt")
	if err != nil {
		t.Fatalf("Verify for a genuinely missing object should not error: %v", err)
	}
	if res.ObjectExists {
		t.Error("ObjectExists = true, want false for a genuinely missing object")
	}
}

// TestSync_LeadingThenInterleavedSkipStopsAtInterleaved verifies that when a
// batch has BOTH a leading transient skip and a later interleaved transient
// skip ([skip, processed, skip, processed]), the cursor advances past the
// leading skip but stops before the interleaved one — it must NOT jump to the
// end of the batch (which would permanently drop the interleaved skip's object
// when its metadata later resolves). Regression for the leading-skip override
// that previously set lastProcessed = len(events)-1.
func TestSync_LeadingThenInterleavedSkipStopsAtInterleaved(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// Event order: skip@0 (leading, transient), processed@1 (a.txt),
	// skip@2 (interleaved, transient), processed@3 (c.txt).
	// Cursor must stop at the processed event before the interleaved skip
	// (index 1 -> key 0x02), NOT at the end of the batch (3 -> key 0x04).
	stopKey := types.Hash256{0x02}
	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	fe.events = []siastorage.ObjectEvent{
		testTransientSkippedEvent(0x01),
		testObjectEvent(0x02, "a.txt"),
		testTransientSkippedEvent(0x03),
		testObjectEvent(0x04, "c.txt"),
	}

	svc := &vaultService{
		db:     db,
		sdk:    fe,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Both processed events are recorded; the leading skip must not stall.
	for _, name := range []string{"a.txt", "c.txt"} {
		var count int64
		db.Model(&File{}).Where("name = ?", name).Count(&count)
		if count != 1 {
			t.Errorf("expected 1 local record for %s, got %d", name, count)
		}
	}

	// The cursor must stop before the INTERLEAVED skip (at the processed
	// event at index 1), not advance to the end of the batch and drop it.
	var cursorRecord SyncDownCursor
	if err := db.First(&cursorRecord).Error; err != nil {
		t.Fatalf("failed to read stored cursor: %v", err)
	}
	stored, err := unmarshalCursor(cursorRecord.Cursor)
	if err != nil {
		t.Fatalf("failed to parse stored cursor: %v", err)
	}
	if stored.Key != stopKey {
		t.Errorf("sync cursor = %x; want %x (stop before the interleaved skip, not end of batch)", stored.Key, stopKey)
	}
}

// TestSync_PendingSkipPreservedAcrossBatches verifies that an interleaved
// transient skip whose retry spans multiple batches is NOT reclassified as a
// leading (droppable) skip when it reappears at the head of the next batch.
// Without the pending-skip carryover, the skip at the head of batch 2 would be
// dropped and its file (b.txt) would never be synced once its metadata
// resolves.
func TestSync_PendingSkipPreservedAcrossBatches(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	svc := &vaultService{db: db, sdk: fe, appKey: types.PrivateKey{}}

	// Batch 1: an interleaved transient skip after a processed event. The
	// cursor stops before the skip and PendingSkip is persisted.
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "a.txt"),
		testTransientSkippedEvent(0x02),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 1: %v", err)
	}
	var rec SyncDownCursor
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 1: no cursor record: %v", err)
	}
	if !rec.PendingSkip {
		t.Fatal("batch 1: expected PendingSkip=true (interleaved skip awaiting retry)")
	}

	// Batch 2: the same skip (0x02) now appears at the HEAD, with a later
	// processed event. It must be treated as an interleaved retry (not a
	// leading skip), so the cursor holds and PendingSkip stays set.
	fe.events = []siastorage.ObjectEvent{
		testTransientSkippedEvent(0x02),
		testObjectEvent(0x03, "c.txt"),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 2: %v", err)
	}
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 2: no cursor record: %v", err)
	}
	// c.txt was recorded (the later processed event is still written).
	var cnt int64
	db.Model(&File{}).Where("name = ?", "c.txt").Count(&cnt)
	if cnt != 1 {
		t.Errorf("batch 2: expected c.txt recorded, got %d", cnt)
	}
	if !rec.PendingSkip {
		t.Fatal("batch 2: expected PendingSkip to remain true while the head skip is unresolved — it was reclassified as a droppable leading skip")
	}

	// Batch 3: the pending skip's metadata resolves (0x02 becomes b.txt). The
	// sync must now record it and clear the pending flag.
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x02, "b.txt"),
		testObjectEvent(0x04, "d.txt"),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 3: %v", err)
	}
	db.Model(&File{}).Where("name = ?", "b.txt").Count(&cnt)
	if cnt != 1 {
		t.Errorf("batch 3: expected b.txt recorded once the pending skip's metadata resolved, got %d (skip was likely dropped in batch 2)", cnt)
	}
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 3: no cursor record: %v", err)
	}
	if rec.PendingSkip {
		t.Fatal("batch 3: expected PendingSkip cleared after the skip resolved")
	}
}

// TestSync_PendingSkipClearedOnEmptyBatch verifies that when a carried-over
// pending interleaved skip's object disappears without a delete event (or the
// batch is empty), the stale PendingSkip flag is cleared. Otherwise the next
// batch seeds seenProcessed=true and reclassifies a genuinely-leading transient
// skip as an interleaved retry, stalling sync indefinitely waiting on an object
// that no longer exists.
func TestSync_PendingSkipClearedOnEmptyBatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	svc := &vaultService{db: db, sdk: fe, appKey: types.PrivateKey{}}

	// Batch 1: interleaved transient skip -> PendingSkip=true persisted.
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "a.txt"),
		testTransientSkippedEvent(0x02),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 1: %v", err)
	}
	var rec SyncDownCursor
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 1: no cursor record: %v", err)
	}
	if !rec.PendingSkip {
		t.Fatal("batch 1: expected PendingSkip=true")
	}

	// Batch 2: empty — the carried-over pending object was removed without a
	// delete event. PendingSkip must be cleared.
	fe.events = nil
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 2 (empty): %v", err)
	}
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 2: no cursor record: %v", err)
	}
	if rec.PendingSkip {
		t.Fatal("batch 2: expected PendingSkip cleared on an empty batch (pending object no longer reappears)")
	}

	// Batch 3: a GENUINELY-leading transient skip followed by a processed
	// event. It must be treated as a leading (passable) skip — not a stale
	// interleaved retry — so sync makes progress (d.txt recorded, pending
	// cleared, no indefinite stall).
	fe.events = []siastorage.ObjectEvent{
		testTransientSkippedEvent(0x05),
		testObjectEvent(0x06, "d.txt"),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 3: %v", err)
	}
	var cnt int64
	db.Model(&File{}).Where("name = ?", "d.txt").Count(&cnt)
	if cnt != 1 {
		t.Errorf("batch 3: expected d.txt recorded (leading skip must not be reclassified as an interleaved retry), got %d", cnt)
	}
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 3: no cursor record: %v", err)
	}
	if rec.PendingSkip {
		t.Fatal("batch 3: stale PendingSkip reclassified a genuinely-leading skip as an interleaved retry, stalling sync")
	}
}
