package vault

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

// waitFlushTerminal polls a job until it reaches a terminal state (done/failed)
// or the deadline elapses. It returns the job snapshot; a nil result indicates
// the job never became terminal in time (the regression signal).
func waitFlushTerminal(t *testing.T, mgr *FlushManager, jobID string, d time.Duration) *FlushJob {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if job, ok := mgr.Job(jobID); ok {
			if job.Status == FlushJobDone || job.Status == FlushJobFailed {
				return job
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// TestFlushWorkerSurvivesRequestContext is a regression test for the Kody
// finding that the per-profile flush worker was bound to the requesting
// Enqueue context: when the initiating request returned and cancelled its
// context, the worker died while still registered in m.workers, leaving every
// subsequent job queued forever (and in-flight uploads cancelled mid-flight).
//
// It enqueues a first job under a request context it immediately cancels, then
// enqueues a second job afterwards. The worker must drain BOTH jobs to a
// terminal state because it runs on the manager's own context, not the request
// context. Under the old code the second job would stay "queued" forever.
func TestFlushWorkerSurvivesRequestContext(t *testing.T) {
	svc := NewMockVaultService(t)
	svc.On("Flush", mock.Anything).Return(1, nil)
	svc.On("Close").Return(nil).Maybe()

	mgr := NewFlushManager(SyncLoopConfig{
		Profiles: func() []string { return []string{"alpha"} },
		Service:  func(string) (VaultService, error) { return svc, nil },
	})
	defer mgr.Close()

	// First request: enqueue a job, then cancel its context as the request
	// returns. This must NOT kill the worker.
	reqCtx, cancel := context.WithCancel(context.Background())
	job1, err := mgr.Enqueue(reqCtx, "alpha", "")
	if err != nil {
		t.Fatalf("Enqueue(first): %v", err)
	}
	cancel()

	// Second request (fresh context) enqueued after the first context was
	// cancelled. The worker must still be alive to drain it.
	job2, err := mgr.Enqueue(context.Background(), "alpha", "")
	if err != nil {
		t.Fatalf("Enqueue(second): %v", err)
	}

	if got := waitFlushTerminal(t, mgr, job1.JobID, 2*time.Second); got == nil {
		t.Fatalf("first job %s never reached a terminal state (worker likely bound to a cancelled context)", job1.JobID)
	}
	got2 := waitFlushTerminal(t, mgr, job2.JobID, 2*time.Second)
	if got2 == nil {
		t.Fatalf("second job %s never reached a terminal state: the worker did not survive the cancelled request context", job2.JobID)
	}
	if got2.Status != FlushJobDone {
		t.Fatalf("second job = %+v, want done (mock Flush returns success)", got2)
	}
	svc.AssertNumberOfCalls(t, "Flush", 2)
}

// TestFlushManagerCloseDrainsQueuedJobsAndJoinsWorkers is a regression test for
// the Kody finding that Close() cancelled the manager context and immediately
// closed every worker's service without joining the workers, leaving queued
// jobs permanently undrained (agents polling vault_flush_status hang) and a
// use-after-close/data-race window.
//
// The first job occupies the worker (its Flush blocks on the manager context),
// so the second job stays queued. Close() must mark the queued job failed (so a
// poll resolves), join the in-flight worker (the blocking Flush returns when the
// manager context cancels), and close the service only afterwards — verified
// here by Close() returning promptly and the queued job reaching "failed".
func TestFlushManagerCloseDrainsQueuedJobsAndJoinsWorkers(t *testing.T) {
	svc := NewMockVaultService(t)
	// Flush call count is scheduling-dependent (the worker may not have reached
	// it before Close() cancels), so it must not be asserted strictly.
	svc.On("Flush", mock.Anything).
		Run(func(args mock.Arguments) { <-args.Get(0).(context.Context).Done() }).
		Return(0, context.Canceled).
		Maybe()
	svc.On("Close").Return(nil).Maybe()

	mgr := NewFlushManager(SyncLoopConfig{
		Profiles: func() []string { return []string{"alpha"} },
		Service:  func(string) (VaultService, error) { return svc, nil },
	})

	job1, err := mgr.Enqueue(context.Background(), "alpha", "")
	if err != nil {
		t.Fatalf("Enqueue(job1): %v", err)
	}
	job2, err := mgr.Enqueue(context.Background(), "alpha", "")
	if err != nil {
		t.Fatalf("Enqueue(job2): %v", err)
	}

	done := make(chan struct{})
	go func() { mgr.Close(); close(done) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close() did not return: it did not join the in-flight worker")
	}

	// The queued job must be failed, never left as "queued" for a polling agent.
	j2, ok := mgr.Job(job2.JobID)
	if !ok {
		t.Fatalf("queued job %s missing from registry after Close", job2.JobID)
	}
	if j2.Status != FlushJobFailed {
		t.Fatalf("queued job after Close = %q, want %q (drained so polls resolve)", j2.Status, FlushJobFailed)
	}
	// The in-flight job reached a terminal state too.
	j1, ok := mgr.Job(job1.JobID)
	if !ok || (j1.Status != FlushJobDone && j1.Status != FlushJobFailed) {
		t.Fatalf("running job after Close = %+v, want a terminal state", j1)
	}
}

// TestFlushManagerCloseBoundsStuckWorker is a regression test for the Kody
// finding that Close() blocked indefinitely on m.wg.Wait() when a worker was
// stuck in a non-cancellable Flush (one that ignores the cancelled manager
// context, e.g. waiting on a lock held by an upload that doesn't honor ctx),
// leaving the job permanently "running" and hanging MCP shutdown. Close() must
// bound the join and fail the stuck job so shutdown and polls resolve.
func TestFlushManagerCloseBoundsStuckWorker(t *testing.T) {
	oldTimeout := flushShutdownTimeout
	flushShutdownTimeout = 50 * time.Millisecond
	defer func() { flushShutdownTimeout = oldTimeout }()

	never := make(chan struct{}) // never closed: Flush ignores the context forever
	svc := NewMockVaultService(t)
	svc.On("Flush", mock.Anything).
		Run(func(mock.Arguments) { <-never }).
		Return(0, nil).
		Maybe()
	svc.On("Close").Return(nil).Maybe()

	mgr := NewFlushManager(SyncLoopConfig{
		Profiles: func() []string { return []string{"alpha"} },
		Service:  func(string) (VaultService, error) { return svc, nil },
	})
	job, err := mgr.Enqueue(context.Background(), "alpha", "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Close must return promptly (bounded by flushShutdownTimeout), not hang on
	// the stuck worker.
	start := time.Now()
	mgr.Close()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Close() took %v: it hung on the stuck worker instead of bounding the join", elapsed)
	}

	// The in-flight stuck job must be failed so vault_flush_status resolves.
	j, ok := mgr.Job(job.JobID)
	if !ok {
		t.Fatalf("job %s missing after Close", job.JobID)
	}
	if j.Status != FlushJobFailed {
		t.Fatalf("stuck job after Close = %q, want %q (failed so a poll resolves)", j.Status, FlushJobFailed)
	}
}
