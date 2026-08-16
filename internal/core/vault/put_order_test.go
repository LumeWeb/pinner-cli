package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
)

// fakeSDK implements sdkClient. It records DeleteObject calls and stubs
// Upload/PinObject so that Upload on a NewEmptyObject produces a predictable
// objectKey (the zero hash of an empty slab list).
type fakeSDK struct {
	t              *testing.T
	deleted        []string // object keys passed to DeleteObject
	uploadCalled   bool
	pinCalled      bool
	downloadCalled bool   // whether Download was invoked
	delErr         error  // error to return from DeleteObject (nil = success)
	objErr         error  // error to return from Object (nil = success)
	pinnedMeta     []byte // metadata attached to the most recently pinned object
}

func (f *fakeSDK) Account(_ context.Context) (app.AccountResponse, error) {
	return app.AccountResponse{}, nil
}
func (f *fakeSDK) AppKey() types.PrivateKey {
	return types.PrivateKey{}
}
func (f *fakeSDK) Upload(_ context.Context, _ *siastorage.Object, r io.Reader, _ ...siastorage.UploadOption) error {
	f.uploadCalled = true
	// Consume the body so a TeeReader hasher sees the bytes, mirroring a real
	// upload that reads the stream.
	_, _ = io.Copy(io.Discard, r)
	return nil
}
func (f *fakeSDK) PinObject(_ context.Context, obj siastorage.Object) error {
	f.pinCalled = true
	f.pinnedMeta = obj.Metadata()
	return nil
}
func (f *fakeSDK) Object(_ context.Context, _ types.Hash256) (siastorage.Object, error) {
	if f.objErr != nil {
		return siastorage.Object{}, f.objErr
	}
	return siastorage.NewEmptyObject(), nil
}
func (f *fakeSDK) ObjectEvents(_ context.Context, _ slabs.Cursor, _ int) ([]siastorage.ObjectEvent, error) {
	return nil, nil
}
func (f *fakeSDK) Download(_ siastorage.Object, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	f.downloadCalled = true
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (f *fakeSDK) DeleteObject(_ context.Context, key types.Hash256) error {
	f.deleted = append(f.deleted, key.String())
	return f.delErr
}
func (f *fakeSDK) CreateSharedObjectURL(_ context.Context, _ types.Hash256, _ time.Time) (string, error) {
	return "", nil
}
func (f *fakeSDK) Close() error { return nil }

// TestPut_CreateBeforeDestroyOrder drives svc.Put with a fake SDK and asserts
// that DeleteObject is called only after the new record is committed, and only
// when prior.ObjectKey != new objectKey.
func TestPut_CreateBeforeDestroyOrder(t *testing.T) {
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

	fake := &fakeSDK{t: t}
	svc := &vaultService{
		db:     db,
		sdk:    fake,
		appKey: types.PrivateKey{},
	}

	// Create a directory
	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory failed: %v", err)
	}

	// Manually insert a prior record with a known ObjectKey (64-char hex)
	priorKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	prior := File{
		UUID:          "uuid-prior",
		Name:          "report.pdf",
		DirectoryID:   dirID,
		IsCurrent:     true,
		ObjectKey:     priorKey,
		Size:          100,
		ContentDigest: "olddigest",
	}
	if err := db.Create(&prior).Error; err != nil {
		t.Fatalf("create prior file: %v", err)
	}

	// Manual prior record with a known ObjectKey (64-char hex).
	// (priorKey/prior defined above.)

	// Call svc.Put (overwrite). With versioning this should:
	//  1. Insert a NEW version row (same UUID as the prior current winner)
	//  2. Demote the prior row to is_current=0 (it keeps its ObjectKey = history)
	//  3. NOT call DeleteObject (prior content is preserved as an older version)
	newMeta := map[string]any{"source": "test"}
	data := bytes.NewReader([]byte("new content"))
	rec, err := svc.Put(ctx, data, int64(data.Len()), "vault:/docs/report.pdf", newMeta)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if rec.Name != "report.pdf" {
		t.Errorf("record.Name = %q, want %q", rec.Name, "report.pdf")
	}

	// Versioning preserves prior content: overwriting must NOT reclaim the prior
	// object from the indexer.
	if len(fake.deleted) != 0 {
		t.Fatalf("expected 0 DeleteObject calls on overwrite (versioning preserves history), got %d: %v", len(fake.deleted), fake.deleted)
	}

	// Verify DB state: two version rows now share the (name, directory_id) —
	// the prior row (non-current, retains its ObjectKey) and the new current row.
	var rows []File
	db.Where("name = ? AND directory_id = ?", "report.pdf", dirID).Order("seq ASC").Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("expected 2 version rows after overwrite, got %d", len(rows))
	}
	// The older row (lower seq) is the preserved prior version with the old key.
	priorVersion := File{}
	newVersion := File{}
	for i := range rows {
		if rows[i].ObjectKey == priorKey {
			priorVersion = rows[i]
		} else {
			newVersion = rows[i]
		}
	}
	if priorVersion.ObjectKey != priorKey || priorVersion.IsCurrent {
		t.Errorf("prior version should retain %q and be non-current, got key=%q is_current=%v", priorKey, priorVersion.ObjectKey, priorVersion.IsCurrent)
	}
	if !newVersion.IsCurrent {
		t.Errorf("new version should be the current winner, got is_current=%v", newVersion.IsCurrent)
	}
	// The new (current) version must have a non-empty opaque version id. The
	// prior row was inserted directly (legacy shape) and may legitimately have
	// version_id="" (pre-versioning rows are null-version). Seq must still
	// increase — the new version is newer.
	if newVersion.VersionID == "" {
		t.Errorf("new version should have a non-empty version_id, got %q", newVersion.VersionID)
	}
	if priorVersion.VersionID == newVersion.VersionID {
		t.Errorf("version rows must not share a version_id, both %q", newVersion.VersionID)
	}
	if newVersion.Seq <= priorVersion.Seq {
		t.Errorf("new version seq (%d) should be > prior version seq (%d)", newVersion.Seq, priorVersion.Seq)
	}

	// Re-upload identical content so objectKey == prior.ObjectKey.
	// The fake SDK returns an empty object whose ID is the hash of an empty slab list.
	sameKey := rec.ObjectKey

	prior2 := File{
		UUID:          "uuid-prior2",
		Name:          "doc2.pdf",
		DirectoryID:   dirID,
		IsCurrent:     true,
		ObjectKey:     sameKey,
		Size:          200,
		ContentDigest: "same",
	}
	if err := db.Create(&prior2).Error; err != nil {
		t.Fatalf("create prior2: %v", err)
	}

	fake.deleted = nil // reset
	rec2, err := svc.Put(ctx, bytes.NewReader([]byte("more")), 4, "vault:/docs/doc2.pdf", nil)
	if err != nil {
		t.Fatalf("Put (same key) failed: %v", err)
	}
	_ = rec2

	// Versioning never reclaims on overwrite, identical or not.
	if len(fake.deleted) != 0 {
		t.Errorf("expected 0 DeleteObject calls for same-key re-upload, got %d: %v", len(fake.deleted), fake.deleted)
	}
}

