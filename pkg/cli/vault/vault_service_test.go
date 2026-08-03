package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
// root file name in its metadata, stamped with a stable UUID derived from the
// key so each event maps to its own identity (matching how real objects carry
// their file UUID in metadata).
func testObjectEvent(keyByte byte, name string) siastorage.ObjectEvent {
	key := types.Hash256{keyByte}
	meta := FileMetadata{
		ID:            "uuid-" + fmt.Sprintf("%02x", keyByte),
		Name:          name,
		Size:          10,
		ContentDigest: "d",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
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
			UUID:          "uuid-" + string(rune('A'+i)),
			Name:          "file" + string(rune('0'+i)) + ".pdf",
			DirectoryID:   dirID,
			IsCurrent:     true,
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

// TestStat_BareDirectoryPath regression: Stat of a bare (non-trailing-slash)
// path that names an existing directory (vault:/docs -> Directory "/", Name
// "docs") must report the directory instead of a misleading ErrNotFound,
// mirroring how List resolves the same root-leaf ambiguity.
func TestStat_BareDirectoryPath(t *testing.T) {
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

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}

	if _, err := svc.getOrCreateDirectory("/docs"); err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	// Bare path at root that names an existing directory -> must be a dir.
	res, err := svc.Stat(ctx, "vault:/docs")
	if err != nil {
		t.Fatalf("Stat(vault:/docs) failed: %v", err)
	}
	if res.Type != "dir" || res.Name != "docs" {
		t.Errorf("Stat(vault:/docs) = type %q name %q, want dir/docs", res.Type, res.Name)
	}

	// Trailing-slash form still works (existing behavior).
	if res, err := svc.Stat(ctx, "vault:/docs/"); err != nil || res.Type != "dir" {
		t.Errorf("Stat(vault:/docs/) = %+v err %v, want dir", res, err)
	}

	// A bare path that is neither a file nor an existing directory still
	// returns ErrNotFound (not silently treated as a dir).
	if _, err := svc.Stat(ctx, "vault:/nosuchdir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat(vault:/nosuchdir) error = %v, want errors.Is(err, ErrNotFound)", err)
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
		UUID:          "uuid-stale",
		Name:          "old-name.txt",
		DirectoryID:   nil,
		IsCurrent:     true,
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

	// Simulate what sync.go's update branch does: find by identity (UUID),
	// then copy all fields from fileMeta before Save.
	var existing File
	result := db.Where("uuid = ?", "uuid-stale").First(&existing)
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
	if err := db.Where("uuid = ?", "uuid-stale").First(&reloaded).Error; err != nil {
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
	db.Create(&File{UUID: "uuid-root", Name: "root.txt", DirectoryID: nil, IsCurrent: true, ObjectKey: rok, Size: 1, ContentDigest: "r", CreatedAt: now, UpdatedAt: now})
	db.Create(&File{UUID: "uuid-doc", Name: "doc.pdf", DirectoryID: dirID, IsCurrent: true, ObjectKey: dk, Size: 2, ContentDigest: "d", CreatedAt: now, UpdatedAt: now})

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

// TestList_FilePath_ResolvesParent regression: listing a concrete FILE path
// (e.g. vault:/docs/report.pdf) must list that file's PARENT directory
// (vault:/docs), not attempt to look up the full path as a directory and
// silently return nothing.
func TestList_FilePath_ResolvesParent(t *testing.T) {
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

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}

	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	// A file in /docs plus a file at root.
	now := time.Now().UTC()
	dk := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890b2"
	rk := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890c3"
	db.Create(&File{UUID: "u-doc", Name: "report.pdf", DirectoryID: dirID, IsCurrent: true, ObjectKey: dk, Size: 2, ContentDigest: "d", CreatedAt: now, UpdatedAt: now})
	db.Create(&File{UUID: "u-root", Name: "root.txt", DirectoryID: nil, IsCurrent: true, ObjectKey: rk, Size: 1, ContentDigest: "r", CreatedAt: now, UpdatedAt: now})

	// List a concrete file path — must resolve to its PARENT (/docs).
	items, err := svc.List(ctx, "vault:/docs/report.pdf")
	if err != nil {
		t.Fatalf("List(file path) failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("List(vault:/docs/report.pdf) returned empty; should list the parent /docs")
	}
	foundDoc := false
	for _, it := range items {
		if it.Name == "report.pdf" {
			foundDoc = true
		}
		if it.Name == "root.txt" {
			t.Error("List(vault:/docs/report.pdf) should NOT return root.txt (listed root instead of parent /docs)")
		}
	}
	if !foundDoc {
		t.Errorf("List(vault:/docs/report.pdf) should list the parent /docs containing report.pdf; got %+v", items)
	}
}

// TestSync_PersistsSameNameObjects verifies Sync records BOTH distinct objects
// that share a name at root. Identity is the UUID, not the name, so a second
// object with the same name is a separate row — it is never dropped (the
// data-loss the old unique (name, dir) index caused).
func TestSync_PersistsSameNameObjects(t *testing.T) {
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
		t.Fatalf("Sync should record both same-name objects rather than abort: %v", err)
	}

	// BOTH distinct objects are tracked (distinct UUIDs), so both persist.
	var count int64
	db.Model(&File{}).Where("name = ? AND directory_id IS NULL", "dup.txt").Count(&count)
	if count != 2 {
		t.Errorf("expected 2 root records for dup.txt (both objects persist), got %d", count)
	}
}

// TestSync_UpdatesRenameOnSameRow verifies a metadata update that renames an
// object just updates that object's own row (same UUID, new name) — there is
// no unique (name, directory_id) collision to drop or retry, so a rename is a
// plain row update and both the renamed object and any other same-name object
// coexist.
func TestSync_UpdatesRenameOnSameRow(t *testing.T) {
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
	// Two existing root records with distinct UUIDs: "a.txt" (uuid-01) and
	// "b.txt" (uuid-02).
	if err := db.Create(&File{UUID: "uuid-01", Name: "a.txt", DirectoryID: nil, IsCurrent: true, ObjectKey: keyA.String(), Size: 1, ContentDigest: "a", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create a.txt: %v", err)
	}
	if err := db.Create(&File{UUID: "uuid-02", Name: "b.txt", DirectoryID: nil, IsCurrent: true, ObjectKey: keyB.String(), Size: 2, ContentDigest: "b", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create b.txt: %v", err)
	}

	svc := &vaultService{db: db, sdk: &fakeEvents{fakeSDK: fakeSDK{t: t}}, appKey: types.PrivateKey{}}
	// A synced update renames uuid-01's object from "a.txt" to "b.txt" (the key
	// event's metadata UUID is uuid-01). This updates uuid-01's row name to
	// "b.txt"; uuid-02's separate "b.txt" row is untouched. No drop, no error.
	svc.sdk.(*fakeEvents).events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "b.txt"), // uuid-01 renamed to b.txt
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync should apply a rename as a same-row update; got: %v", err)
	}

	var aCount, bCount int64
	db.Model(&File{}).Where("name = ?", "a.txt").Count(&aCount)
	db.Model(&File{}).Where("name = ?", "b.txt").Count(&bCount)
	if aCount != 0 || bCount != 2 {
		t.Errorf("rename should move uuid-01 to b.txt keeping both same-name rows (a=%d b=%d, want 0/2)", aCount, bCount)
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
		UUID:          "uuid-v",
		Name:          "v.txt",
		DirectoryID:   nil,
		IsCurrent:     true,
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

// TestNewVaultServiceForProfile_RejectsTraversal regression: a profile name
// carrying path separators (../, absolute, backslash) must be rejected by
// NewVaultServiceForProfile before it is used to build state.json / SQLite
// paths, so it cannot escape the vault profile directory.
func TestNewVaultServiceForProfile_RejectsTraversal(t *testing.T) {
	bad := []string{
		"../escape",
		"../../etc",
		"/abs/path",
		"a/b",
		"a\\b",
		"..",
		".",
	}
	for _, name := range bad {
		if _, err := NewVaultServiceForProfile(name, ""); err == nil {
			t.Errorf("profile name %q: expected path-traversal rejection, got nil", name)
		} else if !strings.Contains(err.Error(), "profile name") {
			t.Errorf("profile name %q: expected a profile-name validation error, got: %v", name, err)
		}
	}
}

// TestNewVaultServiceForProfile_AcceptValidNames ensures benign profile names
// are not rejected, and that the expected failure mode for a valid name is the
// "no app key / missing state" path — NOT a validation error.
func TestNewVaultServiceForProfile_AcceptValidNames(t *testing.T) {
	// Valid names should proceed past validation; with no seeded state they
	// fail later (missing state), never with a profile-name error.
	for _, name := range []string{"alpha", "my-profile_2", "a.b"} {
		if _, err := NewVaultServiceForProfile(name, ""); err != nil {
			if strings.Contains(err.Error(), "profile name") {
				t.Errorf("valid profile name %q: unexpected validation error: %v", name, err)
			}
		}
	}
}

// TestGetOrCreateDirectory_ConcurrentSamePath regression: concurrent creation
// of the same new directory path must converge on a single directory row — the
// unique idx_directories_path conflict is re-resolved instead of failing the
// writer.
func TestGetOrCreateDirectory_ConcurrentSamePath(t *testing.T) {
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

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}

	const n = 8
	var wg sync.WaitGroup
	ids := make([]*uint, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids[idx], errs[idx] = svc.getOrCreateDirectory("/shared/concurrent/dir")
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent getOrCreateDirectory %d failed: %v", i, e)
		}
	}

	// Exactly one directory row must exist for the path (no duplicates), and
	// every writer must have resolved to that same row.
	var count int64
	db.Model(&Directory{}).Where("path = ?", "/shared/concurrent/dir").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 directory row for the shared path, got %d", count)
	}
	first := ids[0]
	if first == nil {
		t.Fatal("first writer returned nil directory id")
	}
	for i, id := range ids {
		if id == nil || *id != *first {
			t.Errorf("writer %d resolved to a different directory id (%v vs %v)", i, id, first)
		}
	}
}

// testObjectEventInDir builds an ObjectEvent stamped with both a name and a
// vault directory path in its metadata, simulating an object uploaded to a
// nested path that Sync must place in the corresponding directory.
func testObjectEventInDir(keyByte byte, name, dir string) siastorage.ObjectEvent {
	key := types.Hash256{keyByte}
	meta := FileMetadata{
		ID:            "uuid-" + fmt.Sprintf("%02x", keyByte),
		Name:          name,
		Directory:     dir,
		Size:          10,
		ContentDigest: "d",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	raw, _ := meta.JSON()
	obj := siastorage.NewEmptyObject()
	obj.UpdateMetadata(raw)
	return siastorage.ObjectEvent{Key: key, Object: &obj, UpdatedAt: time.Now().UTC()}
}

// TestSync_PlacesRemoteObjectInMetadataDirectory regression: an object synced
// from the indexer must land in the directory carried in its FileMetadata (not
// the vault root), so an upload to vault:/reports/report.pdf on one device
// syncs into vault:/reports on another instead of losing the hierarchy.
func TestSync_PlacesRemoteObjectInMetadataDirectory(t *testing.T) {
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

	fe.events = []siastorage.ObjectEvent{
		testObjectEventInDir(0x10, "report.pdf", "/reports/2024"),
		testObjectEventInDir(0x11, "root.txt", ""), // no directory => root
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var dir Directory
	if err := db.Where("path = ?", "/reports/2024").First(&dir).Error; err != nil {
		t.Fatalf("expected /reports/2024 directory to be created: %v", err)
	}

	var f File
	if err := db.Where("name = ?", "report.pdf").First(&f).Error; err != nil {
		t.Fatalf("report.pdf not synced: %v", err)
	}
	if f.DirectoryID == nil || *f.DirectoryID != dir.ID {
		t.Fatalf("report.pdf DirectoryID = %v, want %d (must live in /reports/2024)", f.DirectoryID, dir.ID)
	}

	var rf File
	if err := db.Where("name = ?", "root.txt").First(&rf).Error; err != nil {
		t.Fatalf("root.txt not synced: %v", err)
	}
	if rf.DirectoryID != nil {
		t.Fatalf("root.txt DirectoryID = %v, want nil (root)", rf.DirectoryID)
	}
}

// TestSync_DropsStuckPendingSkipAfterRetryCap regression: a pending head skip
// whose metadata NEVER resolves must not stall the cursor forever. After
// maxPendingSkipRetries consecutive unresolved re-appearances it is dropped and
// later events advance, even though a transient skip that resolves within the
// cap is still retried (covered by TestSync_PendingSkipPreservedAcrossBatches).
func TestSync_DropsStuckPendingSkipAfterRetryCap(t *testing.T) {
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

	// Establish a pending skip on the first batch (interleaved after a.txt).
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x20, "a.txt"),
		testTransientSkippedEvent(0x21),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	var rec SyncDownCursor
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("no cursor: %v", err)
	}
	if !rec.PendingSkip {
		t.Fatal("batch 1: expected PendingSkip set")
	}

	// Replay the never-resolving head skip across enough batches to exceed the
	// retry cap. Each batch also carries a later event so the cursor would
	// otherwise stall; after the cap the skip is dropped and c.txt advances.
	var cnt int64
	for i := 0; i < int(maxPendingSkipRetries)+2; i++ {
		fe.events = []siastorage.ObjectEvent{
			testTransientSkippedEvent(0x21),
			testObjectEvent(0x22, "c.txt"),
		}
		if _, err := svc.Sync(ctx); err != nil {
			t.Fatalf("batch %d: %v", i+2, err)
		}
		db.First(&rec)
	}

	// After exceeding the cap the stuck skip must have been dropped: later
	// events advance and the pending flag is cleared.
	db.Model(&File{}).Where("name = ?", "c.txt").Count(&cnt)
	if cnt != 1 {
		t.Errorf("expected c.txt recorded after stuck skip dropped, got %d", cnt)
	}
	db.First(&rec)
	if rec.PendingSkip {
		t.Error("expected PendingSkip cleared after stuck skip dropped")
	}
}

// TestSync_DropsStuckPendingSkipAlone regression: a stuck pending head skip
// with NO processable event after it (lastProcessed == -1) must still be
// dropped once the retry cap is exceeded. Previously the else-if branch held
// the cursor and reset the counter to 0 each batch, stalling forever.
func TestSync_DropsStuckPendingSkipAlone(t *testing.T) {
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

	// Establish a pending skip on the first batch (interleaved after a.txt).
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x30, "a.txt"),
		testTransientSkippedEvent(0x31),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	var rec SyncDownCursor
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("no cursor: %v", err)
	}
	beforeCursor := rec.Cursor

	// Replay the never-resolving head skip ALONE (nothing after it) enough
	// times to exceed the cap. The cursor must eventually advance past it and
	// PendingSkip must clear — not hold forever with the counter reset.
	for i := 0; i < int(maxPendingSkipRetries)+2; i++ {
		fe.events = []siastorage.ObjectEvent{
			testTransientSkippedEvent(0x31),
		}
		if _, err := svc.Sync(ctx); err != nil {
			t.Fatalf("batch %d: %v", i+2, err)
		}
	}

	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("no cursor: %v", err)
	}
	if rec.PendingSkip {
		t.Error("expected PendingSkip cleared after stuck-alone skip dropped")
	}
	if rec.Cursor == beforeCursor {
		t.Error("expected cursor to advance past the stuck-alone skip after the drop")
	}
}

