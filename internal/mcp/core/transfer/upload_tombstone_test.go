package transfer

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPreparedHandleReportsExpiredAfterPrune verifies a minted-but-never-fulfilled
// (Prepared) handle that lapses and is pruned is later reported as
// UploadStateExpired via its tombstone — NOT the misleading "unknown upload
// handle" that previously made a late upload_status poll indistinguishable from
// a handle that never existed.
func TestPreparedHandleReportsExpiredAfterPrune(t *testing.T) {
	mgr := NewUploadTaskManager(func(_ context.Context, _ io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		return map[string]any{"cid": "QmX"}, nil
	}, time.Hour)
	mgr.PreparedTTL = 5 * time.Minute

	handle, err := mgr.Prepare("idle.bin", mgr.PreparedTTL)
	require.NoError(t, err)

	now := time.Now()
	mgr.mu.Lock()
	mgr.tasks[handle].task.CreatedAt = now.Add(-10 * time.Minute)
	mgr.mu.Unlock()

	// Any status/list/cancel path triggers pruneLocked.
	task, err := mgr.Get(handle)
	require.NoError(t, err, "a known-but-pruned handle must be retrievable from its tombstone")
	require.Equal(t, UploadStateExpired, task.State,
		"a lapsed prepared handle must report expired, not unknown")
	require.Contains(t, task.Err, "expired")

	// Fulfilling the expired handle must refuse with a clear direction rather
	// than the generic unknown error.
	err = mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("x")), 1, "", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")

	// Cancelling it is treated as already-finished, not unknown.
	err = mgr.Cancel(handle)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already finished")
}

// TestCancelFulfilledExpiredHandle verifies Kody finding: a lapsed prepared
// handle that is Fulfilled when nobody has pruned it yet is tombstoned as
// Expired by pruneLocked (inside beginLocked) and then immediately re-added to
// m.tasks as running by the same Fulfill call. Cancel must STILL succeed — it
// must consult the live task before the stale Expired tombstone, not refuse
// with "already finished (state expired)".
func TestCancelFulfilledExpiredHandle(t *testing.T) {
	// Block the executor so the task stays "running" long enough to cancel.
	release := make(chan struct{})
	execRan := make(chan struct{}, 1)
	mgr := NewUploadTaskManager(func(ctx context.Context, _ io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		close(execRan)
		<-release // hold the "running" state until the test cancels
		return map[string]any{"cid": "QmX"}, nil
	}, time.Hour)
	mgr.PreparedTTL = 5 * time.Minute

	handle, err := mgr.Prepare("stale.bin", mgr.PreparedTTL)
	require.NoError(t, err)

	// Backdate the prepared handle so pruneLocked treats it as lapsed.
	mgr.mu.Lock()
	mgr.tasks[handle].task.CreatedAt = time.Now().Add(-10 * time.Minute)
	mgr.mu.Unlock()

	// Fulfill: pruneLocked inside beginLocked tombstones it as Expired, then
	// the same call re-registers the id as running.
	require.NoError(t, mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("x")), 1, "", false))
	<-execRan

	// Both a tombstone AND a live running task now exist under this id.
	mgr.mu.Lock()
	_, hasTomb := mgr.tombstones[handle]
	_, hasLive := mgr.tasks[handle]
	mgr.mu.Unlock()
	require.True(t, hasTomb, "lapsed prepared handle must be tombstoned as expired during Fulfill")
	require.True(t, hasLive, "the same Fulfill re-registers the live running task")

	// Cancel the live upload — this is the regression: it must not be refused
	// because of the coexisting Expired tombstone.
	require.NoError(t, mgr.Cancel(handle), "running upload must be cancellable despite an Expired tombstone under the same id")
	close(release)

	require.Eventually(t, func() bool {
		tk, err := mgr.Get(handle)
		return err == nil && tk.State == UploadStateCancelled
	}, 2*time.Second, 10*time.Millisecond)
}

// TestExpiredTombstonePruned verifies the tombstone map itself is bounded:
// once a tombstone passes the retention window, the handle flips back to
// genuinely unknown.
func TestExpiredTombstonePruned(t *testing.T) {
	mgr := NewUploadTaskManager(func(_ context.Context, _ io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		return map[string]any{"cid": "QmX"}, nil
	}, time.Minute)
	mgr.PreparedTTL = time.Minute

	handle, err := mgr.Prepare("idle.bin", time.Minute)
	require.NoError(t, err)

	// Age it past the prepared TTL so the first Get/prune tombstones it.
	now := time.Now()
	mgr.mu.Lock()
	mgr.tasks[handle].task.CreatedAt = now.Add(-3 * time.Minute)
	mgr.mu.Unlock()
	task, err := mgr.Get(handle)
	require.NoError(t, err)
	require.Equal(t, UploadStateExpired, task.State)

	// Age the tombstone past retention; next prune retires it.
	mgr.mu.Lock()
	if tomb, ok := mgr.tombstones[handle]; ok {
		f := now.Add(-3 * time.Minute)
		tomb.FinishedAt = &f
	}
	delete(mgr.tasks, handle)
	mgr.mu.Unlock()
	mgr.List()

	_, err = mgr.Get(handle)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown upload handle")
}
