package vault

import (
	"context"
	"fmt"
	"sync"
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

	done chan struct{}
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
	seq     int64
	closed  bool

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
}

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
	if w, ok := m.workers[profile]; ok {
		m.mu.Unlock()
		return w
	}
	w := &profileFlushWorker{mgr: m, profile: profile, wake: make(chan struct{}, 1)}
	m.workers[profile] = w
	m.mu.Unlock()
	// The worker runs on the manager's context, not any request context, so it
	// survives the request that created it and keeps draining its profile's
	// queue until Close() cancels the manager.
	go m.runWorker(w)
	return w
}

func (m *FlushManager) runWorker(w *profileFlushWorker) {
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
	job.Status = status
	if status == FlushJobDone {
		job.Flushed = flushed
	}
	job.Error = errMsg
	// Only close the completion channel once, when the job reaches a terminal
	// state. process() first calls setStatus(Running) then setStatus(Done|Failed),
	// so closing here unconditionally would double-close the channel and panic.
	if job.done != nil && (status == FlushJobDone || status == FlushJobFailed) {
		close(job.done)
	}
}

func (m *FlushManager) process(ctx context.Context, w *profileFlushWorker, job *FlushJob) {
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
	if w.svc != nil {
		return w.svc, nil
	}
	svc, err := w.mgr.cfg.Service(w.profile)
	if err != nil {
		return nil, err
	}
	w.svc = svc
	return svc, nil
}

// Close releases every per-profile worker's VaultService, cancels the manager
// context (stopping each worker goroutine), and prevents further Enqueue. Safe
// to call more than once.
func (m *FlushManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	workers := m.workers
	m.workers = map[string]*profileFlushWorker{}
	cancel := m.cancel
	m.mu.Unlock()
	// Cancel after dropping the lock so in-flight workers draining their queue
	// can still observe jobs; the selected ctx is the manager's own.
	cancel()
	for _, w := range workers {
		if w.svc != nil {
			_ = w.svc.Close()
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
