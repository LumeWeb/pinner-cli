package apps

import (
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
	override := func() string { return "https://override.example.com" }
	require.NoError(t, ForViewDomainResolver("https://a.example.com", func() error {
		SetViewDomainResolver(override) // not going through the serialized window
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