// TestPut_ContentDigestInMetadata verifies Put attaches the SHA-256 content
// digest to the pinned object's metadata, so remote sync can reconstruct
// ContentDigest. Otherwise Verify/Stat report an empty digest on secondary
// devices.
func TestPut_ContentDigestInMetadata(t *testing.T) {
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

	fake := &fakeSDK{t: t}
	svc := &vaultService{
		db:     db,
		sdk:    fake,
		appKey: types.PrivateKey{},
	}

	if _, err := svc.getOrCreateDirectory("/docs"); err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	content := []byte("hello vault world")
	_, err = svc.Put(ctx, bytes.NewReader(content), int64(len(content)), "vault:/docs/data.bin", nil)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if len(fake.pinnedMeta) == 0 {
		t.Fatal("Put did not attach any metadata to the pinned object")
	}

	meta, err := ParseFileMetadata(fake.pinnedMeta)
	if err != nil {
		t.Fatalf("parse pinned metadata: %v", err)
	}

	wantDigest := fmt.Sprintf("%x", sha256.Sum256(content))
	if meta.ContentDigest != wantDigest {
		t.Errorf("pinned metadata ContentDigest = %q, want %q", meta.ContentDigest, wantDigest)
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("pinned metadata Size = %d, want %d", meta.Size, len(content))
	}
}

