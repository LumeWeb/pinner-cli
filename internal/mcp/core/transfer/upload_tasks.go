package transfer

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

// defaultPreparedTTL bounds how long an unfulfilled Prepared task (a canonical
// upload operation minted but never supplied bytes) is retained before prune.
// It is deliberately SHORT — as short as the presigned endpoint's own default
// TTL (DefaultHTTPUploadTTL) — so a prepared handle that is never fulfilled is
// evicted about when its corresponding PUT token expires, instead of lingering
// for the full terminal-task TTL (15m) that exists so callers can still fetch
// the RESULT of a finished upload. Prepared tasks hold no bytes and no slot, so
// there is nothing worth keeping beyond the endpoint window.
const defaultPreparedTTL = DefaultHTTPUploadTTL

// defaultMaxPrepared caps how many outstanding (Prepared, unfulfilled) canonical
// upload operations the manager will hold at once. This guards against a caller
// flooding mint/prepare requests: each Prepare allocates a task + a token with
// no bytes, so without a bound an attacker could accumulate unbounded Prepared
// handles within the retention window even though they bypass the MaxActive
// (reader/goroutine) cap. It is independent of MaxActive because prepared
// handles are intentionally light (no slot), and is tuned well above the
// realistic concurrency of in-flight uploads.
const defaultMaxPrepared = 64

const (
	// UploadStatePrepared marks an upload operation whose handle/task exists
	// but whose bytes have not been supplied yet. It is created up front by
	// Prepare so the model-facing upload_file and the App's file picker can
	// converge on the SAME logical operation; whoever fulfills it first (the
	// agent's presigned PUT, or the App's Uppy XHR PUT) supplies the bytes and
	// transitions it to queued/running. A prepared task holds no reader and
	// occupies no executor slot.
	UploadStatePrepared  UploadTaskState = "prepared"
	UploadStateQueued    UploadTaskState = "queued"
	UploadStateRunning   UploadTaskState = "running"
	UploadStateCompleted UploadTaskState = "completed"
	UploadStateFailed    UploadTaskState = "failed"
	UploadStateCancelled UploadTaskState = "cancelled"
	// UploadStateExpired marks a handle that was evicted before completion —
	// most commonly a Prepared (minted but never fulfilled) handle whose
	// presigned endpoint window lapsed. It is only ever produced by tombstone
	// retention so a late status poll on an old handle reports "expired"
	// instead of the misleading "unknown upload handle".
	UploadStateExpired UploadTaskState = "expired"
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
	// preparedTTL is the presigned endpoint lifetime recorded at Prepare time.
	// pruneLocked evicts an unfulfilled Prepared task against THIS value (the
	// actual endpoint TTL) so a task is never pruned while its endpoint is
	// still live. It is set on the trackedTask, not the UploadTask, because it
	// is purely a retention policy detail for the async manager.
	preparedTTL time.Duration
	// archiveMode and wrap record the archive/directory-root handling captured
	// at Prepare time for the mint (presigned PUT) source. The PUT itself
	// carries only raw bytes with no way to express archive semantics, so the
	// transformation must be decided when the handle is minted and applied when
	// the bytes later arrive at Fulfill. Kept on the trackedTask (like
	// preparedTTL) because they are fulfillment policy for the async manager.
	archiveMode string
	wrap        bool
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
	mu         sync.Mutex
	tasks      map[string]*trackedTask
	tombstones map[string]*UploadTask
	// tombstoneOrder is the FIFO of tombstoned ids in the order they were
	// recorded (see tombstoneLocked), so pruneLocked can retire expired
	// tombstones from the front without scanning the whole map.
	tombstoneOrder []string
	exec           UploadExecutor
	ttl            time.Duration
	MaxActive      int
	// PreparedTTL is the fallback how-long an unfulfilled Prepared
	// (minted-but-never-supplied) canonical operation is retained before prune
	// when a task is created without its own endpoint TTL. It is intentionally
	// short (default: DefaultHTTPUploadTTL) so unfulfilled handles expire about
	// when their presigned endpoint does, instead of living the full terminal
	// TTL. Prepare() normally records the actual endpoint TTL on each task, and
	// pruneLocked uses that per-task value so a task is never evicted while its
	// endpoint is still live; this field only falls back when ttl is unset.
	PreparedTTL time.Duration
	// MaxPrepared caps how many outstanding Prepared (unfulfilled) canonical
	// operations the manager holds at once, guarding against a mint/prepare
	// flood that would otherwise accumulate unbounded handles within the
	// prepared retention window (they bypass MaxActive, which counts live
	// reader/goroutine uploads).
	MaxPrepared int
	// ExecTimeout is the hard upper bound on a single async upload's lifetime.
	// A hung executor (network/TUS stall that ignores context cancellation)
	// must not occupy a MaxActive slot forever, or a handful of stuck uploads
	// could exhaust every slot and block all future async uploads (DoS).
	ExecTimeout time.Duration
}