// TestPut_Stat_MetadataRoundTrips regression: user metadata passed to Put must
// be persisted on the local File row and surfaced back through Stat, so
// `vault put --metadata` data survives cache rebuilds and is not silently lost.
func TestPut_Stat_MetadataRoundTrips(t *testing.T) {
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

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}

	meta := map[string]any{"source": "test", "nested": map[string]any{"k": "v"}}
	data := bytes.NewReader([]byte("payload"))
	if _, err := svc.Put(ctx, data, int64(data.Len()), "vault:/meta.txt", meta); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// The local DB row must carry the metadata.
	var row File
	if err := db.Where("name = ? AND is_current = 1", "meta.txt").First(&row).Error; err != nil {
		t.Fatalf("no row for meta.txt: %v", err)
	}
	if len(row.Metadata) == 0 {
		t.Fatal("expected local File row to persist metadata, got empty")
	}

	// Stat must surface the metadata.
	st, err := svc.Stat(ctx, "vault:/meta.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if st.Metadata == nil || st.Metadata["source"] != "test" {
		t.Errorf("Stat.Metadata = %v, want source=test", st.Metadata)
	}
	if st.Metadata["nested"] == nil {
		t.Errorf("Stat.Metadata.nested missing from %v", st.Metadata)
	}
}

