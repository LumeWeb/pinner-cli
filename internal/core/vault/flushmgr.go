package vault

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// FlushJobQueued .. FlushJobFailed are the lifecycle states of an accepted
	// flush job, reported by FlushManager.Job (and the vault_flush_status tool).
	FlushJobQueued  = "queued"
	FlushJobRunning = "running"
	FlushJobDone    = "done"
	FlushJobFailed  = "failed"
)

// FlushJob is a single accepted flush/drain request for one profile. Agents
// poll it by JobID via FlushManager.Job. Status is one of the FlushJob* values.
type FlushJob struct {
	JobID   string `json:"job_id"`
	Profile string `json:"profile"`
	Path    string `json:"path,omitempty"`
	Status  string `json:"status"` // queued|running|done|failed
	Flushed int    `json:"flushed,omitempty"`
	Error   string `json:"error,omitempty"`

	// done/doneClosed/finalized are synchronization state, guarded by the
	// manager lock. done is closed exactly once when the job reaches a terminal
	// state. finalized marks a job terminally settled by shutdown (its status
	// can no longer be flipped by a worker that returns late, nor its done
	// channel re-closed).
	done       chan struct{}
	doneClosed bool
	finalized  bool
}

// FlushManager runs one flush worker per profile, so two profiles never share a
// pin worker. Each profile worker owns its own VaultService and processes its
// profile's accepted jobs sequentially. Enqueue is non-blocking: it stores the
// job and wakes the profile worker.
type FlushManager struct {
	cfg SyncLoopConfig

	mu      sync.Mutex
	workers map[string]*profileFlushWorker
	pending map[string][]*FlushJob // profile -> queued jobs
	jobs    map[string]*FlushJob
	seq      int64
	closed   bool
	wg       sync.WaitGroup // joins per-profile worker goroutines in Close()

	// ctx/cancel is the manager's own lifetime, decoupled from any single
	// Enqueue request context. Workers run on it so a request returning (and
	// cancelling its ctx) never kills a worker or aborts an in-flight upload;
	// Close() cancels it to stop all per-profile workers.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewFlushManager creates a manager over cfg with a manager-scoped lifecycle
// context. Workers are created lazily on first Enqueue. Close() must be called
// to stop them (it cancels the manager context and releases held services).
func NewFlushManager(cfg SyncLoopConfig) *FlushManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &FlushManager{
		cfg:     cfg,
		workers: map[string]*profileFlushWorker{},
		pending: map[string][]*FlushJob{},
		jobs:    map[string]*FlushJob{},
		ctx:     ctx,
		cancel:  cancel,
	}
}

type profileFlushWorker struct {
	mgr     *FlushManager
	profile string
	wake    chan struct{}
	svc     VaultService

	// active is the job currently being processed (guarded by mgr.mu). It lets
	// Close() fail an in-flight job when a worker is too stuck to finish, so a
	// polling agent's vault_flush_status resolves instead of hanging forever.
	active *FlushJob
}

// flushShutdownTimeout bounds how long Close() waits for worker goroutines to
// observe the cancelled manager context before giving up on a stuck worker (one
// blocked in a non-cancellable Flush/FlushPath, e.g. waiting on the per-profile
// flush lock held by an upload that ignores ctx). Bounding the join guarantees
// MCP shutdown cannot hang forever on a single non-cooperating worker. A var
// (not const) so the bounded-join path is testable at a short duration.
var flushShutdownTimeout = 5 * time.Second

// Enqueue accepts a flush request for profile (path may be empty = flush all
// staged files) and returns an accepted job immediately (non-blocking). The
// request context is NOT threaded into the worker: workers and their uploads
// run on the manager's own context so a returning request cannot cancel an
// in-flight flush or kill the per-profile worker. It is idempotent-safe: it
// records the job and kicks the per-profile worker.
func (m *FlushManager) Enqueue(_ context.Context, profile, path string) (*FlushJob, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrVaultClosed
	}
	m.seq++
	job := &FlushJob{
		JobID:   fmt.Sprintf("flush-%d", m.seq),
		Profile: profile,
		Path:    path,
		Status:  FlushJobQueued,
		done:    make(chan struct{}),
	}
	m.jobs[job.JobID] = job
	m.pending[profile] = append(m.pending[profile], job)
	worker, ok := m.workers[profile]
	m.mu.Unlock()
	if !ok {
		worker = m.ensureWorker(profile)
		if worker == nil {
			// The manager shut down between the closed-check and the worker
			// lookup (Close() cleared workers). The job was already marked
			// failed by Close(); surface the shutdown instead of enqueueing.
			return nil, ErrVaultClosed
		}
	}
	select {
	case worker.wake <- struct{}{}:
	default:
	}
	return job, nil
}