// defaultExecTimeout bounds a single async upload when no explicit timeout is
// configured. It is intentionally generous — it only caps a genuinely hung
// upload (one whose transport ignores context cancellation), which is the
// scenario that would otherwise leak a MaxActive slot indefinitely.
const defaultExecTimeout = 30 * time.Minute

// NewUploadTaskManager creates a manager bound to an executor.
func NewUploadTaskManager(exec UploadExecutor, ttl time.Duration) *UploadTaskManager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &UploadTaskManager{
		tasks:       make(map[string]*trackedTask),
		tombstones:  make(map[string]*UploadTask),
		exec:        exec,
		ttl:         ttl,
		MaxActive:   defaultMaxActiveUploads,
		PreparedTTL: defaultPreparedTTL,
		MaxPrepared: defaultMaxPrepared,
		ExecTimeout: defaultExecTimeout,
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
//
// Start creates a brand-new task; to fulfill a handle pre-created by Prepare
// (the shared canonical operation used by upload_file and the upload App), use
// Fulfill instead.
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
	m.mu.Lock()
	runCtx, cancel, tt, err := m.beginLocked(ctx, task, reader)
	if err != nil {
		m.mu.Unlock()
		// Start owns the reader on every return path; release it on the
		// no-slot error so a network-backed body is not leaked.
		cancel()
		_ = reader.Close()
		return "", err
	}
	m.mu.Unlock()
	m.spawn(tt, runCtx, reader, size, name, wait, "preserve", false)
	return id, nil
}

// PrepareOption adjusts the archive/directory-root handling recorded on a
// prepared (minted) upload handle so fulfillment honors the agent's requested
// transformation. The mint/presigned-PUT source is out-of-band: the mode is
// captured at Prepare time and applied when the handle is later fulfilled,
// because the PUT itself arrives as raw bytes with no way to express archive
// semantics.
type PrepareOption func(*prepareOpts)

type prepareOpts struct {
	archiveMode string
	wrap        bool
}

// WithArchiveMode records how the fulfilled bytes are treated. "convert"
// (or empty) extracts an archive into a directory DAG when the bytes are an
// archive; "preserve" keeps them a single file. The default for an undecorated
// prepared handle is "preserve", so a legacy mint (which carries no agent
// contract) never silently extracts; the upload_file mint source always passes
// an explicit option.
func WithArchiveMode(mode string) PrepareOption {
	return func(o *prepareOpts) {
		if mode == "" {
			mode = "convert"
		}
		o.archiveMode = mode
	}
}

// WithWrap records whether a single-file fulfillment should be wrapped into a
// directory root. Only meaningful when the fulfilled bytes are not themselves
// an archive that gets converted.
func WithWrap(wrap bool) PrepareOption {
	return func(o *prepareOpts) { o.wrap = wrap }
}

