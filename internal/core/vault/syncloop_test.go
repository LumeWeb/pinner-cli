package vault

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

func TestServiceScheduler_RunsTick(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	s.Register("t", time.Millisecond, func(ctx context.Context) *time.Duration {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	deadline := time.After(2 * time.Second)
	for calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("worker never ran")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestServiceScheduler_ImmediateRerunOnZeroDuration(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	// Base interval is short so the first tick fires; Duration(0) returns from
	// the first two ticks drive the immediate re-run chain.
	s.Register("t", time.Millisecond, func(ctx context.Context) *time.Duration {
		n := calls.Add(1)
		if n <= 2 {
			z := time.Duration(0)
			return &z
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	deadline := time.After(2 * time.Second)
	for calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("immediate re-run chain did not fire")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestServiceScheduler_CustomIntervalReturn(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	// Base interval is fast (1ms). The first tick overrides the next interval
	// to 1s; proving the second tick does NOT fire within the fast-base window
	// confirms the override superseded the base interval.
	s.Register("t", time.Millisecond, func(ctx context.Context) *time.Duration {
		if calls.Add(1) == 1 {
			d := time.Second
			return &d
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	deadline := time.After(2 * time.Second)
	for calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("first tick never ran")
		case <-time.After(time.Millisecond):
		}
	}

	// The override should hold the next tick off for ~1s, far longer than the
	// 1ms base. If it fires within this window, the base interval was used.
	time.Sleep(150 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("second tick fired within 150ms; custom interval override ignored (calls=%d)", calls.Load())
	}
}

func TestServiceScheduler_ShutdownStopsWorker(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	s.Register("t", time.Millisecond, func(ctx context.Context) *time.Duration {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	deadline := time.After(2 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("worker never reached 2 calls")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	s.Shutdown()
	baseline := calls.Load()

	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != baseline {
		t.Fatalf("worker kept running after shutdown: %d -> %d", baseline, got)
	}
}

func TestServiceScheduler_TriggerHonorsZeroRerun(t *testing.T) {
	// A trigger-invoked tick that returns Duration(0) (drained full batch) must
	// get an immediate re-run rather than waiting out the idle interval — the
	// same contract a timer tick honors. First tick returns 0 (requesting an
	// immediate re-run); the follow-up (whether via trigger or the immediate
	// timer) returns nil (use the interval), proving the zero return wasn't
	// dropped.
	var calls atomic.Int32
	first := true
	var mu sync.Mutex
	s := NewServiceScheduler()
	s.Register("t", time.Hour, func(ctx context.Context) *time.Duration {
		mu.Lock()
		defer mu.Unlock()
		n := calls.Add(1)
		if first {
			first = false
			_ = n
			z := time.Duration(0)
			return &z
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	// Fire the trigger once to start the first (Duration(0)) tick.
	deadline := time.After(2 * time.Second)
	for calls.Load() == 0 {
		s.TriggerNow("t")
		select {
		case <-deadline:
			t.Fatal("trigger never fired the first tick")
		case <-time.After(time.Millisecond):
		}
	}
	// The Duration(0) result must drive an immediate re-run (second call) far
	// faster than the hour-long base interval.
	deadline = time.After(2 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("Duration(0) from a trigger-invoked tick did not re-run immediately")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestServiceScheduler_TriggerNow(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	s.Register("t", time.Hour, func(ctx context.Context) *time.Duration {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	deadline := time.After(2 * time.Second)
	for calls.Load() == 0 {
		s.TriggerNow("t")
		select {
		case <-deadline:
			t.Fatal("trigger never fired")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestServiceScheduler_PanickingWorker(t *testing.T) {
	var calls atomic.Int32
	s := NewServiceScheduler()
	s.Register("panic", time.Millisecond, func(ctx context.Context) *time.Duration {
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return nil
	})
	s.Register("ok", time.Millisecond, func(ctx context.Context) *time.Duration {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer func() { cancel(); s.Shutdown() }()

	deadline := time.After(2 * time.Second)
	for calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("workers halted after a panicking sibling")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestVaultSyncLoop_NoActiveVault(t *testing.T) {
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return nil },
		Service: func(p string) (VaultService, error) {
			t.Fatal("service built when no profiles are accessible")
			return nil, nil
		},
	})
	if d := loop.Tick(context.Background()); d != nil {
		t.Fatalf("no-vault tick must return nil, got %v", d)
	}
}

func TestVaultSyncLoop_ServiceBuildError(t *testing.T) {
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) {
			return nil, errors.New("no app key")
		},
	})
	if d := loop.Tick(context.Background()); d != nil {
		t.Fatalf("build-error tick must return nil, got %v", d)
	}
}

func TestVaultSyncLoop_DrainsFullBatchesAndRequestsRerun(t *testing.T) {
	msvc := &MockVaultService{}
	// Sequence: two full batches then a non-full batch. Full batches must loop;
	// the final non-full batch indicates the backlog is drained. The service is
	// NOT closed after the tick (it is reused across ticks).
	msvc.EXPECT().Sync(mock.Anything).Return(5, true, nil).Once()
	msvc.EXPECT().Sync(mock.Anything).Return(3, true, nil).Once()
	msvc.EXPECT().Sync(mock.Anything).Return(1, false, nil).Once()
	msvc.EXPECT().Close().Return(nil).Once()

	var serviceCalls atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) { serviceCalls.Add(1); return msvc, nil },
	})

	d := loop.Tick(context.Background())
	if d == nil || *d != 0 {
		t.Fatalf("drained tick must return Duration(0), got %v", d)
	}
	msvc.AssertNumberOfCalls(t, "Sync", 3)
	if got := serviceCalls.Load(); got != 1 {
		t.Fatalf("service built %d times in one drain, want 1", got)
	}
	// The service is held (reused) until Close, not torn down per tick.
	msvc.AssertNumberOfCalls(t, "Close", 0)
	loop.Close()
	msvc.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_IdleTick(t *testing.T) {
	msvc := &MockVaultService{}
	msvc.EXPECT().Sync(mock.Anything).Return(0, false, nil).Once()
	msvc.EXPECT().Close().Return(nil).Once()

	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) { return msvc, nil },
	})
	if d := loop.Tick(context.Background()); d != nil {
		t.Fatalf("idle tick must return nil, got %v", d)
	}
	msvc.AssertNumberOfCalls(t, "Sync", 1)
	msvc.AssertNumberOfCalls(t, "Close", 0)
	loop.Close()
	msvc.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_ReusesServiceAcrossIdleTicks(t *testing.T) {
	msvc := &MockVaultService{}
	msvc.EXPECT().Sync(mock.Anything).Return(0, false, nil).Times(3)
	msvc.EXPECT().Close().Return(nil).Once()

	var serviceCalls atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) { serviceCalls.Add(1); return msvc, nil },
	})

	loop.Tick(context.Background())
	loop.Tick(context.Background())
	loop.Tick(context.Background())

	if got := serviceCalls.Load(); got != 1 {
		t.Fatalf("service rebuilt across idle ticks (%d builds); want 1 (reuse)", got)
	}
	msvc.AssertNumberOfCalls(t, "Sync", 3)
	loop.Close()
	msvc.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_SyncsAllAccessibleProfiles(t *testing.T) {
	svcA := &MockVaultService{}
	svcA.EXPECT().Sync(mock.Anything).Return(0, false, nil).Times(2)
	svcA.EXPECT().Close().Return(nil).Once()
	svcB := &MockVaultService{}
	svcB.EXPECT().Sync(mock.Anything).Return(0, false, nil).Times(2)
	svcB.EXPECT().Close().Return(nil).Once()

	var serviceCalls atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"a", "b"} },
		Service: func(p string) (VaultService, error) {
			serviceCalls.Add(1)
			if p == "a" {
				return svcA, nil
			}
			return svcB, nil
		},
	})

	loop.Tick(context.Background())
	loop.Tick(context.Background())

	if got := serviceCalls.Load(); got != 2 {
		t.Fatalf("each accessible profile must get one service (%d builds), want 2", got)
	}
	svcA.AssertNumberOfCalls(t, "Sync", 2)
	svcB.AssertNumberOfCalls(t, "Sync", 2)
	loop.Close()
	svcA.AssertNumberOfCalls(t, "Close", 1)
	svcB.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_AddsNewProfile(t *testing.T) {
	profiles := []string{"a"}
	svcA := &MockVaultService{}
	svcA.EXPECT().Sync(mock.Anything).Return(0, false, nil).Times(2)
	svcA.EXPECT().Close().Return(nil).Once()
	svcB := &MockVaultService{}
	svcB.EXPECT().Sync(mock.Anything).Return(0, false, nil).Once()
	svcB.EXPECT().Close().Return(nil).Once()

	var serviceCalls atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return profiles },
		Service: func(p string) (VaultService, error) {
			serviceCalls.Add(1)
			if p == "a" {
				return svcA, nil
			}
			return svcB, nil
		},
	})

	loop.Tick(context.Background()) // only A
	if got := serviceCalls.Load(); got != 1 {
		t.Fatalf("tick 1 built %d services, want 1", got)
	}
	profiles = []string{"a", "b"} // B is registered now
	loop.Tick(context.Background())
	if got := serviceCalls.Load(); got != 2 {
		t.Fatalf("new profile must build its service (%d builds), want 2", got)
	}
	// A is reused (not rebuilt); B syncs once.
	svcA.AssertNumberOfCalls(t, "Sync", 2)
	svcB.AssertNumberOfCalls(t, "Sync", 1)
	loop.Close()
	svcA.AssertNumberOfCalls(t, "Close", 1)
	svcB.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_RetiresForgottenProfile(t *testing.T) {
	profiles := []string{"a", "b"}
	svcA := &MockVaultService{}
	svcA.EXPECT().Sync(mock.Anything).Return(0, false, nil).Times(2)
	svcA.EXPECT().Close().Return(nil).Once()
	svcB := &MockVaultService{}
	svcB.EXPECT().Sync(mock.Anything).Return(0, false, nil).Once()
	svcB.EXPECT().Close().Return(nil).Once()

	var serviceCalls atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return profiles },
		Service: func(p string) (VaultService, error) {
			serviceCalls.Add(1)
			if p == "a" {
				return svcA, nil
			}
			return svcB, nil
		},
	})

	loop.Tick(context.Background()) // A and B
	if got := serviceCalls.Load(); got != 2 {
		t.Fatalf("tick 1 built %d services, want 2", got)
	}
	profiles = []string{"a"} // B is forgotten
	loop.Tick(context.Background())
	// A is reused; B's service is closed and no longer synced.
	svcA.AssertNumberOfCalls(t, "Sync", 2)
	svcB.AssertNumberOfCalls(t, "Sync", 1)
	svcB.AssertNumberOfCalls(t, "Close", 1)
	loop.Close()
	svcA.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_SyncError(t *testing.T) {
	msvc := &MockVaultService{}
	msvc.EXPECT().Sync(mock.Anything).Return(0, false, errors.New("indexer down")).Once()
	msvc.EXPECT().Close().Return(nil).Once()

	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) { return msvc, nil },
	})
	if d := loop.Tick(context.Background()); d != nil {
		t.Fatalf("error tick must return nil, got %v", d)
	}
	msvc.AssertNumberOfCalls(t, "Sync", 1)
	// The errored service is retained for retry on the next idle tick; only
	// Close (or a profile removal) tears it down.
	msvc.AssertNumberOfCalls(t, "Close", 0)
	loop.Close()
	msvc.AssertNumberOfCalls(t, "Close", 1)
}

func TestVaultSyncLoop_ContextCancelledBeforeBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var built atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return []string{"work"} },
		Service: func(p string) (VaultService, error) {
			built.Add(1)
			return &MockVaultService{}, nil
		},
	})
	if d := loop.Tick(ctx); d != nil {
		t.Fatalf("cancelled tick must return nil, got %v", d)
	}
	if built.Load() != 0 {
		t.Fatalf("tick built a service on a cancelled context; want 0 builds")
	}
}

func TestVaultSyncLoopTicker_Integration(t *testing.T) {
	var ticks atomic.Int32
	loop := NewVaultSyncLoop(SyncLoopConfig{
		Profiles: func() []string { return nil }, // no accessible profiles
		Service: func(p string) (VaultService, error) { return nil, nil },
	})
	defer loop.Close()
	s := NewServiceScheduler()
	s.Register("vaultSync", time.Millisecond, func(ctx context.Context) *time.Duration {
		ticks.Add(1)
		return loop.Tick(ctx)
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	deadline := time.After(2 * time.Second)
	for ticks.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("ticker never ticked")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	s.Shutdown()
}