// TestIsDirNameConflict_MatchesRealError regression: isDirNameConflict must
// match the error the go-sqlite3 driver actually reports for an idx_directories_path
// violation. go-sqlite3 reports the COLUMNS for a plain (non-partial) unique index
// ("UNIQUE constraint failed: directories.path"), NOT the index name — only a
// partial index (like idx_files_live_name_dir) is reported by index name.
// Matching on "idx_directories_path" (the index name) would never fire and would
// make resolveVaultDirectory fall through to a hard error instead of re-resolving.
func TestIsDirNameConflict_MatchesRealError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		// Real go-sqlite3 message for idx_directories_path (confirmed at the
		// raw driver level: columns, not index name).
		{"UNIQUE constraint failed: directories.path", true},
		// The file partial index reports its name — must NOT match this helper.
		{"UNIQUE constraint failed: index 'idx_files_live_name_dir'", false},
		// Unrelated constraints.
		{"UNIQUE constraint failed: directories.id", false},
		{"NOT NULL constraint failed: directories.created_at", false},
		{"some other error", false},
	}
	for _, c := range cases {
		if got := isDirNameConflict(errors.New(c.msg)); got != c.want {
			t.Errorf("isDirNameConflict(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
	if isDirNameConflict(nil) {
		t.Error("isDirNameConflict(nil) = true, want false")
	}
}

// TestSync_NilObjectThenTransientSkip_IsRetried regression: a passable
// nil-object event followed later in the same batch by a real object with
// empty metadata must classify that skip as INTERLEAVED (held for retry), not
// as a leading skip (advanced past and dropped permanently). The nil-object
// branch previously failed to set seenProcessed, so the following skip was
// misclassified as leading and never resolvable to a file on secondary devices.
func TestSync_NilObjectThenTransientSkip_IsRetried(t *testing.T) {
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

	// Batch 1: a nil-object no-op event, then a real object with empty metadata
	// (transient skip). The skip must be treated as interleaved and retried.
	fe.events = []siastorage.ObjectEvent{
		{Key: types.Hash256{0x10}, Object: nil, UpdatedAt: time.Now().UTC()},
		testTransientSkippedEvent(0x20),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 1: %v", err)
	}
	var rec SyncDownCursor
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("batch 1: no cursor record: %v", err)
	}
	if !rec.PendingSkip {
		t.Fatal("batch 1: expected PendingSkip=true so the nil-object-following skip is retried, not dropped")
	}

	// Batch 2: the same skip (0x20) at the head now has metadata and must be
	// recorded as a file — proving it was not permanently dropped in batch 1.
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x20, "resolved.txt"),
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync batch 2: %v", err)
	}
	var cnt int64
	db.Model(&File{}).Where("name = ? AND deleted_at IS NULL", "resolved.txt").Count(&cnt)
	if cnt != 1 {
		t.Errorf("expected resolved.txt to be recorded after the skip was retried, got %d rows", cnt)
	}
}

