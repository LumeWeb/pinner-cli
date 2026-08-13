package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadTaskManagerLifecycle(t *testing.T) {
	var ran atomic.Int64
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		ran.Add(1)
		b, _ := io.ReadAll(reader)
		return map[string]any{"cid": "QmTest", "bytes": len(b)}, nil
	}, 0)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("hello")), 5, "a.txt", true)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	require.Eventually(t, func() bool {
		t, err := mgr.Get(id)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)

	task, err := mgr.Get(id)
	require.NoError(t, err)
	require.Equal(t, UploadStateCompleted, task.State)
	require.Empty(t, task.Err)
	require.Equal(t, int64(1), ran.Load())
}

func TestUploadTaskManagerFailure(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return nil, errors.New("tus failed")
	}, 0)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("x")), 1, "b.txt", true)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		t, _ := mgr.Get(id)
		return t.State == UploadStateFailed
	}, 2*time.Second, 10*time.Millisecond)

	task, err := mgr.Get(id)
	require.NoError(t, err)
	require.Equal(t, UploadStateFailed, task.State)
	require.Equal(t, "tus failed", task.Err)
}

func TestUploadTaskManagerCancelRunning(t *testing.T) {
	release := make(chan struct{})
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, nil
		}
	}, 0)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("y")), 1, "c.txt", true)
	require.NoError(t, err)

	// Wait for it to start running
	require.Eventually(t, func() bool {
		t, _ := mgr.Get(id)
		return t.State == UploadStateRunning
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, mgr.Cancel(id))

	require.Eventually(t, func() bool {
		t, _ := mgr.Get(id)
		return t.State == UploadStateCancelled
	}, 2*time.Second, 10*time.Millisecond)

	// A cancelled task must carry a FinishedAt so pruneLocked evicts it rather
	// than accumulating cancelled tasks without bound.
	ct, _ := mgr.Get(id)
	require.NotNil(t, ct.FinishedAt)

	// Cancelling again on a terminal state fails
	require.Error(t, mgr.Cancel(id))
}

func TestUploadTaskManagerCancelCompletedFails(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return map[string]any{"cid": "QmZ"}, nil
	}, 0)
	id, _ := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("z")), 1, "d.txt", false)
	require.Eventually(t, func() bool {
		t, _ := mgr.Get(id)
		return t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	require.Error(t, mgr.Cancel(id))
}

func TestUploadTaskManagerListAndUnknown(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmA"}, nil
	}, 0)
	id1, _ := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("1")), 1, "1.txt", false)
	id2, _ := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("2")), 1, "2.txt", false)

	tasks := mgr.List()
	require.Len(t, tasks, 2)

	_, err := mgr.Get("nope")
	require.Error(t, err)
	require.Error(t, mgr.Cancel("nope"))

	// Both should complete; IDs unique
	require.NotEqual(t, id1, id2)
}

func TestAsyncUploadToolsRegistered(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return map[string]any{"cid": "QmB"}, nil
	}, 0)
	descs := NewAsyncUploadTools(mgr)
	require.Len(t, descs, 4)

	names := map[string]bool{}
	for _, d := range descs {
		names[d.Name] = true
	}
	require.True(t, names["upload_file_async"])
	require.True(t, names["upload_status"])
	require.True(t, names["upload_cancel"])
	require.True(t, names["upload_list"])
}

func TestAsyncUploadStatusToolMissingHandle(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return nil, nil
	}, 0)
	descs := NewAsyncUploadTools(mgr)
	var status *ToolDescriptor
	for i := range descs {
		if descs[i].Name == "upload_status" {
			status = &descs[i]
		}
	}
	require.NotNil(t, status)
	_, err := status.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "handle is required")
}

func TestUploadTaskManagerTTLEviction(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmE"}, nil
	}, 50*time.Millisecond)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("e")), 1, "e.txt", false)
	require.NoError(t, err)

	// Wait for completion, then confirm present immediately.
	require.Eventually(t, func() bool {
		t, _ := mgr.Get(id)
		return t != nil && t.State == UploadStateCompleted
	}, 2*time.Second, 5*time.Millisecond)
	require.Len(t, mgr.List(), 1)

	// After TTL passes and a List triggers prune, the terminal task is gone.
	time.Sleep(80 * time.Millisecond)
	require.Empty(t, mgr.List())
	_, err = mgr.Get(id)
	require.Error(t, err)
}