// Job returns the current snapshot of a job by id, or (nil,false) if unknown.
func (m *FlushManager) Job(jobID string) (*FlushJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}
	// copy to avoid a data race on fields mutated after the worker completes
	c := *j
	if j.done != nil {
		c.done = j.done
	}
	return &c, true
}

func (m *FlushManager) ensureWorker(profile string) *profileFlushWorker {
	m.mu.Lock()
	if m.closed {
		// Shutdown sentinel: the manager is closing/closed, so return nil and
		// let Enqueue surface ErrVaultClosed (no new Add-to-WaitGroup after
		// Close's Wait begins).
		m.mu.Unlock()
		return nil
	}
	if w, ok := m.workers[profile]; ok {
		m.mu.Unlock()
		return w
	}
	w := &profileFlushWorker{mgr: m, profile: profile, wake: make(chan struct{}, 1)}
	m.workers[profile] = w
	// Add to the WaitGroup before starting the goroutine and under the same
	// lock that guards the closed check, so Add is never called once Close's
	// Wait has begun.
	m.wg.Add(1)
	m.mu.Unlock()
	// The worker runs on the manager's context, not any request context, so it
	// survives the request that created it and keeps draining its profile's
	// queue until Close() cancels the manager.
	go m.runWorker(w)
	return w
}

func (m *FlushManager) runWorker(w *profileFlushWorker) {
	defer m.wg.Done()
	ctx := m.ctx
	for {
		job := m.popPending(w.profile)
		if job == nil {
			select {
			case <-w.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		m.process(ctx, w, job)
	}
}

func (m *FlushManager) popPending(profile string) *FlushJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.pending[profile]
	if len(q) == 0 {
		return nil
	}
	job := q[0]
	m.pending[profile] = q[1:]
	return job
}

func (m *FlushManager) setStatus(job *FlushJob, status string, flushed int, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A job terminally finalized by shutdown is immutable: a worker that returns
	// late (after Close() timed out on it) must not flip Status back to "done"
	// nor re-close the completion channel.
	if job.finalized {
		return
	}
	job.Status = status
	if status == FlushJobDone {
		job.Flushed = flushed
	}
	job.Error = errMsg
	// Close the completion channel exactly once, when the job reaches a terminal
	// state. process() first calls setStatus(Running) then setStatus(Done|Failed),
	// so closing here unconditionally would double-close the channel and panic.
	if job.done != nil && (status == FlushJobDone || status == FlushJobFailed) && !job.doneClosed {
		job.doneClosed = true
		close(job.done)
	}
}

func (m *FlushManager) setActive(w *profileFlushWorker, job *FlushJob) {
	m.mu.Lock()
	w.active = job
	m.mu.Unlock()
}

func (m *FlushManager) process(ctx context.Context, w *profileFlushWorker, job *FlushJob) {
	m.setActive(w, job)
	defer m.setActive(w, nil)
	m.setStatus(job, FlushJobRunning, 0, "")
	svc, err := w.service(ctx)
	if err != nil {
		m.setStatus(job, FlushJobFailed, 0, err.Error())
		return
	}
	if job.Path != "" {
		if err := svc.FlushPath(ctx, job.Path); err != nil {
			m.setStatus(job, FlushJobFailed, 0, err.Error())
			return
		}
		m.setStatus(job, FlushJobDone, 1, "")
		return
	}
	n, err := svc.Flush(ctx)
	if err != nil {
		m.setStatus(job, FlushJobFailed, 0, err.Error())
		return
	}
	m.setStatus(job, FlushJobDone, n, "")
}

func (w *profileFlushWorker) service(ctx context.Context) (VaultService, error) {
	// w.svc is read and written under the manager lock so Close() (which reads
	// it under the same lock) never observes a torn value or a service assigned
	// after shutdown.
	w.mgr.mu.Lock()
	if w.svc != nil {
		w.mgr.mu.Unlock()
		return w.svc, nil
	}
	closed := w.mgr.closed
	w.mgr.mu.Unlock()
	if closed {
		return nil, ErrVaultClosed
	}
	svc, err := w.mgr.cfg.Service(w.profile)
	if err != nil {
		return nil, err
	}
	w.mgr.mu.Lock()
	if w.mgr.closed {
		// Shut down while the service was being built: never publish it, close
		// it immediately, and report shutdown so the caller stops cleanly.
		w.mgr.mu.Unlock()
		_ = svc.Close()
		return nil, ErrVaultClosed
	}
	w.svc = svc
	w.mgr.mu.Unlock()
	return svc, nil
}

// Close stops every per-profile worker and releases held services. It marks
// still-queued jobs failed (so agents polling vault_flush_status never hang),
// cancels the manager context, and joins each worker goroutine — bounded by
// flushShutdownTimeout so a worker stuck in a non-cancellable Flush (ignoring
// the cancelled context) cannot hang MCP shutdown forever. Prevents further
// Enqueue. Safe to call more than once.
func (m *FlushManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	workers := m.workers
	m.workers = map[string]*profileFlushWorker{}
	// Mark queued (never-started) jobs failed so a polling agent's
	// vault_flush_status resolves instead of hanging on "queued" forever.
	for _, q := range m.pending {
		for _, j := range q {
			j.Status = FlushJobFailed
			j.Error = "flush manager closed"
			if j.done != nil && !j.doneClosed {
				j.doneClosed = true
				close(j.done)
			}
		}
	}
	m.pending = map[string][]*FlushJob{}
	cancel := m.cancel
	m.mu.Unlock()

	cancel()
	// Join in-flight workers BEFORE closing their services, so a worker can
	// never be mid-process() touching a concurrently closed service. Bound the
	// join: a worker blocked on a non-cooperating upload (e.g. the per-profile
	// flush lock held by a Flush that ignores ctx) never observes the cancelled
	// context, so we give up after flushShutdownTimeout rather than hang.
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		// Every worker exited: it is safe to close the services it used (no
		// worker is mid-process() touching one).
		for _, w := range workers {
			if w.svc != nil {
				_ = w.svc.Close()
			}
		}
	case <-time.After(flushShutdownTimeout):
		// A worker is stuck in a live service (e.g. blocked on the per-profile
		// flush lock). Permanently fail its job so polls resolve, but do NOT
		// close the worker's service here — that would dispose the DB/SDK while
		// the worker is still mid-Flush (panic/fail on a live service). The
		// service is left for process exit to reclaim; the finalized job is
		// immutable so a late-returning worker cannot flip it back to done.
		m.failStuckJobs(workers)
	}
}

