package vault

import (
	"context"
	"log"
	"sync"
	"time"
)

// SyncLoopInterval is the default time between continuous vault sync ticks in
// MCP mode. It mirrors the Sia Storage App's SYNC_EVENTS_INTERVAL: 10s keeps a
// multi-device vault fresh without hammering the indexer when idle (an idle
// tick performs a single ObjectEvents call that returns 0-1 results).
const SyncLoopInterval = 10 * time.Second

// SyncLoopConfig configures the background vault sync loop used by the MCP
// server to keep the active vault's local cache converged with the indexer
// without an agent needing to call vault_sync explicitly.
type SyncLoopConfig struct {
	// Profiles returns every registered vault profile the process has access to
	// (i.e. provisioned with a readable app key), or an empty slice when none.
	// Passively syncing ALL accessible profiles (not just the active/default
	// one) keeps every vault pinner can unlock converging with its indexer
	// while the server runs. Called once per tick so a newly registered,
	// provisioned, or forgotten profile is picked up without restarting.
	Profiles func() []string
	// Service builds a VaultService for the given profile. It is called only
	// when a profile first appears (or is re-added), and the resulting service
	// is REUSED across idle ticks for that profile (so a long-running MCP
	// server does not rebuild the Sia SDK and re-open the SQLite cache every
	// interval — see VaultSyncLoop). A non-nil error is logged and that profile
	// is skipped for the tick, retrying on the next idle interval.
	Service func(profile string) (VaultService, error)
	// IdleCloseTicks bounds open SDK/DB handles on machines with many
	// registered vaults: after this many consecutive idle ticks (a tick with no
	// full batch drained), a profile's cached VaultService is closed and rebuilt
	// lazily on the next tick that needs it. 0 (or negative) keeps a profile's
	// service open for the server's lifetime. Active bursts still reuse the open
	// service; only genuinely long-idle vaults release their handles.
	IdleCloseTicks int
}

// ServiceScheduler manages background interval-based workers. Each registered
// worker runs on a timer; a TickFunc that returns a non-nil Duration overrides
// the next scheduled interval (a zero Duration means "run again immediately").
// A nil return uses the worker's default interval.
//
// Ticks never overlap: if a tick is still running when its next timer fires,
// the run is deferred and executed immediately after the in-flight tick
// finishes (the Sia Storage App's "rerunRequested" semantics) rather than
// dropped. Shutdown cancels all timers, aborts in-flight ticks via the shared
// context, and waits for them to finish.
type ServiceScheduler struct {
	mu       sync.Mutex
	workers  map[string]*schedulerWorker
	cancel   context.CancelFunc
	started  bool
	wg       sync.WaitGroup
}

type schedulerWorker struct {
	name     string
	interval time.Duration
	fn       TickFunc
	// trigger is a buffered size-1 channel used by TriggerNow to wake a
	// waiting tick immediately (coalescing concurrent triggers).
	trigger chan struct{}
}

// TickFunc is a background worker. If it returns a non-nil Duration, that
// overrides the next scheduled interval (a zero Duration re-runs immediately).
// A nil return uses the worker's default interval.
type TickFunc func(ctx context.Context) *time.Duration

// NewServiceScheduler returns an empty scheduler. Call Register for each worker
// and then Start to begin ticking.
func NewServiceScheduler() *ServiceScheduler {
	return &ServiceScheduler{workers: map[string]*schedulerWorker{}}
}

// Register adds a named worker ticking at interval. Calling Register after
// Start is not supported; register all workers before Start.
func (s *ServiceScheduler) Register(name string, interval time.Duration, fn TickFunc) {
	if !s.started {
		s.workers[name] = &schedulerWorker{name: name, interval: interval, fn: fn, trigger: make(chan struct{}, 1)}
	}
}

// Start begins ticking all registered workers under ctx. It is safe to call
// once; subsequent calls are no-ops. Cancelling ctx aborts every in-flight
// tick.
func (s *ServiceScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	workers := make([]*schedulerWorker, 0, len(s.workers))
	for _, w := range s.workers {
		workers = append(workers, w)
	}
	s.mu.Unlock()

	for _, w := range workers {
		s.wg.Add(1)
		go s.runWorker(runCtx, w)
	}
}

