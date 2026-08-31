//go:build sqlite_fts5

package vault

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
)

// statusFakeSDK is an sdkClient whose Account result is controllable, so the
// Status remote probe can be driven both ways.
type statusFakeSDK struct {
	acc    app.AccountResponse
	accErr error
}

func (f *statusFakeSDK) Account(_ context.Context) (app.AccountResponse, error) {
	return f.acc, f.accErr
}
func (f *statusFakeSDK) AppKey() types.PrivateKey { return types.PrivateKey{} }
func (f *statusFakeSDK) Upload(_ context.Context, _ *siastorage.Object, _ io.Reader, _ ...siastorage.UploadOption) error {
	return nil
}
func (f *statusFakeSDK) UploadPacked(_ ...siastorage.UploadOption) (packedUpload, error) {
	return emptyPackedUpload{}, nil
}
func (f *statusFakeSDK) PinObject(_ context.Context, _ siastorage.Object) error { return nil }
func (f *statusFakeSDK) Object(_ context.Context, _ types.Hash256) (siastorage.Object, error) {
	return siastorage.NewEmptyObject(), nil
}
func (f *statusFakeSDK) ObjectEvents(_ context.Context, _ slabs.Cursor, _ int) ([]siastorage.ObjectEvent, error) {
	return nil, nil
}
func (f *statusFakeSDK) Download(_ siastorage.Object, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	return io.NopCloser(&testReader{}), nil
}
func (f *statusFakeSDK) DeleteObject(_ context.Context, _ types.Hash256) error { return nil }
func (f *statusFakeSDK) CreateSharedObjectURL(_ context.Context, _ types.Hash256, _ time.Time) (string, error) {
	return "", nil
}
func (f *statusFakeSDK) DownloadSharedObject(_ context.Context, _ string, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (f *statusFakeSDK) Close() error { return nil }

type testReader struct{}

func (t *testReader) Read([]byte) (int, error) { return 0, io.EOF }

// statusService builds a vaultService around a temp DB and the given fake SDK.
func statusService(t *testing.T, sdk sdkClient) *vaultService {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return &vaultService{sdk: sdk, db: db, appKey: types.PrivateKey{}}
}

// recordingCloseSDK wraps an sdkClient and fails if Close() is invoked more
// than once. It proves vaultService.Close() is idempotent and does not
// re-invoke sdk.Close() on a second call.
type recordingCloseSDK struct {
	sdkClient
	closeCalls int
	t          *testing.T
}

func (r *recordingCloseSDK) Close() error {
	r.closeCalls++
	if r.closeCalls > 1 {
		r.t.Errorf("sdk.Close() invoked %d times; vaultService.Close() must be idempotent", r.closeCalls)
	}
	return nil
}

// TestClose_Idempotent verifies a second vaultService.Close() does not re-close
// the SDK or the DB handle (the double-close the cache sync-error path triggers
// with an explicit close plus a deferred close).
func TestClose_Idempotent(t *testing.T) {
	rec := &recordingCloseSDK{sdkClient: &statusFakeSDK{}, t: t}
	svc := statusService(t, rec)

	if err := svc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second Close (the deferred one) must be a safe no-op, not a re-close.
	if err := svc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if rec.closeCalls != 1 {
		t.Fatalf("sdk.Close() called %d times, want exactly 1", rec.closeCalls)
	}
}

// TestStatus_RemoteReachable verifies a successful account probe yields real
// live remote + storage data and local cache counts.
func TestStatus_RemoteReachable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	svc := statusService(t, &statusFakeSDK{
		acc: app.AccountResponse{
			Ready:            true,
			PinnedSize:       4096,
			MaxPinnedData:    1 << 30,
			RemainingStorage: 1<<30 - 4096,
		},
	})

	// Seed two live files and one tombstoned/historical row.
	files := []struct {
		name string
		size int64
		cur  bool
		key  string
	}{
		{"a.txt", 100, true, "aa"},
		{"b.txt", 200, true, "bb"},
		{"old.txt", 99999, false, "cc"},
	}
	for i, f := range files {
		if err := svc.db.Create(&File{
			UUID:      uuid.NewString(),
			Name:      f.name,
			IsCurrent: f.cur,
			ObjectKey: f.key,
			Size:      f.size,
		}).Error; err != nil {
			t.Fatalf("seed file %d (%s): %v", i, f.name, err)
		}
	}
	if err := svc.db.Create(&SyncDownCursor{Cursor: "{}", UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	res, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !res.RemoteReachable {
		t.Fatalf("RemoteReachable = false, want true")
	}
	if !res.RemoteReady {
		t.Fatalf("RemoteReady = false, want true")
	}
	if res.RemoteError != "" {
		t.Fatalf("RemoteError = %q, want empty", res.RemoteError)
	}
	if res.StorageUsed != 4096 || res.StorageLimit != 1<<30 || res.RemainingStorage != 1<<30-4096 {
		t.Fatalf("storage wrong: used=%d limit=%d remaining=%d", res.StorageUsed, res.StorageLimit, res.RemainingStorage)
	}
	if res.ObjectsIndexed != 2 {
		t.Fatalf("ObjectsIndexed = %d, want 2", res.ObjectsIndexed)
	}
	if res.TotalBytes != 300 {
		t.Fatalf("TotalBytes = %d, want 300", res.TotalBytes)
	}
	if res.CacheState != "healthy" {
		t.Fatalf("CacheState = %q, want healthy", res.CacheState)
	}
	if res.LastSyncTime != now.Format(time.RFC3339) {
		t.Fatalf("LastSyncTime = %q, want %q", res.LastSyncTime, now.UTC().Format(time.RFC3339))
	}
}

// TestStatus_RemoteUnreachable verifies a failed account probe reports
// unreachable with the error captured, and does not fabricate remote data.
func TestStatus_RemoteUnreachable(t *testing.T) {
	svc := statusService(t, &statusFakeSDK{accErr: errors.New("connection refused")})

	res, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if res.RemoteReachable {
		t.Fatalf("RemoteReachable = true, want false on probe failure")
	}
	if res.RemoteError == "" {
		t.Fatalf("RemoteError empty, want captured probe error")
	}
	if res.RemoteReady {
		t.Fatalf("RemoteReady = true on unreachable, want false")
	}
	// Cache still reports 0/healthy-empty (DB exists but no objects).
	if res.ObjectsIndexed != 0 {
		t.Fatalf("ObjectsIndexed = %d, want 0", res.ObjectsIndexed)
	}
}
