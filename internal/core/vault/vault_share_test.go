package vault

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"go.sia.tech/core/types"
)

// TestShareAcceptPinsCopyAndLedgers verifies the A2A copy-once primitive: given
// a share URL whose shared content is served by the SDK, ShareAccept downloads
// it, pins a NEW self-contained copy into the accepting profile at the target
// path, and appends a write-only audit row to the share ledger. The original
// path must NOT be touched (no reference sharing).
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
	fake := &fakeSDK{t: t, shareContent: []byte("shared-content-from-another-profile")}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })

	// Put an original file so there is a source to compare against (the share is
	// of a DIFFERENT object, but we assert copy-once semantics: accept does not
	// overwrite an existing path).
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("original")), 8, "vault:/docs/orig.txt", nil); err != nil {
		t.Fatalf("Put original failed: %v", err)
	}

	// Accept the share into a new path.
	f, err := svc.ShareAccept(ctx, "vault:/docs/shared.txt", "sia://some-share-url", "alice")
	if err != nil {
		t.Fatalf("ShareAccept failed: %v", err)
	}
	if f == nil {
		t.Fatal("ShareAccept returned nil file")
	}
	if f.Name != "shared.txt" {
		t.Fatalf("accepted file name = %q, want shared.txt", f.Name)
	}

	// The copy must contain the shared content (fakeSDK.DownloadSharedObject
	// served shareContent; Put uploaded those exact bytes).
	var got bytes.Buffer
	if err := svc.Cat(ctx, "vault:/docs/shared.txt", &got); err != nil {
		t.Fatalf("Cat accepted copy failed: %v", err)
	}
	if got.String() != "shared-content-from-another-profile" {
		t.Fatalf("accepted copy content = %q, want shared content", got.String())
	}

	// The streamed accept must record the true shared byte count (reconciled
	// after the pipe drains), not the 0 placeholder passed to Put.
	if f.Size != int64(len("shared-content-from-another-profile")) {
		t.Fatalf("accepted file size = %d, want %d", f.Size, len("shared-content-from-another-profile"))
	}

	// The original file row must be untouched (ShareAccept pins a NEW object —
	// it never overwrites an existing path).
	origStat, err := svc.Stat(ctx, "vault:/docs/orig.txt")
	if err != nil {
		t.Fatalf("Stat original failed: %v", err)
	}
	if origStat.Name != "orig.txt" {
		t.Fatalf("original file name = %q, want orig.txt", origStat.Name)
	}

	// The accepted file is a DISTINCT row with its own UUID — not a reference
	// to the original's row. (ObjectKey comparison is meaningless under the
	// fakeSDK, whose deterministic object ID is identical for every upload; the
	// real SDK assigns a content-unique ID from the upload's slab root.)
	var origID string
	if err := db.Table("files").Where("name = ?", "orig.txt").Pluck("uuid", &origID).Error; err != nil {
		t.Fatalf("query original uuid: %v", err)
	}
	var accID string
	if err := db.Table("files").Where("name = ?", "shared.txt").Pluck("uuid", &accID).Error; err != nil {
		t.Fatalf("query accepted uuid: %v", err)
	}
	if origID == "" || accID == "" {
		t.Fatalf("expected distinct uuid rows, got orig=%q acc=%q", origID, accID)
	}
	if origID == accID {
		t.Fatal("accepted copy shares the original's file row UUID — must be a brand-new file")
	}

	// A write-only audit row must have been appended to the share ledger.
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

// TestShareAcceptRejectsDirAndEmptyURL verifies the input guards.
func TestShareAcceptRejectsDirAndEmptyURL(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.ShareAccept(ctx, "vault:/docs/", "sia://x", ""); err == nil {
		t.Fatal("ShareAccept to a directory should error")
	}
	if _, err := svc.ShareAccept(ctx, "vault:/docs/f.txt", "", ""); err == nil {
		t.Fatal("ShareAccept with empty share_url should error")
	}
}