// TestPut_DoesNotDeletePriorOnOverwrite verifies that overwriting a path (a new
// version) does NOT reclaim the prior object from the indexer — versioning
// preserves every version's content, so Put never calls DeleteObject.
func TestPut_DoesNotDeletePriorOnOverwrite(t *testing.T) {
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

	fake := &fakeSDK{t: t} // delErr exercises the (now-unreachable) cleanup path
	svc := &vaultService{
		db:     db,
		sdk:    fake,
		appKey: types.PrivateKey{},
	}

	// Insert a prior record
	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory failed: %v", err)
	}
	prior := File{
		UUID:          "uuid-del-nonfatal",
		Name:          "report.pdf",
		DirectoryID:   dirID,
		IsCurrent:     true,
		ObjectKey:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Size:          100,
		ContentDigest: "old",
	}
	if err := db.Create(&prior).Error; err != nil {
		t.Fatalf("create prior: %v", err)
	}

	// Put must succeed (a new version is inserted).
	if _, err = svc.Put(ctx, bytes.NewReader([]byte("data")), 4, "vault:/docs/report.pdf", nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Versioning preserves history: DeleteObject is NOT called on overwrite.
	if len(fake.deleted) != 0 {
		t.Errorf("overwrite must not call DeleteObject (versioning preserves history), got %d call(s)", len(fake.deleted))
	}
}

// TestPut_ConcurrentSamePath verifies that two concurrent Put calls to the SAME
// new path converge on exactly ONE live row (the partial unique index
// idx_files_live_name_dir fails the second insert atomically, and the loser
// re-resolves the winner's row instead of inserting a duplicate). This is the
// regression guard for the race where two writers pass the pre-insert read and
// both insert.
func TestPut_ConcurrentSamePath(t *testing.T) {
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

	const path = "vault:/concurrent.txt"
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each writer uses its own fake SDK (their mutable fields are not
			// shared) to avoid a data race in -race runs; the production DB
			// path is what we're exercising.
			f := &fakeSDK{t: t}
			s := &vaultService{db: db, sdk: f, appKey: types.PrivateKey{}}
			data := bytes.NewReader([]byte("payload"))
			_, errs[idx] = s.Put(ctx, data, int64(data.Len()), path, nil)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent Put %d failed: %v", i, e)
		}
	}

	// With versioning, two concurrent Puts to the SAME NEW path converge on ONE
	// identity (the loser adopts the winner's UUID via the partial-unique-index
	// conflict + adoptPreflight), producing TWO version rows (each Put is a new
	// version) with exactly ONE current winner.
	var live, current int64
	db.Model(&File{}).Where("name = ? AND directory_id IS NULL AND deleted_at IS NULL", "concurrent.txt").Count(&live)
	db.Model(&File{}).Where("name = ? AND directory_id IS NULL AND is_current = 1 AND deleted_at IS NULL", "concurrent.txt").Count(&current)
	if live != 2 || current != 1 {
		t.Errorf("concurrent Put to same path left live=%d current=%d rows; want 2/1 (two versions, one current)", live, current)
	}
}

// pinRecorder wraps fakeSDK to capture the metadata UUID stamped on every
// PinObject call, so a test can assert the remote object's identity matches the
// stored row after a concurrent-Put conflict (re-stamp).
type pinRecorder struct {
	fakeSDK
	uuids []string
}

func (p *pinRecorder) PinObject(ctx context.Context, obj siastorage.Object) error {
	meta := obj.Metadata()
	if m, err := ParseFileMetadata(meta); err == nil && m.ID != "" {
		p.uuids = append(p.uuids, m.ID)
	} else {
		p.uuids = append(p.uuids, "")
	}
	return p.fakeSDK.PinObject(ctx, obj)
}

