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
	// the same call re-registers the id as running and drops the now-stale
	// tombstone. Only the live task must remain — no expired tombstone may
	// coexist with an active upload.
	require.NoError(t, mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("x")), 1, "", false))
	<-execRan

	mgr.mu.Lock()
	_, hasTomb := mgr.tombstones[handle]
	_, hasLive := mgr.tasks[handle]
	orderHas := false
	for _, id := range mgr.tombstoneOrder {
		if id == handle {
			orderHas = true
			break
		}
	}
	mgr.mu.Unlock()
	require.True(t, hasLive, "the same Fulfill re-registers the live running task")
	require.False(t, hasTomb, "the stale Expired tombstone must be dropped when the handle becomes live again")
	require.False(t, orderHas, "the re-registered id must leave the tombstone FIFO")

	// Cancel the live upload — this is the regression: it must succeed, not be
	// refused by a stale Expired tombstone under the same id.
	require.NoError(t, mgr.Cancel(handle), "running upload must be cancellable despite a prior Expired tombstone under the same id")
	close(release)

	require.Eventually(t, func() bool {
		tk, err := mgr.Get(handle)
		return err == nil && tk.State == UploadStateCancelled
	}, 2*time.Second, 10*time.Millisecond)
}

// TestReRegisteredHeadDoesNotStrandSuccessors verifies Kody finding: the FIFO
// retirement assumes tombstoneOrder is monotonic by FinishedAt. A lapsed
// prepared handle re-registered live AFTER another tombstone was queued would
// get a young FinishedAt at an OLD FIFO slot (if its old slot was retained),
// breaking the monotonic head assumption and stranding successors. With the
// root-cause fix (untombstoneLocked drops the stale tombstone on re-register),
// the FIFO stays truthful and all expired tombstones retire.
func TestReRegisteredHeadDoesNotStrandSuccessors(t *testing.T) {
	mgr := NewUploadTaskManager(func(_ context.Context, _ io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		return map[string]any{"cid": "QmX"}, nil
	}, time.Minute)
	mgr.PreparedTTL = time.Minute

	// Prepare two handles: A (re-registered later) and B (a normal successor).
	a, err := mgr.Prepare("a.bin", time.Minute)
	require.NoError(t, err)
	b, err := mgr.Prepare("b.bin", time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, a, b)

	// Tombstone both by aging them past the prepared TTL and pruning. A is
	// queued first so it sits at the head of the FIFO.
	now := time.Now()
	mgr.mu.Lock()
	mgr.tasks[a].task.CreatedAt = now.Add(-3 * time.Minute)
	mgr.tasks[b].task.CreatedAt = now.Add(-3 * time.Minute)
	mgr.mu.Unlock()
	mgr.List() // prunes + tombstones both

	// Re-register A as a live running upload (simulating Fulfill of the lapsed
	// handle). untombstoneLocked drops its stale Expired tombstone.
	mgr.mu.Lock()
	tt := &trackedTask{task: &UploadTask{ID: a, Name: "a.live", State: UploadStateRunning, CreatedAt: now}}
	mgr.tasks[a] = tt
	mgr.untombstoneLocked(a)
	// Age all remaining tombstones (including B) past retention.
	for id := range mgr.tombstones {
		if tt2, ok := mgr.tombstones[id]; ok {
			f := time.Now().Add(-3 * time.Minute)
			tt2.FinishedAt = &f
		}
	}
	mgr.mu.Unlock()

	// The head is now a's LIVE re-registered id, NOT a young expired tombstone.
	// Run a prune: B and any aged tombstones must retire despite the FIFO head
	// no longer being expired.
	mgr.List()

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	require.NotContains(t, mgr.tombstones, b, "successor tombstone B must retire past retention")
	_, aLive := mgr.tasks[a]
	require.True(t, aLive, "live re-registered task A must be untouched by tombstone retirement")
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
