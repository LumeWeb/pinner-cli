package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func newVerifyTestService(t *testing.T) (*vaultService, *fakeSDK, []byte) {
	t.Helper()
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	content := []byte("hello world verify test")
	fake := &fakeSDK{t: t, shareContent: content}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	svc.indexerURL = "https://indexer.example.com"
	t.Cleanup(func() { _ = svc.Close() })

	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	objectKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	row := File{
		UUID:        "uuid-verify",
		Name:        "charter.md",
		DirectoryID: dirID,
		IsCurrent:   true,
		ObjectKey:   objectKey,
		Size:        int64(len(content)),
		MediaType:   "text/markdown",
		Status:      FileStatusOK,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create file row: %v", err)
	}

	// Use the shared object to set up pinned metadata so Object() returns it.
	obj := siastorage.NewEmptyObject()
	meta := FileMetadata{
		ID:        "uuid-verify",
		Name:      "charter.md",
		Directory: "/docs",
		Size:      int64(len(content)),
		Status:    FileStatusOK,
	}
	metaJSON, _ := meta.JSON()
	obj.UpdateMetadata(metaJSON)
	fake.PinObject(ctx, obj)

	return svc, fake, content
}

func TestVerifyShallow_NoDigest_NotApplicable(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newVerifyTestService(t)

	res, err := svc.Verify(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.ObjectExists {
		t.Fatal("ObjectExists should be true")
	}
	if res.DigestMatch != nil {
		t.Fatal("DigestMatch should be nil (no verdict) when no digest is recorded")
	}
	// After accept/send there is no recorded digest until first get/decrypt/
	// deep verify; the report must be neutral "not_applicable", NOT
	// "unverified" which agents read as a pin failure.
	if res.DigestVerified != DigestVerifiedNotApplicable {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedNotApplicable)
	}
}

func TestVerifyShallow_DigestMatch(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	digest := sha256Hex(content)

	dirID, _ := svc.getDirectoryID("/docs")
	if err := svc.db.Model(&File{}).
		Where("name = ? AND directory_id = ?", "charter.md", dirID).
		Update("content_digest", digest).Error; err != nil {
		t.Fatalf("update digest: %v", err)
	}

	// Update the fake's pinned metadata to include the digest.
	obj := siastorage.NewEmptyObject()
	meta := FileMetadata{ContentDigest: digest}
	metaJSON, _ := meta.JSON()
	obj.UpdateMetadata(metaJSON)
	fake.pinnedMeta = metaJSON

	res, err := svc.Verify(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if res.DigestMatch == nil || !*res.DigestMatch {
		t.Fatal("DigestMatch should be true")
	}
	if res.DigestVerified != DigestVerifiedVerified {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedVerified)
	}
}