// TriggerNow wakes the named worker immediately, bypassing its scheduled
// interval. If that worker's tick is currently running, the trigger is honored
// as a deferred re-run (the tick that just finished runs again immediately).
// A worker that has not been started, or is paused, is a no-op. This is how the
// Sia Storage App surfaces "a write just landed; sync now" to its scheduler.
func (s *ServiceScheduler) TriggerNow(name string) {
	s.mu.Lock()
	w, ok := s.workers[name]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

// Shutdown cancels the scheduler context (aborting all in-flight ticks) and
// blocks until every worker goroutine has returned. It is safe to call more
// than once and from a deferred function. After Shutdown, Start must not be
// called again on the same scheduler.
func (s *ServiceScheduler) Shutdown() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// runWorker is the per-worker goroutine. It schedules the initial tick after
// interval, then re-schedules based on each tick's returned Duration.
func (s *ServiceScheduler) runWorker(ctx context.Context, w *schedulerWorker) {
	defer s.wg.Done()

	interval := w.interval
	// A nil/negative configured interval falls back to SyncLoopInterval so a
	// misconfigured worker doesn't spin.
	if interval <= 0 {
		interval = SyncLoopInterval
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// The timer fired (timer.C was drained by the receive).
			if applyTickResult(s.runTick(ctx, w), &interval) {
				// Immediate re-run: the tick drained full batches, so keep
				// draining without waiting for the idle interval.
				timer.Reset(0)
				continue
			}
			timer.Reset(interval)
		case <-w.trigger:
			// A trigger now bypasses the idle wait. If the timer also fired
			// (value pending in the channel), it is not a problem: runTick is
			// synchronous in this goroutine so ticks never overlap, and the
			// stale value is dropped by the non-blocking drain below.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			// The tick's returned Duration is honored the same as a timer tick:
			// a Duration(0) (drained full batch) still requests an immediate
			// re-run via the timer, and a custom override updates the interval.
			if applyTickResult(s.runTick(ctx, w), &interval) {
				timer.Reset(0)
				continue
			}
			timer.Reset(interval)
		}
	}
}

// applyTickResult consumes a tick's returned duration and applies it to the
// scheduler's interval in place. It reports true when the tick requested an
// immediate re-run (Duration(0)), which the caller honors by resetting the
// timer to 0 and looping. A nil or negative result is ignored (use the current
// interval). Shared by the timer and trigger branches so a trigger-invoked tick
// gets the same zero/custom-override treatment as a timer tick.
func applyTickResult(d *time.Duration, interval *time.Duration) bool {
	if d == nil || *d < 0 {
		return false
	}
	if *d == 0 {
		return true
	}
	*interval = *d
	return false
}

// runTick invokes a single tick synchronously. Its Duration return (if any)
// determines the next interval. The scheduler's ctx is passed through so a
// Shutdown aborts a still-draining tick.
func (s *ServiceScheduler) runTick(ctx context.Context, w *schedulerWorker) *time.Duration {
	defer func() {
		// A panicking worker must not take down the whole scheduler; log and
		// continue with the default interval.
		if r := recover(); r != nil {
			// The package logger is unavailable here to avoid an import cycle;
			// use the standard logger, mirroring other core recovery sites.
			log.Printf("vault service scheduler: worker %q panicked: %v", w.name, r)
		}
	}()
	return w.fn(ctx)
}

// VaultSyncLoop is a ServiceScheduler TickFunc that passively keeps EVERY
// registered vault profile the process has access to converged with its Sia
// indexer while the MCP server runs — not just the active/default one. It owns
// one VaultService per profile across idle ticks and rebuilds a profile's
// service only when that profile is added or re-provisioned, so a long-running
// server does not re-open each SQLite cache and reconstruct each Sia SDK (with
// network auth) roughly every interval when a vault is idle.
//
// Each tick:
//
//  1. Enumerates the accessible profiles (cheap registry read).
//  2. Retires and closes services for profiles no longer accessible (forgotten
//     or de-provisioned).
//  3. For each accessible profile, reuses (or builds) its cached service and
//     drains the profile's pending events via Sync() in a loop while the
//     returned batch is full (mirrors the catalogops vault_sync handler and the
//     Sia Storage App's batch loop).
//
// It returns Duration(0) when any profile drained a full batch (so the
// scheduler re-runs immediately within the same burst, still reusing the open
// services). Errors are logged per profile and that profile is retried on the
// next idle interval; a background sync that fails (e.g. indexer down) must not
// panic the server.
type VaultSyncLoop struct {
	cfg SyncLoopConfig

	mu     sync.Mutex
	svcs   map[string]VaultService
	idle   map[string]int // consecutive idle ticks per profile (idle-close bookkeeping)
	closed bool
}

// NewVaultSyncLoop creates a persistent multi-profile sync loop over cfg.
// Close it when the server shuts down so every held VaultService (and its
// SDK/DB handles) is released.
func NewVaultSyncLoop(cfg SyncLoopConfig) *VaultSyncLoop {
	return &VaultSyncLoop{cfg: cfg, svcs: map[string]VaultService{}, idle: map[string]int{}}
}