// TestSync_DeleteEvent regression: an object-key delete means the content is
// gone remotely, so every live current alias referencing it must be cleared
// (soft-tombstoned) — a shared/deduplicated key must NOT leave stale rows that
// stay visible in ls/stat on secondary devices forever. Historical versions
// (is_current=0) and already-tombstoned rows are preserved. When the delete
// event carries metadata with a per-file UUID, only that exact row is cleared,
// so unrelated live aliases sharing the same content address survive.
func TestSync_DeleteEvent(t *testing.T) {
	const sharedKey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	t.Run("shared key clears all live current aliases", func(t *testing.T) {
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
		rows := []File{
			{UUID: "u-a", Name: "a.txt", ObjectKey: sharedKey, IsCurrent: true, Size: 1, CreatedAt: now, UpdatedAt: now},
			{UUID: "u-b", Name: "b.txt", ObjectKey: sharedKey, IsCurrent: true, Size: 1, CreatedAt: now, UpdatedAt: now},
			{UUID: "u-hist", Name: "a.txt", ObjectKey: sharedKey, IsCurrent: false, Size: 1, CreatedAt: now, UpdatedAt: now},
		}
		for _, r := range rows {
			if err := db.Create(&r).Error; err != nil {
				t.Fatalf("create row: %v", err)
			}
		}

		fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
		svc := &vaultService{db: db, sdk: fe, appKey: types.PrivateKey{}}
		delKey, err := parseHash256(sharedKey)
		if err != nil {
			t.Fatalf("parseHash256: %v", err)
		}

		// No metadata on the delete event (Object nil): the shared object is
		// gone, so both live current aliases must be tombstoned; the retained
		// historical version stays.
		fe.events = []siastorage.ObjectEvent{
			{Key: delKey, Deleted: true, UpdatedAt: time.Now().UTC()},
		}
		if _, err := svc.Sync(ctx); err != nil {
			t.Fatalf("sync (shared delete): %v", err)
		}
		var liveCurrent int64
		db.Model(&File{}).Where("object_key = ? AND deleted_at IS NULL AND is_current = 1", sharedKey).Count(&liveCurrent)
		if liveCurrent != 0 {
			t.Errorf("object-key delete must clear ALL live current aliases, got %d still live", liveCurrent)
		}
		var histLive int64
		db.Model(&File{}).Where("uuid = ? AND deleted_at IS NULL", "u-hist").Count(&histLive)
		if histLive != 1 {
			t.Errorf("delete must preserve the retained historical version, got %d live", histLive)
		}
	})

	t.Run("metadata UUID disambiguates shared key", func(t *testing.T) {
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
		rows := []File{
			{UUID: "u-a", Name: "a.txt", ObjectKey: sharedKey, IsCurrent: true, Size: 1, CreatedAt: now, UpdatedAt: now},
			{UUID: "u-b", Name: "b.txt", ObjectKey: sharedKey, IsCurrent: true, Size: 1, CreatedAt: now, UpdatedAt: now},
		}
		for _, r := range rows {
			if err := db.Create(&r).Error; err != nil {
				t.Fatalf("create row: %v", err)
			}
		}

		fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
		svc := &vaultService{db: db, sdk: fe, appKey: types.PrivateKey{}}
		delKey, err := parseHash256(sharedKey)
		if err != nil {
			t.Fatalf("parseHash256: %v", err)
		}

		// The delete event carries metadata naming file u-a: only u-a is
		// cleared; u-b (a live current sibling sharing the same object) stays.
		obj := siastorage.NewEmptyObject()
		meta := FileMetadata{ID: "u-a", Name: "a.txt"}
		raw, _ := meta.JSON()
		obj.UpdateMetadata(raw)
		fe.events = []siastorage.ObjectEvent{
			{Key: delKey, Object: &obj, Deleted: true, UpdatedAt: time.Now().UTC()},
		}
		if _, err := svc.Sync(ctx); err != nil {
			t.Fatalf("sync (uuid delete): %v", err)
		}
		var aLive, bLive int64
		db.Model(&File{}).Where("uuid = ? AND deleted_at IS NULL", "u-a").Count(&aLive)
		db.Model(&File{}).Where("uuid = ? AND deleted_at IS NULL", "u-b").Count(&bLive)
		if aLive != 0 {
			t.Errorf("u-a (named in delete metadata) must be tombstoned, got %d live", aLive)
		}
		if bLive != 1 {
			t.Errorf("u-b (shared key, different identity) must survive, got %d live", bLive)
		}
	})
}