// failStuckJobs permanently fails any in-flight job of a worker that failed to
// observe the cancelled manager context, then closes its completion channel.
// The job is marked finalized so, if the stuck worker ever returns, its
// setStatus call is a no-op: it can neither flip the job back to done nor
// double-close the channel.
func (m *FlushManager) failStuckJobs(workers map[string]*profileFlushWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range workers {
		if w.active == nil {
			continue
		}
		j := w.active
		if j.done != nil && !j.doneClosed {
			j.doneClosed = true
			close(j.done)
		}
		if j.finalized {
			continue
		}
		j.finalized = true
		j.Status = FlushJobFailed
		if j.Error == "" {
			j.Error = "flush manager shutdown timed out"
		}
	}
}

// The flush manager is registered process-wide (like the existing process-global
// flushLocks in upload_staging.go) so the CLI owns its SyncLoopConfig (service
// factory + profile source) while the MCP server, which drives the catalog
// surface and cannot import internal/cli, can still close it on shutdown.
var (
	globalFlushMu  sync.Mutex
	globalFlushMgr *FlushManager
	globalFlushSet bool
)

// RegisterFlushManager installs the process-wide per-profile flush manager with
// the given SyncLoopConfig and returns it. It is idempotent: the first call
// takes effect and later calls return the existing manager, so concurrent
// vault_flush / vault_send invocations never double-start per-profile workers.
func RegisterFlushManager(cfg SyncLoopConfig) *FlushManager {
	globalFlushMu.Lock()
	defer globalFlushMu.Unlock()
	if globalFlushSet {
		return globalFlushMgr
	}
	globalFlushMgr = NewFlushManager(cfg)
	globalFlushSet = true
	return globalFlushMgr
}

// GlobalFlushManager returns the registered process-wide flush manager, or nil
// if none has been registered (callers fall back to a detached single-use
// goroutine). It is the getter wired into the catalog ops.
func GlobalFlushManager() *FlushManager {
	globalFlushMu.Lock()
	defer globalFlushMu.Unlock()
	return globalFlushMgr
}

// CloseFlushManager closes and clears the process-wide flush manager, releasing
// every held per-profile VaultService, so a long-running MCP server does not
// leak SDK/DB handles. A later Register installs a fresh one. Safe to call when
// nothing is registered or more than once.
func CloseFlushManager() {
	globalFlushMu.Lock()
	mgr := globalFlushMgr
	globalFlushMgr = nil
	globalFlushSet = false
	globalFlushMu.Unlock()
	if mgr != nil {
		mgr.Close()
	}
}