// Prepare pre-registers a canonical upload operation and returns its opaque
// handle WITHOUT supplying any bytes. The resulting task is visible to
// status/list in the Prepared state but holds no reader and occupies no
// executor slot. Either the agent transport path (a presigned PUT) or the App
// file picker fulfills it via Fulfill; duplicate fulfillment is rejected, so
// whoever completes it first becomes the authoritative single result.
//
// ttl is the presigned endpoint lifetime that will back this handle (e.g. the
// TTL used when minting the HTTP PUT endpoint). It is stored on the task so
// pruneLocked evicts an unfulfilled Prepared task against the TRUE endpoint
// lifetime rather than a hardcoded default — a task is never pruned while its
// endpoint is still live, so a late PUT can never hit a pruned handle. A
// non-positive ttl falls back to the manager-wide PreparedTTL default.
func (m *UploadTaskManager) Prepare(name string, ttl time.Duration, opts ...PrepareOption) (string, error) {
	if m.exec == nil {
		return "", errors.New("upload executor is not configured")
	}
	if name == "" {
		name = DefaultUploadName
	}
	// Default the archive handling to "preserve" so an undecorated (legacy)
	// prepared handle stays single-file; the upload_file mint source overrides
	// this with an explicit WithArchiveMode.
	po := prepareOpts{archiveMode: "preserve"}
	for _, o := range opts {
		if o != nil {
			o(&po)
		}
	}
	id := newTaskID()
	task := &UploadTask{
		ID:        id,
		Name:      name,
		State:     UploadStatePrepared,
		CreatedAt: time.Now(),
	}
	m.mu.Lock()
	m.pruneLocked()
	if err := m.acquirePreparedSlotLocked(); err != nil {
		m.mu.Unlock()
		return "", err
	}
	m.tasks[id] = &trackedTask{task: task, preparedTTL: ttl, archiveMode: po.archiveMode, wrap: po.wrap}
	m.mu.Unlock()
	return id, nil
}

// Fulfill supplies bytes to a handle pre-created by Prepare, transitioning it
// from Prepared to queued/running and starting the same async upload path as
// Start (the two share spawn). It is idempotent against duplicate fulfillment:
// an already-claimed (queued/running) or finished (completed/failed/cancelled)
// task returns an explicit error rather than starting a second upload, so
// whichever participant PUTs the bytes first is the authoritative result for
// the handle. On error it closes the supplied reader (ownership is handed
// over). If the id does not correspond to a known prepared task, callers
// should fall back to Start for a brand-new task.
func (m *UploadTaskManager) Fulfill(ctx context.Context, id string, reader io.ReadCloser, size int64, name string, wait bool) error {
	if m.exec == nil {
		_ = reader.Close()
		return errors.New("upload executor is not configured")
	}
	m.mu.Lock()
	if _, tomb := m.tombstones[id]; tomb {
		m.mu.Unlock()
		_ = reader.Close()
		return fmt.Errorf("upload handle %q has expired; start a fresh upload", id)
	}
	tt, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		_ = reader.Close()
		return fmt.Errorf("unknown upload handle %q", id)
	}
	switch tt.task.State {
	case UploadStatePrepared:
		// Proceed to claim below.
	case UploadStateQueued, UploadStateRunning:
		m.mu.Unlock()
		_ = reader.Close()
		return fmt.Errorf("upload %q is already in progress; refusing a second fulfillment", id)
	case UploadStateCompleted, UploadStateFailed, UploadStateCancelled:
		m.mu.Unlock()
		_ = reader.Close()
		return fmt.Errorf("upload %q already finished (state %s); refusing a second fulfillment", id, tt.task.State)
	default:
		m.mu.Unlock()
		_ = reader.Close()
		return fmt.Errorf("upload %q cannot be fulfilled from state %s", id, tt.task.State)
	}
	if name == "" {
		name = tt.task.Name
	}
	// Carry the archive/wrap handling recorded on the handle at Prepare time
	// into the executor. beginLocked builds a fresh trackedTask, so capture
	// these from the original handle before it is re-registered.
	mode, wrapped := tt.archiveMode, tt.wrap
	runCtx, cancel, claimed, err := m.beginLocked(ctx, tt.task, reader)
	if err != nil {
		m.mu.Unlock()
		cancel()
		_ = reader.Close()
		return err
	}
	m.mu.Unlock()
	m.spawn(claimed, runCtx, reader, size, name, wait, mode, wrapped)
	return nil
}

