package apps

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForViewDomainResolverScopedClear pins the guarded-clear semantics of the
// serialized deployment-origin resolver window:
//
//   - inside the window the installed resolver returns the window's origin;
//   - after the window the resolver is reset (assembly-scoped, so a later
//     assembly never inherits the hosted origin);
//   - a direct SetViewDomainResolver call made DURING the window (another
//     assembly's test or embed that bypassed serialization) is NOT clobbered
//     by this window's teardown.
func TestForViewDomainResolverScopedClear(t *testing.T) {
	previous := ViewDomainResolver()
	SetViewDomainResolver(nil)
	t.Cleanup(func() { SetViewDomainResolver(previous) })

	var seen string
	require.NoError(t, ForViewDomainResolver("https://a.example.com", func() error {
		f := ViewDomainResolver()
		require.NotNil(t, f, "resolver installed inside the window")
		seen = f()
		return nil
	}))
	assert.Equal(t, "https://a.example.com", seen, "the window's resolver resolves its own origin")
	assert.Nil(t, ViewDomainResolver(), "the resolver is cleared when the window returns")

	// Guarded clear: an external override installed mid-window survives.
	// The override must NOT go through the apps package setter — by design
	// SetViewDomainResolver is serialized on viewDomainMu, which the window
	// itself holds for the whole window — so install the override the raw
	// way an unserialized caller would. ALWAYS bump the CLI-side generation
	// BEFORE the registry set when installing an unserialized override (the
	// same ordering the wrapper uses): bump-then-set guarantees a concurrent
	// window teardown racing this install observes the changed generation and
	// skips its restore, so the override cannot be clobbered. Set-then-bump
	// would let a teardown read the unchanged generation and restore
	// prevResolver over the override.
	override := func() string { return "https://override.example.com" }
	require.NoError(t, ForViewDomainResolver("https://a.example.com", func() error {
		viewDomainGeneration.Add(1)              // the wrapper's gen bump, unserialized
		registry.SetViewDomainResolver(override) // bypasses serialization
		return nil
	}))
	got := ViewDomainResolver()
	require.NotNil(t, got)
	assert.Equal(t, "https://override.example.com", got(),
		"the window teardown must not clobber a resolver installed over it mid-window")
	SetViewDomainResolver(nil)
}

// TestForViewDomainResolverSerializesOrigins pins that concurrent
// resolver windows with distinct origins never interleave: each window's fn
// must observe its own origin through the registry, and (per the
// one-server-at-a-time assembly restriction in registrar.go) that is the
// contract the hosted assembly relies on.
func TestForViewDomainResolverSerializesOrigins(t *testing.T) {
	previous := ViewDomainResolver()
	SetViewDomainResolver(nil)
	t.Cleanup(func() { SetViewDomainResolver(previous) })

	const windows = 8
	var wg sync.WaitGroup
	errs := make([]error, windows)
	seen := make([]string, windows)
	origins := make([]string, windows)
	for i := 0; i < windows; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			origin := originForWindow(i)
			origins[i] = origin
			errs[i] = ForViewDomainResolver(origin, func() error {
				f := ViewDomainResolver()
				seen[i] = f()
				return nil
			})
		}(i)
	}
	wg.Wait()
	for i := 0; i < windows; i++ {
		require.NoErrorf(t, errs[i], "window %d must succeed", i)
		require.Equalf(t, origins[i], seen[i], "window %d must observe its own origin, not a concurrently-running window's", i)
	}
	assert.Nil(t, ViewDomainResolver(), "no window leaks its resolver past the serialized run")
}

func originForWindow(i int) string {
	if i%2 == 0 {
		return "https://a.example.com"
	}
	return "https://b.example.com"
}

// TestSetViewDomainResolverConcurrentWithGuardedTeardown pins that the direct
// setter is serialized on the same viewDomainMu as ForViewDomainResolver's
// generation-guarded teardown. Before the shared mutex, a setter racing a
// window's teardown interleaved the teardown's generation-check/add +
// registry-restore against the setter's generation-add + registry-set, and
// the teardown's restore clobbered the setter's fresh install.
//
// Phase 1 is choreographed to be deterministic: a setter fired while a
// window's fn is still running can only acquire viewDomainMu AFTER that
// window's teardown has released it, so the teardown (which observed an
// unchanged generation) runs first, and the setter's install must survive
// the window's exit. Under the old unserialized setter the restore could
// land after the setter's set and wipe it.
//
// Phase 2 free-runs a window goroutine against a setter goroutine under
// -race and pins the now-serialized end-state invariant: a window never
// leaks its origin past its guarded teardown, and after quiescence the
// installed resolver is the setter's install or a restored prev — never a
// window's origin.
func TestSetViewDomainResolverConcurrentWithGuardedTeardown(t *testing.T) {
	previous := ViewDomainResolver()
	SetViewDomainResolver(nil)
	t.Cleanup(func() { SetViewDomainResolver(previous) })

	const (
		iters        = 100
		setterOrigin = "https://setter.example.com"
		windowOrigin = "https://window.example.com"
	)
	setter := func() string { return setterOrigin }

	// Phase 1 — choreographed interleave: setter while the window is live.
	for i := 0; i < iters; i++ {
		fnRan := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- ForViewDomainResolver(windowOrigin, func() error {
				close(fnRan)
				return nil
			})
		}()
		<-fnRan
		SetViewDomainResolver(setter) // blocked until the window's teardown releases viewDomainMu
		require.NoErrorf(t, <-done, "iter %d: window must succeed", i)
		installed := ViewDomainResolver()
		require.NotNilf(t, installed,
			"iter %d: the setter's install was clobbered by the racing teardown's restore", i)
		require.Equal(t, setterOrigin, installed(),
			"iter %d: the setter's install must survive the window it raced", i)
	}

	// Phase 2 — free-running hammer under -race.
	const (
		windowIters  = 8 * iters
		setterIters  = 2 * iters
		windowRounds = 4
	)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, windowRounds+1)

	for r := 0; r < windowRounds; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			// Distinct origin per round so a round's post-teardown check can
			// never mistake a sibling round's legitimate install for a leak
			// of its own origin.
			ownOrigin := fmt.Sprintf("https://window-%d.example.com", r)
			<-start
			for i := 0; i < windowIters; i++ {
				seen := ""
				if err := ForViewDomainResolver(ownOrigin, func() error {
					if f := ViewDomainResolver(); f != nil {
						seen = f()
					}
					return nil
				}); err != nil {
					errs <- fmt.Errorf("window round %d iter %d: %w", r, i, err)
					return
				}
				if seen != ownOrigin {
					errs <- fmt.Errorf(
						"window round %d iter %d observed resolver %q inside its own window: a concurrent setter slipped past the serialization",
						r, i, seen)
					return
				}
				if f := ViewDomainResolver(); f != nil && f() == ownOrigin {
					errs <- fmt.Errorf(
						"window round %d iter %d leaked its origin past the guarded teardown (the generation guard skipped the restore while its resolver stayed installed)",
						r, i)
					return
				}
			}
		}(r)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < setterIters; i++ {
			SetViewDomainResolver(setter)
			// A window's origin may only be installed while that window
			// holds viewDomainMu — never at the instant the serialized
			// setter returns.
			if f := ViewDomainResolver(); f != nil && f() == windowOrigin {
				errs <- fmt.Errorf(
					"setter %d: observed a window's origin installed right after the setter returned", i)
				return
			}
		}
	}()

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	if f := ViewDomainResolver(); f != nil {
		assert.Equalf(t, setterOrigin, f(),
			"after quiescence the installed resolver must be the setter's or a restored prev — never a window's origin")
	}
}