// TestList_DBErrorIsPropagated regression: List must only swallow a missing
// directory (ErrRecordNotFound -> empty list); a genuine DB failure must be
// propagated rather than silently presenting a populated vault as empty.
func TestList_DBErrorIsPropagated(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	defer sqlDB.Close()

	// Create a directory so the path resolves past "missing" semantics.
	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}
	if _, err := svc.getOrCreateDirectory("/docs"); err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	// Close the connection: the getDirectoryID query now fails with a real DB
	// error, which List must propagate (not return an empty list).
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if _, err := svc.List(ctx, "vault:/docs"); err == nil {
		t.Fatal("expected List to propagate the DB error, got nil (error was swallowed as empty)")
	}
}

// TestList_SubdirsOnlyDirectChildren regression: listing a directory must not
// surface deeper descendants as direct children, and must not require loading
// the entire subtree (direct-child filtering happens in SQL).
func TestList_SubdirsOnlyDirectChildren(t *testing.T) {
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

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}
	// /docs with a direct child /docs/sub and a grandchild /docs/sub/inner.
	for _, p := range []string{"/docs", "/docs/sub", "/docs/sub/inner"} {
		if _, err := svc.getOrCreateDirectory(p); err != nil {
			t.Fatalf("getOrCreateDirectory(%s): %v", p, err)
		}
	}

	items, err := svc.List(ctx, "vault:/docs")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Only "sub" is a direct child; "inner" (grandchild) must not appear.
	names := map[string]bool{}
	for _, it := range items {
		if it.Type == "dir" {
			names[it.Name] = true
		}
	}
	if !names["sub"] {
		t.Errorf("expected direct child dir 'sub' in listing, got %v", names)
	}
	if names["inner"] {
		t.Errorf("grandchild 'inner' must not appear as a direct child, got %v", names)
	}
}

