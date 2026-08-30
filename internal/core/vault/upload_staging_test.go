package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.sia.tech/core/types"
)

// newStagingService builds a vault service with a staging directory and the
// shared fakeSDK, backed by a temp SQLite cache.
func newStagingService(t *testing.T, uploadsDir string) (*vaultService, *fakeSDK) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	fake := &fakeSDK{t: t}
	return &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}, uploadsDir: uploadsDir}, fake
}

func TestStage_PutReturnsPendingAndStagesBytes(t *testing.T) {
	uploads := t.TempDir()
	svc, fake := newStagingService(t, uploads)

	content := []byte("small but wants to be packed")
	size := int64(len(content))
	got, err := svc.Put(context.Background(), bytes.NewReader(content), size, "vault:/docs/a.txt", map[string]any{})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got.Status != FileStatusPending {
		t.Fatalf("status = %q, want %q", got.Status, FileStatusPending)
	}
	if got.StagedPath == "" {
		t.Fatalf("expected a staged path on the pending file")
	}
	if got.ObjectKey != "" {
		t.Fatalf("pending file should not have an object key yet, got %q", got.ObjectKey)
	}
	// The bytes must be staged to disk, and no Sia interaction happened yet.
	raw, err := os.ReadFile(got.StagedPath)
	if err != nil {
		t.Fatalf("read staged buffer: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("staged bytes differ from input")
	}
	sum := sha256.Sum256(content)
	if got.ContentDigest != hex.EncodeToString(sum[:]) {
		t.Fatalf("content digest = %q, want sha256", got.ContentDigest)
	}
	if fake.uploadCalled || fake.pinCalled {
		t.Fatalf("staging must not touch the SDK (upload=%v pin=%v)", fake.uploadCalled, fake.pinCalled)
	}
}

func TestStage_GetReadsFromStagedBuffer(t *testing.T) {
	uploads := t.TempDir()
	svc, _ := newStagingService(t, uploads)
	content := []byte("hello staged read")
	if _, err := svc.Put(context.Background(), bytes.NewReader(content), int64(len(content)), "vault:/a.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var buf bytes.Buffer
	if err := svc.Get(context.Background(), "vault:/a.txt", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Fatalf("Get from staged buffer mismatch: got %q", buf.String())
	}
}

func TestFlush_PacksPendingAndMarksDurable(t *testing.T) {
	uploads := t.TempDir()
	svc, fake := newStagingService(t, uploads)

	files := []string{"vault:/a.txt", "vault:/b.txt"}
	staged := map[string]string{}
	for i, path := range files {
		content := strings.Repeat("x", i+1)
		if got, err := svc.Put(context.Background(), strings.NewReader(content), int64(len(content)), path, nil); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		} else {
			staged[path] = got.StagedPath
		}
	}

	n, err := svc.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n != len(files) {
		t.Fatalf("Flush flushed %d, want %d", n, len(files))
	}
	if !fake.uploadCalled || !fake.pinCalled {
		t.Fatalf("flush must upload+pin: upload=%v pin=%v", fake.uploadCalled, fake.pinCalled)
	}

	for _, path := range files {
		rec, err := svc.resolveFile(parseTestVault(t, path))
		if err != nil {
			t.Fatalf("resolve %s: %v", path, err)
		}
		if rec.Status != FileStatusOK {
			t.Fatalf("%s status = %q, want ok after flush", path, rec.Status)
		}
		if rec.StagedPath != "" {
			t.Fatalf("%s staged_path not cleared after flush", path)
		}
		if _, serr := os.Stat(staged[path]); !os.IsNotExist(serr) {
			t.Fatalf("staged buffer %s still exists after flush (%v)", staged[path], serr)
		}
	}
	// Flushing again is a no-op.
	if n, err := svc.Flush(context.Background()); err != nil || n != 0 {
		t.Fatalf("second Flush = (%d, %v), want (0, nil)", n, err)
	}
}

func TestFlushPath_ForcesSingleFile(t *testing.T) {
	uploads := t.TempDir()
	svc, _ := newStagingService(t, uploads)

	if _, err := svc.Put(context.Background(), strings.NewReader("x"), 1, "vault:/a.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := svc.FlushPath(context.Background(), "vault:/a.txt"); err != nil {
		t.Fatalf("FlushPath: %v", err)
	}
	rec, err := svc.resolveFile(parseTestVault(t, "vault:/a.txt"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec.Status != FileStatusOK || rec.StagedPath != "" {
		t.Fatalf("after FlushPath: status=%q staged=%q", rec.Status, rec.StagedPath)
	}
}

func TestStage_DiskBackpressure(t *testing.T) {
	uploads := t.TempDir()
	svc, _ := newStagingService(t, uploads)
	svc.diskUsageLimit = 10
	svc.diskUsageTimeout = 50 * time.Millisecond

	// First Put fits within the 10-byte limit (reserves 5 bytes).
	if _, err := svc.Put(context.Background(), strings.NewReader("12345"), 5, "vault:/a.txt", nil); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	// Second Put (5 more) does not exceed reserved(5)+5 <= 10, so it fits.
	if _, err := svc.Put(context.Background(), strings.NewReader("67890"), 5, "vault:/b.txt", nil); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	// A third Put exceeds the total reservation of 10.
	_, err := svc.Put(context.Background(), strings.NewReader("zz"), 2, "vault:/c.txt", nil)
	if !errors.Is(err, ErrSlowDown) {
		t.Fatalf("third Put error = %v, want ErrSlowDown", err)
	}
}

func parseTestVault(t *testing.T, s string) *VaultPath {
	t.Helper()
	vp, err := ParseVaultPath(s)
	if err != nil {
		t.Fatalf("ParseVaultPath(%q): %v", s, err)
	}
	return vp
}