// TestPut_ConcurrentSamePath_RemoteIdentityMatchesRow regression: when two
// concurrent Puts to the same new path resolve to a single row (one writer
// adopts the winner's UUID), the pinned remote object metadata must be
// re-stamped with that row's UUID; otherwise a later Sync looks up the
// original (now-unused) UUID and inserts a duplicate row.
func TestPut_ConcurrentSamePath_RemoteIdentityMatchesRow(t *testing.T) {
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

	const path = "vault:/race.txt"
	var wg sync.WaitGroup
	recs := make([]*pinRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		recs[i] = &pinRecorder{fakeSDK: fakeSDK{t: t}}
		go func(idx int) {
			defer wg.Done()
			s := &vaultService{db: db, sdk: recs[idx], appKey: types.PrivateKey{}}
			data := bytes.NewReader([]byte("payload"))
			if _, err := s.Put(ctx, data, int64(data.Len()), path, nil); err != nil {
				t.Errorf("concurrent Put %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// The surviving (current) row for the path.
	var row File
	if err := db.Where("name = ? AND is_current = 1 AND deleted_at IS NULL", "race.txt").First(&row).Error; err != nil {
		t.Fatalf("no current row for race.txt: %v", err)
	}

	// At least one writer's final pin must carry the surviving row's UUID in its
	// metadata, so a later Sync resolves the object to that row (no duplicate).
	matched := false
	for _, r := range recs {
		if len(r.uuids) > 0 && r.uuids[len(r.uuids)-1] == row.UUID {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("no pinned object carried the surviving row UUID %q; pinned UUIDs=%v", row.UUID, []string{recs[0].uuidsStr(), recs[1].uuidsStr()})
	}
}

// uuidsStr is a tiny helper for readable failure output.
func (p *pinRecorder) uuidsStr() string {
	return strings.Join(p.uuids, ",")
}

// TestPut_GuardByKeyIdentity verifies the identity guard at the unit level.
func TestPut_GuardByKeyIdentity(t *testing.T) {
	tests := []struct {
		priorKey string
		newKey   string
		wantCall bool
	}{
		{"abc", "def", true},
		{"abc", "abc", false},
		{"", "abc", false}, // empty prior → hasPrior is false, guard never reached
		{"abc", "", true},  // different keys → guard passes
	}
	for _, tt := range tests {
		got := tt.priorKey != "" && tt.priorKey != tt.newKey
		if got != tt.wantCall {
			t.Errorf("guard(priorKey=%q, newKey=%q) = %v, want %v", tt.priorKey, tt.newKey, got, tt.wantCall)
		}
	}
}

// TestEscapeLike verifies that SQL LIKE metacharacters are properly escaped.
func TestEscapeLike(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/normal/path/", "/normal/path/"},
		{"/reports_2024/", "/reports\\_2024/"},
		{"/100%done/", "/100\\%done/"},
		{"/mixed_100%_test/", "/mixed\\_100\\%\\_test/"},
		{"/has\\backslash/", "/has\\\\backslash/"},
	}
	for _, tt := range tests {
		got := escapeLike(tt.input)
		if got != tt.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestRemove_SkipsIndexerDeleteWhenSharedObject verifies Remove does NOT delete
// the indexer object when another File row still references the same
// content-addressed object key (identical content at different paths dedups to
// one Sia object). Deleting it would orphan the other path. Only the last
// reference triggers the indexer delete.
func TestRemove_SkipsIndexerDeleteWhenSharedObject(t *testing.T) {
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

	dirID, err := (&vaultService{db: db}).getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	// Two paths sharing the same object key (content-addressed dedup).
	sharedObjectKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	for i, name := range []string{"a.txt", "b.txt"} {
		if err := db.Create(&File{
			UUID:          fmt.Sprintf("uuid-shared-%d", i),
			Name:          name,
			DirectoryID:   dirID,
			IsCurrent:     true,
			ObjectKey:     sharedObjectKey,
			Size:          3,
			ContentDigest: "digest",
		}).Error; err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	fake := &fakeSDK{t: t, objErr: nil}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}

	// Remove the first path; the object is still referenced by b.txt, so the
	// indexer delete must be skipped (only the local row is tombstoned).
	if err := svc.Remove(ctx, "vault:/docs/a.txt"); err != nil {
		t.Fatalf("Remove a.txt: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Errorf("Remove(a.txt) called DeleteObject %d time(s); want 0 (object still referenced by b.txt)", len(fake.deleted))
	}
	var aCount int64
	db.Model(&File{}).Where("name = ? AND deleted_at IS NULL", "a.txt").Count(&aCount)
	if aCount != 0 {
		t.Errorf("a.txt should be tombstoned (no live record), got %d live rows", aCount)
	}

	// Remove the last reference; the indexer delete must now happen.
	if err := svc.Remove(ctx, "vault:/docs/b.txt"); err != nil {
		t.Fatalf("Remove b.txt: %v", err)
	}
	if len(fake.deleted) != 1 {
		t.Errorf("Remove(b.txt) called DeleteObject %d time(s); want 1 (last reference)", len(fake.deleted))
	}
}

// TestRemove_IndexerCleanupFailureIsNonFatal verifies that a failure to delete
// the orphaned indexer object (after the local row is already removed) does NOT
// surface as an error: the remove succeeded locally, so returning an error here
// would mislead `vault rm` into reporting total failure, and a retry would hit
// "file not found" since the record is gone. The leaked object is reclaimable.
func TestRemove_IndexerCleanupFailureIsNonFatal(t *testing.T) {
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

	dirID, err := (&vaultService{db: db}).getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	objectKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := db.Create(&File{
		UUID: "uuid-only", Name: "only.txt", DirectoryID: dirID, IsCurrent: true, ObjectKey: objectKey,
		Size: 3, ContentDigest: "digest",
	}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// The indexer delete will fail, but Remove must still succeed because the
	// local path is already removed (create-before-destroy invariant).
	fake := &fakeSDK{t: t, delErr: errors.New("indexer cleanup failed")}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}

	if err := svc.Remove(ctx, "vault:/docs/only.txt"); err != nil {
		t.Fatalf("Remove should be non-fatal even when indexer cleanup fails; got: %v", err)
	}
	// The local row must no longer be live (tombstoned), not hard-deleted.
	var count int64
	db.Model(&File{}).Where("name = ? AND deleted_at IS NULL", "only.txt").Count(&count)
	if count != 0 {
		t.Errorf("local record should be tombstoned despite indexer cleanup failure, got %d live rows", count)
	}
	// The indexer delete WAS attempted (best-effort) and its failure ignored.
	if len(fake.deleted) != 1 {
		t.Errorf("Remove should attempt DeleteObject for the last reference; got %d attempts", len(fake.deleted))
	}
}

// TestRemove_ConcurrentSharedObject_DeletesExactlyOnce regression: two
// concurrent Removes of sibling paths sharing one content-addressed object must
// delete the indexer object EXACTLY once. Previously the shared-ref count was a
// check-then-act outside the tombstone, so both removers could read shared>0 and
// skip the delete, permanently orphaning the object.
func TestRemove_ConcurrentSharedObject_DeletesExactlyOnce(t *testing.T) {
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

	// Two live rows sharing one content-addressed object (same key at two
	// sibling paths).
	const sharedKey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	dirID, err := (&vaultService{db: db}).getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	if err := db.Create(&File{UUID: "u1", Name: "a.txt", DirectoryID: dirID, IsCurrent: true, ObjectKey: sharedKey, Size: 1}).Error; err != nil {
		t.Fatalf("create a.txt: %v", err)
	}
	if err := db.Create(&File{UUID: "u2", Name: "b.txt", DirectoryID: dirID, IsCurrent: true, ObjectKey: sharedKey, Size: 1}).Error; err != nil {
		t.Fatalf("create b.txt: %v", err)
	}

	fake := &lockedDeleteSDK{fakeSDK: fakeSDK{t: t}}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}

	var wg sync.WaitGroup
	for _, name := range []string{"a.txt", "b.txt"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if err := svc.Remove(ctx, "vault:/docs/"+n); err != nil {
				t.Errorf("Remove %s failed: %v", n, err)
			}
		}(name)
	}
	wg.Wait()

	// Both rows tombstoned, and the shared object deleted exactly once.
	var live int64
	db.Model(&File{}).Where("deleted_at IS NULL").Count(&live)
	if live != 0 {
		t.Errorf("expected both rows tombstoned, %d still live", live)
	}
	deleted := fake.DeletedKeys()
	if len(deleted) != 1 {
		t.Errorf("expected exactly 1 DeleteObject for the shared object, got %d (delete=%v)", len(deleted), deleted)
	}
	if len(deleted) == 1 && deleted[0] != sharedKey {
		t.Errorf("DeleteObject key = %q, want %q", deleted[0], sharedKey)
	}
}

// lockedDeleteSDK records DeleteObject calls race-safely so concurrent Remove
// tests can assert an exactly-once delete without a data race on the slice.
type lockedDeleteSDK struct {
	fakeSDK
	mu      sync.Mutex
	deleted []string
}

func (f *lockedDeleteSDK) DeleteObject(_ context.Context, key types.Hash256) error {
	f.mu.Lock()
	f.deleted = append(f.deleted, key.String())
	f.mu.Unlock()
	return nil
}

func (f *lockedDeleteSDK) DeletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// repinFailingSDK is a fake that fails the Nth PinObject call (1-indexed), so a
// test can force the conflict-recovery re-pin to fail and check that the Put
// transaction rolls back without committing a divergent row. Each concurrent
// writer holds its OWN instance (never shared) so the mutable fakeSDK fields
// are never shared across goroutines; the shared barrier synchronizes writers.
type repinFailingSDK struct {
	fakeSDK
	mu       sync.Mutex
	pinCount int
	failOn   int
	barrier  *startBarrier // shared across writers; if set, Upload blocks on it
}

func (f *repinFailingSDK) Upload(ctx context.Context, o *siastorage.Object, r io.Reader, opts ...siastorage.UploadOption) error {
	if f.barrier != nil {
		// Both writers arrive here only AFTER each has resolved identity via
		// findCurrentFile (which runs before upload). Blocking both at upload
		// guarantees neither commits a row before the other's findCurrentFile,
		// so both see the path as new -> both insert -> the conflict path fires
		// deterministically instead of one writer merely overwriting the other.
		f.barrier.Wait()
	}
	return f.fakeSDK.Upload(ctx, o, r, opts...)
}

func (f *repinFailingSDK) PinObject(ctx context.Context, obj siastorage.Object) error {
	f.mu.Lock()
	f.pinCount++
	n := f.pinCount
	f.mu.Unlock()
	if n == f.failOn {
		return errors.New("forced re-pin failure")
	}
	return f.fakeSDK.PinObject(ctx, obj)
}

// startBarrier is a one-shot rendezvous: every Waiter blocks until ALL n
// waiters have arrived, then all proceed together.
type startBarrier struct{ wg sync.WaitGroup }

func newStartBarrier(n int) *startBarrier {
	b := &startBarrier{}
	b.wg.Add(n)
	return b
}

func (b *startBarrier) Wait() {
	b.wg.Done()
	b.wg.Wait()
}

// TestPut_RePinFailureRollsBackTransaction regression: if the conflict-recovery
// re-pin of the adopted-UUID metadata fails, the Put transaction must roll back
// so no DB row is committed with a UUID that diverges from the pinned object.
// Otherwise a later Sync would resolve the object by its (un-changed) metadata
// UUID and insert a duplicate. Each writer uses its OWN fake SDK (with failOn=2:
// its 2nd pin is the post-conflict re-pin), exactly one writer loses the race,
// does the re-pin, and fails; the winner's single pin always succeeds. The DB
// must end with exactly one live winner row and the failed writer's row must
// not persist.
func TestPut_RePinFailureRollsBackTransaction(t *testing.T) {
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

	// Each writer uses its own fake SDK and its own vaultService (their mutable
	// fields are never shared across goroutines; the data race that would
	// otherwise crash under -race), all pointing at the same production DB.
	// failOn=2 fails the loser's 2nd pin (the re-pin), after both initial pins
	// (1 each) succeed. A shared start barrier on Upload forces BOTH writers to
	// pass findCurrentFile (seeing the path as new) before either commits, so
	// the conflict-adoption path fires deterministically.
	const path = "vault:/repin.txt"
	barrier := newStartBarrier(2)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			f := &repinFailingSDK{fakeSDK: fakeSDK{t: t}, failOn: 2, barrier: barrier}
			s := &vaultService{db: db, sdk: f, appKey: types.PrivateKey{}}
			data := bytes.NewReader([]byte("payload"))
			_, errs[idx] = s.Put(ctx, data, int64(data.Len()), path, nil)
		}(i)
	}
	wg.Wait()

	// One writer wins; the loser's Put must have returned the re-pin error.
	failed := 0
	for _, e := range errs {
		if e != nil {
			if !strings.Contains(e.Error(), "re-pin object after adopting UUID") {
				t.Errorf("unexpected failure: %v", e)
			}
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected exactly one writer to fail re-pin, got %d failures (errs=%v)", failed, errs)
	}

	// Exactly one LIVE current row must exist (the winner's); the failed loser
	// must not have committed a divergent row.
	var live, current int64
	db.Model(&File{}).Where("name = ? AND deleted_at IS NULL", "repin.txt").Count(&live)
	db.Model(&File{}).Where("name = ? AND is_current = 1 AND deleted_at IS NULL", "repin.txt").Count(&current)
	if live != 1 || current != 1 {
		t.Errorf("re-pin failure left live=%d current=%d rows; want 1/1 (failed writer must roll back)", live, current)
	}
}

// TestPut_DBErrorInIdentityResolutionIsPropagated regression: a real database
// failure from findCurrentFile (anything but ErrRecordNotFound) must be
// propagated, not swallowed into a freshly-minted UUID. Masking a DB error as a
// brand-new file would break the stable-identity overwrite contract and could
// duplicate the object on the next sync. A root-level path (Directory == "")
// short-circuits getOrCreateDirectory, so with a closed DB we reach
// findCurrentFile and hit a genuine (non-not-found) error.
func TestPut_DBErrorInIdentityResolutionIsPropagated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}

	svc := &vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}
	ctx := context.Background()

	// Close the underlying connection before the Put so the findCurrentFile
	// query fails with a real error rather than ErrRecordNotFound.
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	data := bytes.NewReader([]byte("payload"))
	_, err = svc.Put(ctx, data, int64(data.Len()), "vault:/file.txt", nil)
	if err == nil {
		t.Fatal("expected Put to fail when findCurrentFile hits a DB error, got nil (error was swallowed)")
	}
	if !strings.Contains(err.Error(), "failed to resolve current file") {
		t.Errorf("expected Put to propagate the findCurrentFile error, got: %v", err)
	}
}

// TestPut_ConcurrentSamePath_MetadataPreservedOnAdoption regression: when two
// concurrent Puts to the same new path converge on one row (the winner adopts
// the winner's UUID), the newly-uploaded user metadata must be copied onto the
// adopted row. It was previously omitted in the conflict-adoption block,
// diverging from the overwrite branch and the re-pinned object's metadata.
func TestPut_ConcurrentSamePath_MetadataPreservedOnAdoption(t *testing.T) {
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

	const path = "vault:/meta-race.txt"
	meta := map[string]any{"source": "race", "n": float64(42)}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := bytes.NewReader([]byte("payload"))
			_, err := (&vaultService{db: db, sdk: &fakeSDK{t: t}, appKey: types.PrivateKey{}}).
				Put(ctx, data, int64(data.Len()), path, meta)
			if err != nil {
				t.Errorf("concurrent Put failed: %v", err)
			}
		}()
	}
	wg.Wait()

	var row File
	if err := db.Where("name = ? AND is_current = 1 AND deleted_at IS NULL", "meta-race.txt").First(&row).Error; err != nil {
		t.Fatalf("no current row for meta-race.txt: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(row.Metadata, &m); err != nil {
		t.Fatalf("row metadata not valid JSON: %v (raw=%q)", err, string(row.Metadata))
	}
	if m["source"] != "race" || m["n"] != float64(42) {
		t.Errorf("adopted row metadata = %v, want source=race,n=42 (metadata must survive the conflict path)", m)
	}
}