// TestVerifyShallow_CacheMiss_Unverified is a regression: when a digest is
// recorded locally but the object's sealed metadata carries no digest to
// compare (cold cache), shallow verify must report "unverified", NOT
// "mismatch" — "mismatch" is only valid when two real hashes exist and differ.
func TestVerifyShallow_CacheMiss_Unverified(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	// A digest is recorded on the local row...
	digest := sha256Hex(content)
	dirID, _ := svc.getDirectoryID("/docs")
	if err := svc.db.Model(&File{}).
		Where("name = ? AND directory_id = ?", "charter.md", dirID).
		Update("content_digest", digest).Error; err != nil {
		t.Fatalf("update digest: %v", err)
	}
	// ...but the object's sealed metadata carries NO digest (cold cache).
	obj := siastorage.NewEmptyObject()
	meta := FileMetadata{ContentDigest: ""}
	metaJSON, _ := meta.JSON()
	obj.UpdateMetadata(metaJSON)
	fake.pinnedMeta = metaJSON

	res, err := svc.Verify(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.ObjectExists {
		t.Fatal("ObjectExists should be true")
	}
	if res.DigestVerified != DigestVerifiedUnverified {
		t.Fatalf("DigestVerified = %q, want %q (cache miss is unverified, not mismatch)", res.DigestVerified, DigestVerifiedUnverified)
	}
	if res.DigestMatch != nil {
		t.Fatalf("DigestMatch = %v, want nil (no verdict on cache miss)", res.DigestMatch)
	}
}

func TestVerifyDeep_NoDigest_Backfills(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	pinCountBefore := fake.pinCount

	res, err := svc.VerifyDeep(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("VerifyDeep failed: %v", err)
	}
	if !res.ObjectExists {
		t.Fatal("ObjectExists should be true")
	}
	if res.DigestMatch == nil || !*res.DigestMatch {
		t.Fatal("DigestMatch should be true after deep verify backfill")
	}
	if res.DigestVerified != DigestVerifiedVerified {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedVerified)
	}

	wantDigest := sha256Hex(content)
	if res.ContentDigest != wantDigest {
		t.Fatalf("ContentDigest = %q, want %q", res.ContentDigest, wantDigest)
	}

	if fake.pinCount <= pinCountBefore {
		t.Fatal("PinObject should have been called to backfill the digest")
	}

	dirID, _ := svc.getDirectoryID("/docs")
	var row File
	if err := svc.db.Where("name = ? AND directory_id = ?", "charter.md", dirID).First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.ContentDigest != wantDigest {
		t.Fatalf("local row ContentDigest = %q, want %q", row.ContentDigest, wantDigest)
	}

	var sealedMeta FileMetadata
	if err := json.Unmarshal(fake.pinnedMeta, &sealedMeta); err != nil {
		t.Fatalf("unmarshal pinned metadata: %v", err)
	}
	if sealedMeta.ContentDigest != wantDigest {
		t.Fatalf("sealed object ContentDigest = %q, want %q", sealedMeta.ContentDigest, wantDigest)
	}
}

func TestVerifyDeep_DigestMatch(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	digest := sha256Hex(content)

	dirID, _ := svc.getDirectoryID("/docs")
	if err := svc.db.Model(&File{}).
		Where("name = ? AND directory_id = ?", "charter.md", dirID).
		Update("content_digest", digest).Error; err != nil {
		t.Fatalf("update digest: %v", err)
	}

	obj := siastorage.NewEmptyObject()
	meta := FileMetadata{ContentDigest: digest}
	metaJSON, _ := meta.JSON()
	obj.UpdateMetadata(metaJSON)
	fake.pinnedMeta = metaJSON

	res, err := svc.VerifyDeep(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("VerifyDeep failed: %v", err)
	}
	if res.DigestMatch == nil || !*res.DigestMatch {
		t.Fatal("DigestMatch should be true")
	}
	if res.DigestVerified != DigestVerifiedVerified {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedVerified)
	}
}

func TestVerifyDeep_DigestMismatch(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	// Record a wrong digest on the row.
	dirID, _ := svc.getDirectoryID("/docs")
	if err := svc.db.Model(&File{}).
		Where("name = ? AND directory_id = ?", "charter.md", dirID).
		Update("content_digest", "deadbeef").Error; err != nil {
		t.Fatalf("update digest: %v", err)
	}

	// Stamp the same wrong digest on the object so shallow would match (but
	// deep won't).
	obj := siastorage.NewEmptyObject()
	meta := FileMetadata{ContentDigest: "deadbeef"}
	metaJSON, _ := meta.JSON()
	obj.UpdateMetadata(metaJSON)
	fake.pinnedMeta = metaJSON

	res, err := svc.VerifyDeep(ctx, "vault:/docs/charter.md")
	if err != nil {
		t.Fatalf("VerifyDeep failed: %v", err)
	}
	if res.DigestMatch == nil || *res.DigestMatch {
		t.Fatal("DigestMatch should be false on mismatch")
	}
	if res.DigestVerified != DigestVerifiedMismatch {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedMismatch)
	}

	wantDigest := sha256Hex(content)
	if res.ContentDigest != "deadbeef" {
		t.Fatalf("ContentDigest should still be the recorded value, got %q, want deadbeef", res.ContentDigest)
	}
	_ = wantDigest
}

