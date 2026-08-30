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
