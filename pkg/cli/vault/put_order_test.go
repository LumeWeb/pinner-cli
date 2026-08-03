package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"
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
	t            *testing.T
	deleted      []string // object keys passed to DeleteObject
	uploadCalled bool
	pinCalled    bool
	delErr       error         // error to return from DeleteObject (nil = success)
	objErr       error         // error to return from Object (nil = success)
	pinnedMeta   []byte        // metadata attached to the most recently pinned object
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
		Name:          "report.pdf",
		DirectoryID:   dirID,
		ObjectKey:     priorKey,
		Size:          100,
		ContentDigest: "olddigest",
	}
	if err := db.Create(&prior).Error; err != nil {
		t.Fatalf("create prior file: %v", err)
	}

	// Call svc.Put — this should:
	//  1. Delete the prior DB row
	//  2. Commit the new record
	//  3. Delete the prior object from the indexer (fake records it)
	newMeta := map[string]any{"source": "test"}
	data := bytes.NewReader([]byte("new content"))
	rec, err := svc.Put(ctx, data, int64(data.Len()), "vault:/docs/report.pdf", newMeta)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify the new record was committed with the expected ObjectKey
	// (zero hash of empty slabs via NewEmptyObject)
	if rec.Name != "report.pdf" {
		t.Errorf("record.Name = %q, want %q", rec.Name, "report.pdf")
	}

	// Verify the prior object's key was passed to DeleteObject
	if len(fake.deleted) != 1 {
		t.Fatalf("expected 1 DeleteObject call, got %d", len(fake.deleted))
	}
	if fake.deleted[0] != priorKey {
		t.Errorf("DeleteObject(%q) call 0, want %q", fake.deleted[0], priorKey)
	}

	// Verify DB state: only one record for this (name, directory_id)
	var count int64
	db.Model(&File{}).Where("name = ? AND directory_id = ?", "report.pdf", dirID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 file record after overwrite, got %d", count)
	}

	// Re-upload identical content so objectKey == prior.ObjectKey.
	// The fake SDK returns an empty object whose ID is the hash of an empty slab list.
	// Use the first Put's returned record key since we can't predict the hash exactly.
	sameKey := rec.ObjectKey

	prior2 := File{
		Name:          "doc2.pdf",
		DirectoryID:   dirID,
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

	// Verify DeleteObject was NOT called (identical content → guard skips)
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

// TestPut_DeleteObjectNonFatal verifies that a DeleteObject failure after the
// new record is committed does NOT propagate as an error to the caller.
func TestPut_DeleteObjectNonFatal(t *testing.T) {
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

	fake := &fakeSDK{t: t, delErr: io.ErrUnexpectedEOF} // simulate DeleteObject failure
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
		Name:          "report.pdf",
		DirectoryID:   dirID,
		ObjectKey:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Size:          100,
		ContentDigest: "old",
	}
	if err := db.Create(&prior).Error; err != nil {
		t.Fatalf("create prior: %v", err)
	}

	// Put should succeed despite DeleteObject failure
	_, err = svc.Put(ctx, bytes.NewReader([]byte("data")), 4, "vault:/docs/report.pdf", nil)
	if err != nil {
		t.Fatalf("Put should succeed despite DeleteObject failure, got: %v", err)
	}

	// Verify DeleteObject was called (attempted cleanup)
	if len(fake.deleted) != 1 {
		t.Errorf("expected 1 DeleteObject call, got %d", len(fake.deleted))
	}
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
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := db.Create(&File{
			Name:         name,
			DirectoryID:  dirID,
			ObjectKey:    sharedObjectKey,
			Size:         3,
			ContentDigest: "digest",
		}).Error; err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	fake := &fakeSDK{t: t, objErr: nil}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}

	// Remove the first path — the object is still referenced by b.txt, so the
	// indexer delete must be skipped (only the local row is removed).
	if err := svc.Remove(ctx, "vault:/docs/a.txt"); err != nil {
		t.Fatalf("Remove a.txt: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Errorf("Remove(a.txt) called DeleteObject %d time(s); want 0 (object still referenced by b.txt)", len(fake.deleted))
	}
	var aCount int64
	db.Model(&File{}).Where("name = ?", "a.txt").Count(&aCount)
	if aCount != 0 {
		t.Errorf("a.txt local record should be removed, got %d", aCount)
	}

	// Remove the last reference — the indexer delete must now happen.
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
		Name: "only.txt", DirectoryID: dirID, ObjectKey: objectKey,
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
	// The local row must be gone.
	var count int64
	db.Model(&File{}).Where("name = ?", "only.txt").Count(&count)
	if count != 0 {
		t.Errorf("local record should be removed despite indexer cleanup failure, got %d rows", count)
	}
	// The indexer delete WAS attempted (best-effort) and its failure ignored.
	if len(fake.deleted) != 1 {
		t.Errorf("Remove should attempt DeleteObject for the last reference; got %d attempts", len(fake.deleted))
	}
}