// beginLocked claims an executor slot, attaches a detached run context (with
// the hard ExecTimeout), registers the task under m.mu (for a brand-new task)
// or reuses the existing entry, and transitions it to the claimable queued
// state. Caller must hold m.mu on entry; the lock is still held on return so
// the caller must unlock before spawning. It returns the detached runCtx and
// cancel for the spawn goroutine plus the registered trackedTask, or an error
// after constructing a no-op cancel (callers must close reader themselves on
// the error path).
func (m *UploadTaskManager) beginLocked(ctx context.Context, task *UploadTask, reader io.ReadCloser) (context.Context, context.CancelFunc, *trackedTask, error) {
	m.pruneLocked()
	if err := m.acquireSlotLocked(); err != nil {
		return nil, func() {}, nil, err
	}
	// Preserve the caller's context values (tracing, auth, logging) while
	// dropping the deadline and cancellation that would otherwise abort the
	// async work once the request that started it returns. A hard ExecTimeout
	// still bounds the work so a hung executor is force-cancelled and its
	// MaxActive slot freed instead of leaking.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.ExecTimeout)
	tt := &trackedTask{task: task, cancel: cancel, reader: reader}
	m.tasks[task.ID] = tt
	// A lapsed prepared handle is tombstoned as Expired by the pruneLocked
	// above, then re-registered here as a live running upload by the same
	// Fulfill. Drop that stale tombstone: the handle is actively uploading, so
	// reporting "expired" would be wrong, and leaving it would let a live task
	// coexist with a tombstone (breaking Cancel) and refresh the FIFO's
	// FinishedAt (breaking processLocked's monotonic termination).
	m.untombstoneLocked(task.ID)
	// Bring the task to the claimable (queued) state so spawn's bail check and
	// cancel handling see a coherent lifecycle; the goroutine flips it to
	// running immediately after. Start's task is already queued; Fulfill's is
	// prepared.
	task.State = UploadStateQueued
	now := time.Now()
	task.StartedAt = &now
	return runCtx, cancel, tt, nil
}