// Tick implements the ServiceScheduler TickFunc contract. It is safe to call
// from concurrent goroutines (the scheduler runs one at a time; Close may run
// concurrently on shutdown and is serialized through mu).
func (l *VaultSyncLoop) Tick(ctx context.Context) *time.Duration {
	if err := ctx.Err(); err != nil {
		// Shutting down / ctx aborted; bail before building any service.
		return nil
	}
	profiles := l.cfg.Profiles()

	// Retire services for profiles that are no longer accessible, so a
	// forgotten/de-provisioned vault stops being synced and its SDK/DB handle
	// is released.
	accessible := make(map[string]struct{}, len(profiles))
	for _, p := range profiles {
		accessible[p] = struct{}{}
	}
	l.mu.Lock()
	for p, svc := range l.svcs {
		if _, ok := accessible[p]; !ok {
			_ = svc.Close()
			delete(l.svcs, p)
			delete(l.idle, p)
		}
	}
	l.mu.Unlock()

	// Draining every accessible profile. A profile that drained a full batch is
	// active: it resets its idle counter and requests an immediate scheduler
	// re-run. Idle counters are NOT advanced here — they advance only on a
	// fully-idle tick (below), so a busy sibling's immediate re-runs (which can
	// fire ticks far faster than the idle interval) never churn a quiescent
	// vault's service.
	rerun := false
	for _, p := range profiles {
		if err := ctx.Err(); err != nil {
			return nil
		}
		svc, err := l.ensureService(p)
		if err != nil {
			log.Printf("vault sync: skip profile %q (service build: %v)", p, err)
			continue
		}
		if drainService(ctx, svc) {
			rerun = true
			l.resetIdle(p)
		}
	}
	if rerun {
		// The scheduler will re-run immediately; do not advance idle counters,
		// so a busy peer's short cycles cannot idle-close a quiescent vault.
		zero := time.Duration(0)
		return &zero
	}
	// Fully-idle tick (the true idle cadence): advance every cached profile's
	// idle counter and idle-close those past IdleCloseTicks.
	l.advanceIdleAndClose()
	return nil
}

// ensureService returns the cached VaultService for a profile, building it on
// first appearance and caching for reuse across ticks. It returns ErrVaultClosed
// if the loop has been closed.
func (l *VaultSyncLoop) ensureService(profile string) (VaultService, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, ErrVaultClosed
	}
	if svc := l.svcs[profile]; svc != nil {
		if _, ok := l.idle[profile]; !ok {
			l.idle[profile] = 0
		}
		return svc, nil
	}
	svc, err := l.cfg.Service(profile)
	if err != nil {
		return nil, err
	}
	l.svcs[profile] = svc
	l.idle[profile] = 0
	return svc, nil
}

// resetIdle clears a profile's idle-tick counter after an active (drained)
// tick, so an actively-syncing vault is never idle-closed.
func (l *VaultSyncLoop) resetIdle(profile string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.idle[profile] = 0
}

// advanceIdleAndClose advances every cached profile's idle counter by one and
// closes each whose counter reaches cfg.IdleCloseTicks. It is called only on a
// fully-idle tick — the true idle cadence — so an increment corresponds to one
// real idle interval, independent of sibling-triggered immediate re-runs
// (which, left unguarded, would drive a quiescent profile's counter to
// IdleCloseTicks in seconds and churn its SDK/DB rebuild/close cycle). A
// non-positive IdleCloseTicks never closes. Services are rebuilt lazily by
// ensureService on the next tick that needs them.
func (l *VaultSyncLoop) advanceIdleAndClose() {
	if l.cfg.IdleCloseTicks <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for p, svc := range l.svcs {
		l.idle[p]++
		if l.idle[p] >= l.cfg.IdleCloseTicks {
			_ = svc.Close()
			delete(l.svcs, p)
			delete(l.idle, p)
		}
	}
}

// drainService syncs one VaultService, looping while the fetched batch is full
// so a large backlog converges in one burst. It reports whether any full batch
// was drained (the caller uses that to request an immediate scheduler re-run).
func drainService(ctx context.Context, svc VaultService) bool {
	drained := false
	for {
		if err := ctx.Err(); err != nil {
			return drained
		}
		applied, full, err := svc.Sync(ctx)
		if err != nil {
			log.Printf("vault sync: tick failed (%v)", err)
			return drained
		}
		if full {
			drained = true
			continue
		}
		if applied > 0 {
			// Applied events are useful progress signal; keep them visible at
			// info level only when non-trivial to avoid noise.
			log.Printf("vault sync: applied %d events", applied)
		}
		return drained
	}
}

// Close releases every held VaultService. It is safe to call more than once and
// concurrently with a running Tick; after Close returns, a subsequently-running
// Tick will rebuild the services on its next pass. Call it once the server
// shuts down (after the scheduler's Shutdown) to release SDK/DB handles.
func (l *VaultSyncLoop) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	for p, svc := range l.svcs {
		_ = svc.Close()
		delete(l.svcs, p)
	}
	l.idle = map[string]int{}
}
