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
	// Profile resolves the active vault profile and reports whether a
	// provisioned vault is available (ok=false when no active vault is
	// configured or the profile has no app key yet). ok=false makes a tick a
	// silent no-op without building a service. Called once per tick to detect
	// an active-profile change, so a switch is picked up without restarting the
	// server.
	Profile func() (profile string, ok bool)
	// Service builds a VaultService for the given resolved profile. It is called
	// only when the active profile changes (or on the first tick), and the
	// resulting service is REUSED across idle ticks (so a long-running MCP
	// server does not rebuild the Sia SDK and re-open the SQLite cache every
	// interval — see VaultSyncLoop). A non-nil error is logged and the tick
	// ends, retrying on the next idle interval.
	Service func(profile string) (VaultService, error)
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

// VaultSyncLoop is a ServiceScheduler TickFunc that keeps the active vault
// profile's local cache converged with the Sia indexer while the MCP server
// runs. Unlike a per-tick build-and-close, it owns one VaultService across idle
// ticks and rebuilds it ONLY when the resolved active profile changes, so a
// long-running server does not re-open the SQLite cache and reconstruct the Sia
// SDK (with network auth) roughly every interval when the vault is idle.
//
// Each tick:
//
//  1. Resolves the active profile (cheap registry read). ok=false (no active
//     vault, or not yet provisioned) is a silent no-op.
//  2. Rebuilds the service only if the profile changed; otherwise reuses the
//     cached one.
//  3. Calls Sync() in a loop while the returned batch is full, draining all
//     pending events so a large backlog converges in one burst (mirrors the
//     catalogops vault_sync handler and the Sia Storage App's batch loop).
//
// It returns Duration(0) when any full batch was drained (so the scheduler
// re-runs immediately within the same burst, still reusing the open service).
// Errors are logged and the tick falls back to the idle interval; a background
// sync that fails (e.g. indexer down) must not panic the server and is retried
// on the next tick.
type VaultSyncLoop struct {
	cfg SyncLoopConfig

	mu      sync.Mutex
	svc     VaultService
	profile string
}

// NewVaultSyncLoop creates a persistent sync loop over cfg. Close it when the
// server shuts down so the held VaultService (and its SDK/DB handles) is
// released.
func NewVaultSyncLoop(cfg SyncLoopConfig) *VaultSyncLoop {
	return &VaultSyncLoop{cfg: cfg}
}

// Tick implements the ServiceScheduler TickFunc contract. It is safe to call
// from concurrent goroutines (the scheduler runs one at a time; Close may run
// concurrently on shutdown and is serialized through mu).
func (l *VaultSyncLoop) Tick(ctx context.Context) *time.Duration {
	if err := ctx.Err(); err != nil {
		// Shutting down / ctx aborted; bail before building a service.
		return nil
	}
	profile, ok := l.cfg.Profile()
	if !ok {
		// No active/provisioned vault; nothing to sync. Silent skip.
		return nil
	}

	l.mu.Lock()
	svc := l.svc
	if svc != nil && l.profile != profile {
		// Active profile changed; retire the old service and build a fresh one
		// bound to the new profile.
		_ = svc.Close()
		svc = nil
		l.svc = nil
		l.profile = ""
	}
	l.mu.Unlock()

	if svc == nil {
		var err error
		svc, err = l.cfg.Service(profile)
		if err != nil {
			log.Printf("vault sync: skip tick (service build: %v)", err)
			return nil
		}
		l.mu.Lock()
		l.svc = svc
		l.profile = profile
		l.mu.Unlock()
	}

	drained := false
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		applied, full, err := svc.Sync(ctx)
		if err != nil {
			log.Printf("vault sync: tick failed (%v)", err)
			return nil
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
		break
	}
	if drained {
		zero := time.Duration(0)
		return &zero
	}
	return nil
}

// Close releases the held VaultService. It is safe to call more than once and
// concurrently with a running Tick; after Close returns, a subsequently-running
// Tick will rebuild the service on its next Sync. Call it once the server
// shuts down (after the scheduler's Shutdown) to release SDK/DB handles.
func (l *VaultSyncLoop) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.svc != nil {
		_ = l.svc.Close()
		l.svc = nil
		l.profile = ""
	}
}
