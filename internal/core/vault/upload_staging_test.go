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
	"sync"
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
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}, uploadsDir: uploadsDir}
	// Close the SQLite connection before TempDir cleanup: on Windows a TempDir
	// cannot be removed while its DB file is still open, which failed every
	// staging test on the CI Windows runner.
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return svc, fake
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

// TestDiskBackpressure_WakeOnRelease regression for the diskWake nil-channel
// bug: a Put that is over the disk limit must wake as soon as space is released
// (via a flush), not block the full timeout and then return ErrSlowDown.
func TestDiskBackpressure_WakeOnRelease(t *testing.T) {
	uploads := t.TempDir()
	svc, _ := newStagingService(t, uploads)
	svc.diskUsageLimit = 10
	svc.diskUsageTimeout = 5 * time.Second // long: a broken wake would hang then error

	// First write reserves 5 bytes (fits).
	if _, err := svc.Put(context.Background(), strings.NewReader("aaaaa"), 5, "vault:/a.txt", nil); err != nil {
		t.Fatalf("Put a: %v", err)
	}

	// Second write (6 bytes) exceeds the remaining 5 — it must block until the
	// first is flushed (releasing its 5-byte reservation) and then proceed.
	done := make(chan error, 1)
	go func() {
		_, err := svc.Put(context.Background(), strings.NewReader("bbbbbb"), 6, "vault:/b.txt", nil)
		done <- err
	}()

	// Give the blocked Put a beat to hit the diskWait, then flush the first
	// file to free capacity.
	time.Sleep(50 * time.Millisecond)
	if err := svc.FlushPath(context.Background(), "vault:/a.txt"); err != nil {
		t.Fatalf("FlushPath a: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("blocked Put b returned %v, want nil (should wake on release)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("blocked Put b did not wake after space was released")
	}
}

// TestFlush_ConcurrentNoRace regression for the concurrent Flush/FlushPath race
// on the same pending row: serialized flush work must leave a single durable
// result and never double-remove a staged buffer. Run under -race.
func TestFlush_ConcurrentNoRace(t *testing.T) {
	uploads := t.TempDir()
	svc, _ := newStagingService(t, uploads)
	for i, p := range []string{"vault:/a.txt", "vault:/b.txt"} {
		content := strings.Repeat("x", i+1)
		if _, err := svc.Put(context.Background(), strings.NewReader(content), int64(len(content)), p, nil); err != nil {
			t.Fatalf("Put %s: %v", p, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	wg.Add(4)
	go func() { defer wg.Done(); _, err := svc.Flush(context.Background()); errs <- err }()
	go func() { defer wg.Done(); _, err := svc.Flush(context.Background()); errs <- err }()
	go func() { defer wg.Done(); errs <- svc.FlushPath(context.Background(), "vault:/a.txt") }()
	go func() { defer wg.Done(); errs <- svc.FlushPath(context.Background(), "vault:/b.txt") }()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent flush error: %v", err)
		}
	}

	// Both files must be durable with the staged buffer removed exactly once.
	for _, p := range []string{"vault:/a.txt", "vault:/b.txt"} {
		rec, err := svc.resolveFile(parseTestVault(t, p))
		if err != nil {
			t.Fatalf("resolve %s: %v", p, err)
		}
		if rec.Status != FileStatusOK || rec.StagedPath != "" {
			t.Fatalf("%s after concurrent flush: status=%q staged=%q", p, rec.Status, rec.StagedPath)
		}
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
