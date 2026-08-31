//go:build sqlite_fts5

package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
)


// makeSharedObject builds a slabs.SharedObject + 32-byte data key for tests.
// The shared slabs are self-consistent: PinObject on NewUnsafeObject will
// accept them under the fakeSDK.
func makeSharedObject(t *testing.T) (slabs.SharedObject, []byte) {
	t.Helper()
	obj := siastorage.NewEmptyObject()
	dataKey := obj.UnsafeDataKey()
	return slabs.SharedObject{Slabs: obj.Slabs()}, dataKey[:]
}

// TestShareAcceptPinsCopyAndLedgers verifies the slab-reference pin primitive:
// given a share URL whose slab metadata is served by sharedObjectFn,
// ShareAccept pins the slab references into the accepting profile, creates a
// local DB row, and appends a write-only audit row to the share ledger. No
// content is downloaded.
func TestShareAcceptPinsCopyAndLedgers(t *testing.T) {
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
	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })
	svc.indexerURL = "https://indexer.example.com"

	sharedObj, encKey := makeSharedObject(t)
	svc.sharedObjectFn = func(_ context.Context, _ string) (slabs.SharedObject, []byte, error) {
		return sharedObj, encKey, nil
	}

	f, err := svc.ShareAccept(ctx, "vault:/docs/shared.txt", "https://indexer.example.com/objects/abc/shared#encryption_key=dGVzdA", "alice", nil)
	if err != nil {
		t.Fatalf("ShareAccept failed: %v", err)
	}
	if f == nil {
		t.Fatal("ShareAccept returned nil file")
	}
	if f.Name != "shared.txt" {
		t.Fatalf("accepted file name = %q, want shared.txt", f.Name)
	}

	if !fake.pinCalled {
		t.Fatal("PinObject was not called during ShareAccept")
	}

	if len(fake.pinnedMeta) == 0 {
		t.Fatal("PinObject did not capture any object metadata")
	}
	var sealedMeta FileMetadata
	if err := json.Unmarshal(fake.pinnedMeta, &sealedMeta); err != nil {
		t.Fatalf("unmarshal sealed metadata: %v", err)
	}
	if sealedMeta.ID == "" {
		t.Fatal("sealed metadata is missing file ID (UUID)")
	}

	var ledger []ShareLedger
	if err := db.Find(&ledger).Error; err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	if len(ledger) != 1 {
		t.Fatalf("share ledger has %d rows, want 1", len(ledger))
	}
	if ledger[0].TargetPrincipal != "alice" {
		t.Fatalf("ledger target_principal = %q, want alice", ledger[0].TargetPrincipal)
	}
	if ledger[0].SharedVaultPath != "vault:/docs/shared.txt" {
		t.Fatalf("ledger shared_vault_path = %q, want vault:/docs/shared.txt", ledger[0].SharedVaultPath)
	}
	if ledger[0].ObjectKey == "" {
		t.Fatal("ledger object_key is empty")
	}
}

// TestShareAcceptDuplicateReturnsExisting verifies that if the accepting
// profile already has a file with the same object key (same slabs), the accept
// is a no-op and returns the existing file.
// TestShareAcceptAcceptsAtRequestedPath verifies that accepting a share whose
// object is already pinned elsewhere still mints a fresh row at the REQUESTED
// destination path referencing the same object key. The accept must not report
// success at the requested path while pointing at a pre-existing file at
// another path.
func TestShareAcceptAcceptsAtRequestedPath(t *testing.T) {
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
	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })

	// The share URL must match the indexer origin (scheme + host) that the
	// service is wired to, or ShareAccept rejects it as a likely SSRF before
	// ever resolving the share. indexer.example.com is chosen to match a
	// service with that indexer host.
	svc.indexerURL = "https://indexer.example.com"

	obj := siastorage.NewEmptyObject()
	dataKey := obj.UnsafeDataKey()
	sharedObj := slabs.SharedObject{Slabs: obj.Slabs()}
	svc.sharedObjectFn = func(_ context.Context, _ string) (slabs.SharedObject, []byte, error) {
		return sharedObj, dataKey[:], nil
	}

	f, err := svc.ShareAccept(ctx, "vault:/docs/accepted.txt", "https://indexer.example.com/objects/x/shared#encryption_key=dGVzdA", "bob", nil)
	if err != nil {
		t.Fatalf("ShareAccept failed: %v", err)
	}
	if f == nil {
		t.Fatal("ShareAccept returned nil file")
	}
	if f.Name != "accepted.txt" {
		t.Fatalf("accepted file name = %q, want accepted.txt (the requested path)", f.Name)
	}
	// The requested path must now resolve as the created file.
	got, err := svc.Stat(ctx, "vault:/docs/accepted.txt")
	if err != nil {
		t.Fatalf("Stat accepted path failed: %v", err)
	}
	if got.Name != "accepted.txt" {
		t.Fatalf("stat name = %q, want accepted.txt", got.Name)
	}
}