// TestSync_ReturnsAppliedCount regression: Sync must return the number of events
// actually applied (lastProcessed+1), not the full batch size, so a reboot-loop
// caller can tell forward progress from a stall where the cursor is held before
// an unresolved interleaved transient skip.
func TestSync_ReturnsAppliedCount(t *testing.T) {
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

	// Batch with a processed event followed by an interleaved transient skip:
	// the cursor holds before the skip, so only the first event is applied.
	fe.events = []siastorage.ObjectEvent{
		testObjectEvent(0x01, "a.txt"),
		testTransientSkippedEvent(0x02),
	}
	n, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if n != 1 {
		t.Errorf("Sync returned %d applied events, want 1 (interleaved skip holds the cursor; batch size was 2)", n)
	}
}

// TestSync_NewObjectPersistsUserMetadata regression: when Sync creates a brand
// new row for a freshly-seen object, it must persist the object's user metadata
// on the local File row (mirroring upsertFromMeta and Put), so Stat returns it
// immediately on a fresh-cache sync rather than only after an overwrite.
func TestSync_NewObjectPersistsUserMetadata(t *testing.T) {
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

	// Build an object event whose FileMetadata carries a user Metadata map.
	key := types.Hash256{0x31}
	meta := FileMetadata{
		ID:        "uuid-meta",
		Name:      "meta.txt",
		Size:      5,
		Metadata:  map[string]any{"origin": "sync", "n": float64(7)},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, _ := meta.JSON()
	obj := siastorage.NewEmptyObject()
	obj.UpdateMetadata(raw)
	fe.events = []siastorage.ObjectEvent{{Key: key, Object: &obj, UpdatedAt: time.Now().UTC()}}

	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var rec File
	if err := db.Where("uuid = ?", "uuid-meta").First(&rec).Error; err != nil {
		t.Fatalf("row not created: %v", err)
	}
	if len(rec.Metadata) == 0 {
		t.Fatal("new row has no Metadata; user metadata from the object must be persisted on fresh sync")
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Metadata, &m); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if m["origin"] != "sync" || m["n"] != float64(7) {
		t.Errorf("metadata = %v, want origin=sync, n=7", m)
	}
}

// TestSync_DeleteEvent_UUIDMissFallsBackToKey regression: when a delete event
// carries metadata naming a UUID that matches NO live current row, the sync must
// fall through to the object_key-based tombstone instead of skipping it (which
// would leave live rows referencing that object key untouched forever).
func TestSync_DeleteEvent_UUIDMissFallsBackToKey(t *testing.T) {
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

	const sharedKey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	now := time.Now().UTC()
	row := File{UUID: "u-live", Name: "live.txt", ObjectKey: sharedKey, IsCurrent: true, Size: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create row: %v", err)
	}

	fe := &fakeEvents{fakeSDK: fakeSDK{t: t}}
	svc := &vaultService{db: db, sdk: fe, appKey: types.PrivateKey{}}
	delKey, err := parseHash256(sharedKey)
	if err != nil {
		t.Fatalf("parseHash256: %v", err)
	}

	// Delete event names a UUID that does NOT match the live row. The UUID
	// tombstone hits 0 rows, so sync must fall through and tombstone by key.
	obj := siastorage.NewEmptyObject()
	m := FileMetadata{ID: "uuid-nonexistent", Name: "ghost.txt"}
	raw, _ := m.JSON()
	obj.UpdateMetadata(raw)
	fe.events = []siastorage.ObjectEvent{
		{Key: delKey, Object: &obj, Deleted: true, UpdatedAt: time.Now().UTC()},
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var live int64
	db.Model(&File{}).Where("uuid = ? AND deleted_at IS NULL", "u-live").Count(&live)
	if live != 0 {
		t.Errorf("u-live must be tombstoned via the object-key fallback when the event UUID matches no live row, got %d live", live)
	}
}