func TestGet_BackfillsDigest(t *testing.T) {
	ctx := context.Background()
	svc, fake, content := newVerifyTestService(t)

	pinCountBefore := fake.pinCount

	var buf bytes.Buffer
	if err := svc.Get(ctx, "vault:/docs/charter.md", &buf); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Fatalf("downloaded content mismatch: got %q, want %q", buf.String(), string(content))
	}

	if fake.pinCount <= pinCountBefore {
		t.Fatal("PinObject should have been called to backfill the digest")
	}

	wantDigest := sha256Hex(content)
	dirID, _ := svc.getDirectoryID("/docs")
	var row File
	if err := svc.db.Where("name = ? AND directory_id = ?", "charter.md", dirID).First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.ContentDigest != wantDigest {
		t.Fatalf("local row ContentDigest = %q, want %q", row.ContentDigest, wantDigest)
	}

	var sealedMeta FileMetadata
	if err := json.Unmarshal(fake.pinnedMeta, &sealedMeta); err != nil {
		t.Fatalf("unmarshal pinned metadata: %v", err)
	}
	if sealedMeta.ContentDigest != wantDigest {
		t.Fatalf("sealed object ContentDigest = %q, want %q", sealedMeta.ContentDigest, wantDigest)
	}

	// A second Get should NOT trigger another backfill (digest is now populated).
	pinCountAfter := fake.pinCount
	buf.Reset()
	_ = svc.Get(ctx, "vault:/docs/charter.md", &buf)
	if fake.pinCount != pinCountAfter {
		t.Fatalf("second Get should not trigger backfill (pinCount was %d, now %d)", pinCountAfter, fake.pinCount)
	}
}

// TestVerify_PendingFile_Unverified is a regression: a freshly staged (pending)
// file has an empty ObjectKey and no Sia object yet. Verify (shallow and deep)
// must report "unverified"/pending — NOT throw "invalid object key" — and must
// not try to download a non-existent object.
func TestVerify_PendingFile_Unverified(t *testing.T) {
	ctx := context.Background()
	svc, fake, _ := newVerifyTestService(t)

	dirID, err := svc.getOrCreateDirectory("/staging")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	row := File{
		UUID:        "uuid-pending",
		Name:        "put-probe.txt",
		DirectoryID: dirID,
		IsCurrent:   true,
		// ObjectKey is intentionally empty: the bytes are staged locally and the
		// background flush / vault_flush will upload+pin them later.
		ObjectKey:     "",
		Size:          4,
		MediaType:     "text/plain",
		ContentDigest: sha256Hex([]byte("data")),
		Status:        FileStatusPending,
		StagedPath:    t.TempDir() + "/pending-buffer",
	}
	if err := svc.db.Create(&row).Error; err != nil {
		t.Fatalf("create pending file row: %v", err)
	}

	// Deep verify must not attempt a download for a pending object: no Sia
	// object exists yet, so Download would fail. All we can report is pending.
	res, err := svc.VerifyDeep(ctx, "vault:/staging/put-probe.txt")
	if err != nil {
		t.Fatalf("VerifyDeep on pending file must not error, got: %v", err)
	}
	if !res.Pending {
		t.Fatal("res.Pending should be true for a staged file")
	}
	if res.ObjectExists {
		t.Fatal("ObjectExists should be false (no Sia object yet)")
	}
	if res.DigestVerified != DigestVerifiedUnverified {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedUnverified)
	}

	// Shallow verify reports the same pending state.
	res, err = svc.Verify(ctx, "vault:/staging/put-probe.txt")
	if err != nil {
		t.Fatalf("Verify on pending file must not error, got: %v", err)
	}
	if !res.Pending {
		t.Fatal("res.Pending should be true for a staged file")
	}
	if res.DigestVerified != DigestVerifiedUnverified {
		t.Fatalf("DigestVerified = %q, want %q", res.DigestVerified, DigestVerifiedUnverified)
	}

	// Deep verify should never have downloaded content for the pending file.
	if fake.downloadCalled {
		t.Fatal("pending file must not be downloaded")
	}
}
