package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// UploadExecutor runs an authenticated upload. It is an alias for the
// vendor-agnostic UploadHandler; this name exists for the async path's caller.
// Pinner owns the auth and the underlying TUS path; the upload source may be
// any stream the caller provides.

// DefaultUploadName is the fallback upload name used across every file-input
// tool (upload_data, upload_file, async uploads) and the CLI's local-path
// handlers when the caller supplies no explicit name. Exported so users and
// the CLI layer can reference the same default.
const DefaultUploadName = "upload"

// UploadTaskState is the lifecycle state of an async upload handle.
type UploadTaskState string

// defaultMaxActiveUploads caps how many queued/running async uploads the
// manager will track at once, guarding against an MCP caller flooding the
// server with in-flight uploads (each holds an open reader plus goroutine).
const defaultMaxActiveUploads = 8

const (
	UploadStateQueued    UploadTaskState = "queued"
	UploadStateRunning   UploadTaskState = "running"
	UploadStateCompleted UploadTaskState = "completed"
	UploadStateFailed    UploadTaskState = "failed"
	UploadStateCancelled UploadTaskState = "cancelled"
)

// UploadTask describes a tracked async upload.
type UploadTask struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	State      UploadTaskState `json:"state"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Result     any             `json:"result,omitempty"`
	Err        string          `json:"error,omitempty"`
}

type trackedTask struct {
	task   *UploadTask
	cancel context.CancelFunc
	// reader is the owned io.ReadCloser (e.g. a network-backed HTTP body).
	// Cancel closes it so an in-flight read is aborted, not just the context.
	reader io.Closer
	// closeOnce guarantees the owned reader is closed exactly once even when
	// Cancel and the completion goroutine race (http bodies are not guaranteed
	// to be idempotent on Close).
	closeOnce sync.Once
}

// closeReader closes the owned reader exactly once from either the Cancel path
// or the completion path. Callers do not need to hold m.mu.
func (t *trackedTask) closeReader() {
	t.closeOnce.Do(func() {
		if t.reader != nil {
			_ = t.reader.Close()
		}
		t.reader = nil
	})
}

// UploadTaskManager tracks in-flight async uploads by opaque handle. It is
// safe for concurrent use. Uploads run the existing synchronous authenticated
// upload path in a background goroutine; cancellation cancels that context.
type UploadTaskManager struct {
	mu        sync.Mutex
	tasks     map[string]*trackedTask
	exec      UploadExecutor
	ttl       time.Duration
	maxActive int
	// execTimeout is the hard upper bound on a single async upload's lifetime.
	// A hung executor (network/TUS stall that ignores context cancellation)
	// must not occupy a maxActive slot forever, or a handful of stuck uploads
	// could exhaust every slot and block all future async uploads (DoS).
	execTimeout time.Duration
}

// defaultExecTimeout bounds a single async upload when no explicit timeout is
// configured. It is intentionally generous — it only caps a genuinely hung
// upload (one whose transport ignores context cancellation), which is the
// scenario that would otherwise leak a maxActive slot indefinitely.
const defaultExecTimeout = 30 * time.Minute

// NewUploadTaskManager creates a manager bound to an executor.
func NewUploadTaskManager(exec UploadExecutor, ttl time.Duration) *UploadTaskManager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &UploadTaskManager{
		tasks:       make(map[string]*trackedTask),
		exec:        exec,
		ttl:         ttl,
		maxActive:   defaultMaxActiveUploads,
		execTimeout: defaultExecTimeout,
	}
}

func newTaskID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start begins an async upload and returns an opaque handle. The manager owns
// the supplied io.ReadCloser: it is read by the background executor and closed
// by the manager when the task finishes, so callers must not close it. The
// upload runs on a context detached from the caller's deadline/cancellation
// (context.WithoutCancel) so it outlives the MCP request that started it, while
// retaining the caller's context values; only an explicit Cancel() aborts it.
func (m *UploadTaskManager) Start(ctx context.Context, reader io.ReadCloser, size int64, name string, wait bool) (string, error) {
	if m.exec == nil {
		// No executor wired: never began ownership, but release the reader so
		// the caller's contractual "Start owns it" holds on every return path.
		_ = reader.Close()
		return "", errors.New("upload executor is not configured")
	}
	if name == "" {
		name = DefaultUploadName
	}
	id := newTaskID()
	task := &UploadTask{
		ID:        id,
		Name:      name,
		State:     UploadStateQueued,
		CreatedAt: time.Now(),
	}
	// Preserve the caller's context values (tracing, auth, logging) while
	// dropping the deadline and cancellation that would otherwise abort the
	// async work once the request that started it returns. A hard execTimeout
	// still bounds the work so a hung executor is force-cancelled and its
	// maxActive slot freed instead of leaking.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.execTimeout)
	m.mu.Lock()
	m.pruneLocked()
	if err := m.acquireSlotLocked(); err != nil {
		m.mu.Unlock()
		cancel()
		// Start owns the reader; release it on the no-slot path so a
		// network-backed body is not leaked when the concurrency cap is hit.
		_ = reader.Close()
		return "", err
	}
	tt := &trackedTask{task: task, cancel: cancel, reader: reader}
	m.tasks[id] = tt
	m.mu.Unlock()

	go func() {
		now := time.Now()
		m.mu.Lock()
		// Bail if the task was cancelled between Start() returning the handle
		// and this goroutine acquiring the lock; never begin an upload that
		// was already aborted. Cancel() already closed the reader, and this
		// early path never closes it.
		if task.State != UploadStateQueued {
			m.mu.Unlock()
			return
		}
		task.StartedAt = &now
		task.State = UploadStateRunning
		m.mu.Unlock()
		// Watchdog: when execTimeout elapses, force-abandon the owned reader
		// (aborting an in-flight network read that ignores context
		// cancellation) and cancel runCtx. Without this, an executor blocked
		// in a non-cancellable read would never return and would leak both the
		// reader and the maxActive slot forever, defeating the slot guard.
		// AfterFunc fires exactly once; Stop below disarms it on normal return.
		watchdog := time.AfterFunc(m.execTimeout, func() {
			tt.closeReader()
			cancel()
		})
		result, err := m.exec(runCtx, reader, size, name, wait)
		// The work finished before the timeout; disarm the watchdog so it
		// cannot close the reader afterwards (it would otherwise, on a very
		// slow-but-finished path, race the normal completion close).
		if !watchdog.Stop() {
			// The watchdog already fired (timeout raced the return): release
			// the reader exactly once here too, since the completion close
			// below is idempotent via closeOnce.
			tt.closeReader()
		}
		finished := time.Now()
		m.mu.Lock()
		cancelled := task.State == UploadStateCancelled
		if !cancelled {
			task.Result = result
			task.FinishedAt = &finished
			if err != nil {
				if errors.Is(err, context.Canceled) {
					task.State = UploadStateCancelled
					task.Err = ""
				} else {
					task.State = UploadStateFailed
					task.Err = err.Error()
				}
			} else {
				task.State = UploadStateCompleted
			}
		}
		m.mu.Unlock()
		// Release the owned reader (idempotent via closeOnce): on normal
		// completion the goroutine is the owner; if Cancel raced us it already
		// closed it via the same guard and this is a no-op.
		tt.closeReader()
		cancel()
	}()

	return id, nil
}

// cloneTask deep-copies the time pointers so a returned UploadTask does not
// share mutable StartedAt/FinishedAt memory with the live tracked task (whose
// goroutine writes them under m.mu). The Result interface value is treated as
// read-only after completion and is shared as-is.
func cloneTask(src *UploadTask) *UploadTask {
	c := *src
	if src.StartedAt != nil {
		v := *src.StartedAt
		c.StartedAt = &v
	}
	if src.FinishedAt != nil {
		v := *src.FinishedAt
		c.FinishedAt = &v
	}
	return &c
}

// Get returns a copy of the task for the given handle, or an error if unknown.
func (m *UploadTaskManager) Get(id string) (*UploadTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mirror List: evict terminal tasks past TTL so an agent that only polls
	// status/cancel (not List) does not let completed tasks accumulate forever.
	m.pruneLocked()
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("unknown upload handle %q", id)
	}
	return cloneTask(t.task), nil
}

// Cancel cancels a running/queued task. Cancelling a completed task is a no-op
// that returns an error so callers can surface it.
func (m *UploadTaskManager) Cancel(id string) error {
	m.mu.Lock()
	t, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown upload handle %q", id)
	}
	switch t.task.State {
	case UploadStateQueued, UploadStateRunning:
		now := time.Now()
		t.task.State = UploadStateCancelled
		// Mark the task finished so pruneLocked eventually evicts it; without
		// this the goroutine's own completion path skips setting FinishedAt for
		// cancelled tasks and they would accumulate without bound.
		t.task.FinishedAt = &now
		cancel := t.cancel
		m.mu.Unlock()
		cancel()
		// Close the owned reader so an in-flight network read is aborted
		// immediately rather than letting the upload drain to completion in
		// the background after the task reports cancelled. closeReader is
		// idempotent, so if the goroutine is already finishing it wins and this
		// is a no-op, never a concurrent double-close.
		t.closeReader()
		return nil
	default:
		state := t.task.State
		m.mu.Unlock()
		return fmt.Errorf("upload %q is not cancellable (state %s)", id, state)
	}
}

// List returns all tracked tasks ordered by creation time, pruned of terminal
// tasks older than the TTL.
func (m *UploadTaskManager) List() []*UploadTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	out := make([]*UploadTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, cloneTask(t.task))
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.Before(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// acquireSlotLocked returns an error if the manager is at its concurrent-upload
// cap. Caller must hold m.mu; only queued/running (non-terminal) tasks count.
func (m *UploadTaskManager) acquireSlotLocked() error {
	if m.maxActive <= 0 {
		return nil
	}
	active := 0
	for _, t := range m.tasks {
		switch t.task.State {
		case UploadStateQueued, UploadStateRunning:
			active++
			if active >= m.maxActive {
				return errors.New("too many concurrent async uploads")
			}
		}
	}
	return nil
}

// pruneLocked removes terminal (completed/failed/cancelled) tasks older than
// the TTL. Caller must hold m.mu. Terminal tasks are kept at least TTL after
// they finish so callers can still fetch status/result within the window.
func (m *UploadTaskManager) pruneLocked() {
	if m.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-m.ttl)
	for id, t := range m.tasks {
		switch t.task.State {
		case UploadStateCompleted, UploadStateFailed, UploadStateCancelled:
			if t.task.FinishedAt != nil && t.task.FinishedAt.Before(cutoff) {
				delete(m.tasks, id)
			}
		}
	}
}