func TestUploadTaskManagerCancelBeforeStartDoesNotRun(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		// A cancelled upload must never fabricate a completion: if the executor
		// observes the cancelled context it reports the cancellation instead of
		// returning a bogus result. The executor blocks on ctx.Done() so the
		// cancel deterministically beats completion — a non-blocking select
		// with default would let the goroutine finish before Cancel() ran,
		// making the race outcome (and this test) scheduling-dependent.
		<-ctx.Done()
		return nil, ctx.Err()
	}, 0)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("c")), 1, "c.txt", false)
	require.NoError(t, err)
	// Cancel immediately after start; the goroutine must observe the cancel and
	// end in the Cancelled state — never a spurious "completed" result.
	require.NoError(t, mgr.Cancel(id))

	require.Eventually(t, func() bool {
		tk, _ := mgr.Get(id)
		return tk != nil && tk.State == UploadStateCancelled
	}, 2*time.Second, 5*time.Millisecond)
	// Give the goroutine a moment to settle; the terminal state must remain
	// Cancelled (the manager's completion path refuses to overwrite a cancel
	// with Completed/Failed once Cancel has been applied), so the executor can
	// never convert a cancelled task into a fabricated completion.
	time.Sleep(50 * time.Millisecond)
	tk, err := mgr.Get(id)
	require.NoError(t, err)
	require.Equal(t, UploadStateCancelled, tk.State)
}

func TestUploadTaskManagerCancelledTasksArePruned(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		// Block on ctx.Done() so the cancel deterministically wins the race
		// against completion — an instant-return executor can finish before
		// Cancel() runs, making mgr.Cancel fail with 'not cancellable (state
		// completed)' and this test scheduling-dependent on CI.
		<-ctx.Done()
		return nil, ctx.Err()
	}, 50*time.Millisecond)

	id, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("c")), 1, "c.txt", false)
	require.NoError(t, err)
	require.NoError(t, mgr.Cancel(id))

	// A cancelled task must carry FinishedAt (regression for the leak where
	// cancelled tasks never got FinishedAt and could therefore never prune).
	task, _ := mgr.Get(id)
	require.NotNil(t, task.FinishedAt)

	// After the TTL (with margin for race-mode slowdown), a List triggers
	// pruneLocked and the cancelled task is gone.
	require.Eventually(t, func() bool {
		return len(mgr.List()) == 0
	}, 2*time.Second, 20*time.Millisecond)
	_, err = mgr.Get(id)
	require.Error(t, err)
}

// countingCloser records how many times its Close was called so a test can
// assert the owned reader is released exactly once despite a cancel race.
type countingCloser struct {
	io.Reader
	closes atomic.Int64
}

func (c *countingCloser) Close() error {
	c.closes.Add(1)
	return nil
}

func TestUploadTaskManagerCancelClosesReaderOnce(t *testing.T) {
	release := make(chan struct{})
	reader := &countingCloser{Reader: strings.NewReader("x")}
	mgr := NewUploadTaskManager(func(ctx context.Context, r io.Reader, size int64, name string, wait bool) (any, error) {
		<-release // hold the executor so Cancel runs while it is reading
		return nil, nil
	}, 0)

	id, err := mgr.Start(context.Background(), reader, 1, "c.txt", true)
	require.NoError(t, err)
	require.NoError(t, mgr.Cancel(id))
	close(release)

	// Both Cancel and the completion goroutine release the reader; the close
	// guard must ensure exactly one Close call, never two.
	require.Eventually(t, func() bool {
		return reader.closes.Load() == 1
	}, 2*time.Second, 5*time.Millisecond)
	ct, _ := mgr.Get(id)
	require.Equal(t, UploadStateCancelled, ct.State)
}

func TestUploadTaskManagerConcurrencyCap(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		started.Add(1)
		<-release // hold tasks in the running state
		return map[string]any{"cid": "QmF"}, nil
	}, 0)
	// Force a tiny cap to test rejection without starting many goroutines.
	mgr.maxActive = 2

	id1, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("1")), 1, "1.txt", false)
	require.NoError(t, err)
	id2, err := mgr.Start(context.Background(), io.NopCloser(strings.NewReader("2")), 1, "2.txt", false)
	require.NoError(t, err)
	// The slot-exhaustion path must release the caller's reader (no leak).
	rejected := &countingCloser{Reader: strings.NewReader("3")}
	_, err = mgr.Start(context.Background(), rejected, 1, "3.txt", false)
	require.ErrorContains(t, err, "too many concurrent")
	require.Equal(t, int64(1), rejected.closes.Load(), "rejected reader should be closed")

	require.NotEmpty(t, id1)
	require.NotEmpty(t, id2)
	close(release)
}

// TestUploadTaskManagerExecTimeoutForcesReaderClose verifies the watchdog:
// when execTimeout elapses and the executor ignores context cancellation, the
// owned reader is still closed (aborting the in-flight read) so the slot and
// HTTP handle are not leaked indefinitely.
func TestUploadTaskManagerExecTimeoutForcesReaderClose(t *testing.T) {
	// An executor that blocks forever, ignoring runCtx cancellation — the
	// worst case a non-cancellable network read.
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		select {}
	}, 0)
	// Force a short execTimeout so the watchdog fires quickly.
	mgr.execTimeout = 50 * time.Millisecond

	reader := &countingCloser{Reader: strings.NewReader("x")}
	_, err := mgr.Start(context.Background(), reader, 1, "c.txt", true)
	require.NoError(t, err)

	// The watchdog must close the reader once the execTimeout elapses, even
	// though the executor never returns.
	require.Eventually(t, func() bool {
		return reader.closes.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)
}