// TestShareAcceptRewritesForeignOrigin verifies the SSRF guard: a share URL
// with a foreign host is rewritten to the configured indexer origin before
// it reaches SharedObject. The injected sharedObjectFn captures the resolved
// URL so we can assert the host was rewritten, not rejected.
func TestShareAcceptRewritesForeignOrigin(t *testing.T) {
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
	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })
	svc.indexerURL = "https://indexer.example.com"

	sharedObj, encKey := makeSharedObject(t)
	var capturedURL string
	svc.sharedObjectFn = func(_ context.Context, resolved string) (slabs.SharedObject, []byte, error) {
		capturedURL = resolved
		return sharedObj, encKey, nil
	}

	// Foreign host + wrong scheme — must be rewritten, not rejected.
	if _, err := svc.ShareAccept(ctx, "vault:/docs/f.txt", "http://127.0.0.1:8080/objects/x/shared#encryption_key=dGVzdA", "", nil); err != nil {
		t.Fatalf("ShareAccept with foreign host failed: %v", err)
	}
	if capturedURL == "" {
		t.Fatal("sharedObjectFn was not called (share URL not resolved)")
	}
	if !strings.Contains(capturedURL, "indexer.example.com") {
		t.Fatalf("resolved URL host was not rewritten to indexer origin: %s", capturedURL)
	}
	if strings.Contains(capturedURL, "127.0.0.1") {
		t.Fatalf("foreign host leaked into resolved URL: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "#encryption_key=") {
		t.Fatalf("encryption key fragment was lost during rewrite: %s", capturedURL)
	}
}


// TestShareAcceptRejectsDirAndEmptyURL verifies the input guards.
func TestShareAcceptRejectsDirAndEmptyURL(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.ShareAccept(ctx, "vault:/docs/", "https://x", "", nil); err == nil {
		t.Fatal("ShareAccept to a directory should error")
	}
	if _, err := svc.ShareAccept(ctx, "vault:/docs/f.txt", "", "", nil); err == nil {
		t.Fatal("ShareAccept with empty share_url should error")
	}
}

// TestShareAcceptTagsApplied verifies that tags passed to ShareAccept are
// sealed into the object metadata and reconciled to the local tag index.
func TestShareAcceptTagsApplied(t *testing.T) {
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
	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })
	svc.indexerURL = "https://indexer.example.com"

	sharedObj, encKey := makeSharedObject(t)
	svc.sharedObjectFn = func(_ context.Context, _ string) (slabs.SharedObject, []byte, error) {
		return sharedObj, encKey, nil
	}

	tags := []string{"shared", "important"}
	_, err = svc.ShareAccept(ctx, "vault:/docs/tagged.txt", "https://indexer.example.com/objects/y/shared#encryption_key=dGVzdA", "", map[string]any{"tags": tags})
	if err != nil {
		t.Fatalf("ShareAccept with tags failed: %v", err)
	}

	var sealedMeta FileMetadata
	if err := json.Unmarshal(fake.pinnedMeta, &sealedMeta); err != nil {
		t.Fatalf("unmarshal sealed metadata: %v", err)
	}
	metaTagsRaw, _ := sealedMeta.Metadata["tags"].([]any)
	if len(metaTagsRaw) != 2 {
		t.Fatalf("sealed metadata tags = %v, want 2 items", metaTagsRaw)
	}

	tags2, err := svc.TagList(ctx)
	if err != nil {
		t.Fatalf("TagList failed: %v", err)
	}
	if len(tags2) != 2 {
		t.Fatalf("tag list = %v, want 2 tags", tags2)
	}

	// Regression: the local File row must carry the same normalized metadata
	// (tags) as the sealed object's metadata map, or sync-down would
	// reconstruct a diverging row with un-normalized tags.
	var row File
	if err := db.Where("name = ?", "tagged.txt").First(&row).Error; err != nil {
		t.Fatalf("load File row: %v", err)
	}
	sealedMetaJSON, err := json.Marshal(sealedMeta.Metadata)
	if err != nil {
		t.Fatalf("marshal sealed metadata map: %v", err)
	}
	if !bytes.Equal(row.Metadata, sealedMetaJSON) {
		t.Fatalf("local row metadata diverges from sealed object metadata:\nrow=%s\nsealed=%s", row.Metadata, sealedMetaJSON)
	}
}