// spawn launches the async upload work for a claimed task on a background
// goroutine. Both Start and Fulfill converge here after the task has been
// transitioned to queued and registered under m.mu, so the two entry points
// share exactly one upload/execution path. It owns the reader (passed in via
// the trackedTask) and closes it exactly once on completion/cancel via
// closeOnce.
func (m *UploadTaskManager) spawn(tt *trackedTask, runCtx context.Context, reader io.ReadCloser, size int64, name string, wait bool, archiveMode string, wrap bool) {
	task := tt.task
	go func() {
		m.mu.Lock()
		// Bail if the task was cancelled between the claim returning and this
		// goroutine acquiring the lock; never begin an upload that was already
		// aborted. Cancel() already closed the reader, and this early path
		// never closes it.
		if task.State != UploadStateQueued {
			m.mu.Unlock()
			return
		}
		now := time.Now()
		task.StartedAt = &now
		task.State = UploadStateRunning
		m.mu.Unlock()
		// Watchdog: when ExecTimeout elapses, force-abandon the owned reader
		// (aborting an in-flight network read that ignores context
		// cancellation) and cancel runCtx. Without this, an executor blocked
		// in a non-cancellable read would never return and would leak both the
		// reader and the MaxActive slot forever, defeating the slot guard.
		// AfterFunc fires exactly once; Stop below disarms it on normal return.
		watchdog := time.AfterFunc(m.ExecTimeout, func() {
			tt.closeReader()
			cancel := tt.cancel
			if cancel != nil {
				cancel()
			}
		})
		// archiveMode/wrap are sourced per-path: Start (a raw app-picker or
		// legacy bare-mint PUT with no agent contract) passes explicit
		// "preserve"/false so ParseArchiveMode can never default "" to convert
		// and silently extract a raw .zip into a directory DAG the caller never
		// requested. Fulfill (a handle prepared by the mint source) passes the
		// archiveMode/wrap recorded on the handle at Prepare time, honoring the
		// agent's requested conversion.
		result, err := m.exec(runCtx, reader, size, name, wait, archiveMode, wrap)
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
		cancel := tt.cancel
		if cancel != nil {
			cancel()
		}
	}()
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
// A handle that was legitimately known but was pruned before completion is
// reported from its tombstone as UploadStateExpired (rather than the misleading
// "unknown upload handle"), so a late status poll on an old handle is honest.
func (m *UploadTaskManager) Get(id string) (*UploadTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mirror List: evict terminal tasks past TTL so an agent that only polls
	// status/cancel (not List) does not let completed tasks accumulate forever.
	m.pruneLocked()
	t, ok := m.tasks[id]
	if ok {
		return cloneTask(t.task), nil
	}
	if tomb, ok := m.tombstones[id]; ok {
		return cloneTask(tomb), nil
	}
	return nil, fmt.Errorf("unknown upload handle %q", id)
}

// Cancel cancels a prepared/queued/running task. Cancelling a completed task
// is a no-op that returns an error so callers can surface it. A prepared task
// (bytes not yet supplied) has no goroutine or reader, so cancelling it just
// retires the handle.
func (m *UploadTaskManager) Cancel(id string) error {
	m.mu.Lock()
	// A live task always wins over a tombstone: a fulfilled handle that was
	// briefly tombstoned by pruneLocked (as Expired, before the same Fulfill
	// re-registered it as running) must still be cancellable. Only fall back to
	// the tombstone when no live task exists under the id.
	t, ok := m.tasks[id]
	if !ok {
		if tomb, ok := m.tombstones[id]; ok {
			m.mu.Unlock()
			return fmt.Errorf("upload %q already finished (state %s)", id, tomb.State)
		}
		m.mu.Unlock()
		return fmt.Errorf("unknown upload handle %q", id)
	}
	switch t.task.State {
	case UploadStatePrepared, UploadStateQueued, UploadStateRunning:
		now := time.Now()
		t.task.State = UploadStateCancelled
		// Mark the task finished so pruneLocked eventually evicts it; without
		// this the goroutine's own completion path skips setting FinishedAt for
		// cancelled tasks and they would accumulate without bound.
		t.task.FinishedAt = &now
		cancel := t.cancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		// Close the owned reader so an in-flight network read is aborted
		// immediately rather than letting the upload drain to completion in
		// the background after the task reports cancelled. closeReader is
		// idempotent, so if the goroutine is already finishing it wins and this
		// is a no-op, never a concurrent double-close. A prepared task has no
		// reader, so this is a no-op there too.
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
	if m.MaxActive <= 0 {
		return nil
	}
	active := 0
	for _, t := range m.tasks {
		switch t.task.State {
		case UploadStateQueued, UploadStateRunning:
			active++
			if active >= m.MaxActive {
				return errors.New("too many concurrent async uploads")
			}
		}
	}
	return nil
}

// acquirePreparedSlotLocked returns an error if the number of outstanding
// (Prepared, unfulfilled) tasks has reached MaxPrepared. Caller must hold m.mu.
// Prepared handles hold no reader and no executor slot, so they are bounded
// independently of MaxActive to stop a mint/prepare flood from accumulating
// unbounded handles within the prepared retention window. If MaxPrepared <= 0,
// prepared tasks are unbounded.
func (m *UploadTaskManager) acquirePreparedSlotLocked() error {
	if m.MaxPrepared <= 0 {
		return nil
	}
	prepared := 0
	for _, t := range m.tasks {
		if t.task.State == UploadStatePrepared {
			prepared++
			if prepared >= m.MaxPrepared {
				return errors.New("too many unresolved upload preparations")
			}
		}
	}
	return nil
}

// pruneLocked removes terminal (completed/failed/cancelled) tasks older than
// the TTL, plus prepared tasks that were never fulfilled within PreparedTTL
// (they hold no bytes and no slot, so they would otherwise accumulate if
// nobody fulfills them). Caller must hold m.mu. Terminal tasks are kept at
// least TTL after they finish so callers can still fetch status/result within
// the window. Prepared tasks are evicted on the (shorter, endpoint-aligned)
// PreparedTTL instead of the terminal TTL because an unfulfilled handle is
// only valid for the window its presigned endpoint lives.
func (m *UploadTaskManager) pruneLocked() {
	if m.ttl <= 0 {
		return
	}
	now := time.Now()
	for id, t := range m.tasks {
		switch t.task.State {
		case UploadStateCompleted, UploadStateFailed, UploadStateCancelled:
			cutoff := now.Add(-m.ttl)
			if t.task.FinishedAt != nil && t.task.FinishedAt.Before(cutoff) {
				m.tombstoneLocked(id, t.task)
				delete(m.tasks, id)
			}
		case UploadStatePrepared:
			// Never fulfilled before its window lapsed: evict by creation time
			// so an abandoned prepare cannot pin a handle forever. The window
			// is the task's OWN endpoint TTL recorded at Prepare time (falling
			// back to the manager-wide PreparedTTL when unset), so a prepared
			// task is never pruned while its presigned endpoint is still live —
			// a late PUT can always still fulfill it.
			preparedTTL := t.preparedTTL
			if preparedTTL <= 0 {
				preparedTTL = m.PreparedTTL
			}
			if preparedTTL > 0 && t.task.CreatedAt.Before(now.Add(-preparedTTL)) {
				exp := cloneTask(t.task)
				now := time.Now()
				exp.State = UploadStateExpired
				exp.Err = "upload handle expired before any bytes were supplied (presigned endpoint window lapsed)"
				exp.FinishedAt = &now
				m.tombstoneLocked(id, exp)
				delete(m.tasks, id)
			}
		}
	}
	// Retire tombstones that are themselves past the retention window so the
	// tombstone map cannot grow without bound; after this point a handle is
	// genuinely unknown (never existed within retention). Tombstones are kept
	// in insertion order (see tombstoneLocked), so only the front of the queue
	// is ever eligible: once the head is young enough, every successor is too
	// (they were tombstoned later), so we stop scanning and truncate the
	// already-retired prefix. O(tombstones) worst case, but the common hot
	// path (nothing expired) is O(1).
	cutoff := now.Add(-m.ttl)
	retired := 0
	for _, id := range m.tombstoneOrder {
		tomb, ok := m.tombstones[id]
		if !ok || tomb.FinishedAt == nil || !tomb.FinishedAt.Before(cutoff) {
			break
		}
		delete(m.tombstones, id)
		retired++
	}
	if retired > 0 {
		// Drop the retired prefix from the FIFO so those ids cannot block the
		// head of the queue on later prunes.
		m.tombstoneOrder = append([]string(nil), m.tombstoneOrder[retired:]...)
	}
}

// tombstoneLocked records a pruned handle's final task snapshot so a later
// status/cancel/fulfill poll on the old handle reports "expired"/"finished"
// instead of the misleading "unknown upload handle". Caller must hold m.mu.
// The id is appended to the FIFO only when it is new (an id re-registered and
// re-pruned keeps its original position so the ordering stays monotonic).
func (m *UploadTaskManager) tombstoneLocked(id string, task *UploadTask) {
	if _, exists := m.tombstones[id]; !exists {
		m.tombstoneOrder = append(m.tombstoneOrder, id)
	}
	m.tombstones[id] = cloneTask(task)
}

// untombstoneLocked removes a tombstone (and its FIFO entry) for an id that is
// being re-registered as a live task. A lapsed prepared handle can be
// tombstoned by pruneLocked and then re-registered by the same Fulfill; the
// tombstone is then stale (the handle is active, not expired) and must be
// dropped so a live task never coexists with a tombstone and the FIFO stays
// monotonic by FinishedAt (see processLocked). No-op when no tombstone exists.
// Caller must hold m.mu.
func (m *UploadTaskManager) untombstoneLocked(id string) {
	if _, ok := m.tombstones[id]; !ok {
		return
	}
	delete(m.tombstones, id)
	for i, tid := range m.tombstoneOrder {
		if tid == id {
			m.tombstoneOrder = append(m.tombstoneOrder[:i], m.tombstoneOrder[i+1:]...)
			return
		}
	}
}
