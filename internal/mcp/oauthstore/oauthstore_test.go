package oauthstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "oauth.db"), 30*24*time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	// Shorten the reuse window so tests don't sleep.
	s.reuseWindow = 100 * time.Millisecond
	return s
}

func TestRefreshRotationAndReuse(t *testing.T) {
	s := openTestStore(t)
	rt1 := "refresh-1"

	require.NoError(t, s.SaveClient("cli", "Claude", []string{"https://claude.ai/api/mcp/auth_callback"}))
	require.NoError(t, s.IssueRefreshToken(rt1, "cli", ""))

	// First use: rotate → accept; the store mints the successor (rt2).
	client, succ, status, err := s.RotateRefreshToken(rt1, "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)
	require.Equal(t, "cli", client)

	// Pin used_at slightly in the future so the reuse is DETERMINISTICALLY
	// inside the 100ms window (now.Sub(used_at) is negative ⇒ ≤ window) with no
	// wall-clock dependency — on Windows CI the clock is coarse and the SQLite
	// round-trip between the two calls previously exceeded the window, flaking
	// RotateReplay where RotateOKReused was expected.
	require.NoError(t, s.db.Model(&RefreshToken{}).Where("token = ?", rt1).Update("used_at", time.Now().Add(time.Hour)).Error)

	// Reuse the same token within the reuse window → benign; returns the SAME
	// already-issued successor (succ), not a freshly minted one.
	_, succ2, status, err := s.RotateRefreshToken(rt1, "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateOKReused, status)
	require.Equal(t, succ2, succ, "reuse must return the stored successor, not mint a new pair")

	// A never-used successor (succ, the token actually issued by rotation) still
	// rotates as a fresh first use, minting a new successor and keeping the
	// chain alive.
	_, succ3, status, err := s.RotateRefreshToken(succ, "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)
	require.NotEqual(t, succ3, "")
	require.NotEqual(t, succ3, succ)
}

func TestRefreshReplayRevokesChain(t *testing.T) {
	s := openTestStore(t)
	rt1 := "refresh-replay-1"
	require.NoError(t, s.IssueRefreshToken(rt1, "cli", ""))

	_, _, status, err := s.RotateRefreshToken(rt1, "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)

	// Travel past the reuse window.
	require.NoError(t, s.db.Model(&RefreshToken{}).Where("token = ?", rt1).Update("used_at", time.Now().Add(-time.Minute)).Error)

	// Re-present the rotated token beyond the window → replay → chain revoked.
	_, _, status, err = s.RotateRefreshToken(rt1, "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateReplay, status)

	// The whole chain is now revoked. Fetch the successor that was issued and
	// verify it, too, is revoked.
	var root RefreshToken
	require.NoError(t, s.db.Where("token = ?", rt1).First(&root).Error)
	require.True(t, root.Revoked, "token %s must be revoked with its chain", rt1)
	if root.Successor != "" {
		var succ RefreshToken
		require.NoError(t, s.db.Where("token = ?", root.Successor).First(&succ).Error)
		require.True(t, succ.Revoked, "successor %s must be revoked with its chain", root.Successor)
	}
}

func TestRefreshUnknownIsReject(t *testing.T) {
	s := openTestStore(t)
	_, _, status, err := s.RotateRefreshToken("does-not-exist", "", "")
	require.NoError(t, err)
	require.Equal(t, RotateUnknown, status)
}

func TestClientBindingMismatchRejects(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.SaveClient("cli-a", "A", nil))
	require.NoError(t, s.IssueRefreshToken("rt-a", "cli-a", ""))
	// Presenting with a different client → replay/rejection.
	_, _, status, err := s.RotateRefreshToken("rt-a", "cli-b", "")
	require.NoError(t, err)
	require.Equal(t, RotateReplay, status)
}

// TestRepeatedReuseMintsNoNewTokens guards against the weakness where every
// in-window reuse of a rotated token mints a fresh successor — that lets a
// stolen token be re-presented repeatedly to manufacture an unbounded number of
// valid pairs. Reuse must always return the SAME already-issued successor.
func TestRepeatedReuseMintsNoNewTokens(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.IssueRefreshToken("rt-reuse", "cli", ""))

	// First use rotates and mints one successor.
	_, succ1, status, err := s.RotateRefreshToken("rt-reuse", "cli", "")
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)
	require.NotEqual(t, succ1, "")

	// Pin used_at in the future so every re-presentation is deterministically
	// inside the window (no Windows wall-clock flake), then re-present many times.
	require.NoError(t, s.db.Model(&RefreshToken{}).Where("token = ?", "rt-reuse").Update("used_at", time.Now().Add(time.Hour)).Error)
	for i := 0; i < 10; i++ {
		_, succN, status, err := s.RotateRefreshToken("rt-reuse", "cli", "")
		require.NoError(t, err)
		require.Equal(t, RotateOKReused, status)
		require.Equal(t, succN, succ1, "reuse %d must return the stored successor, not mint a new pair", i)
	}

	// Only the one successor row may exist in the chain (root + 1).
	var count int64
	require.NoError(t, s.db.Model(&RefreshToken{}).Where("chain_root = ?", "rt-reuse").Count(&count).Error)
	require.Equal(t, int64(2), count, "chain must contain exactly the root and one successor, not unbounded tokens")
}

// TestPersistedClientSurvivesReopen verifies the durability contract: a client
// registered in one process is readable after reopening the store, so a fresh
// authorization-code login still works after a server restart.
func TestPersistedClientSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth.db")

	s1, err := Open(path, 30*24*time.Hour)
	require.NoError(t, err)
	require.NoError(t, s1.SaveClient("client_a", "Claude", []string{"https://claude.ai/api/mcp/auth_callback"}))
	require.NoError(t, s1.Close())

	s2, err := Open(path, 30*24*time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	uris, err := s2.ClientRedirectURIs("client_a")
	require.NoError(t, err)
	require.Equal(t, []string{"https://claude.ai/api/mcp/auth_callback"}, uris)

	all, err := s2.Clients()
	require.NoError(t, err)
	require.Contains(t, all, "client_a")
}

// TestConcurrentFirstUseRotatesOnce guards the atomic first-use claim: racing
// two presentations of the same never-used token must yield exactly one
// RotateOK (the winner); the loser is treated as a benign reuse, never a
// second independent first-use rotation.
func TestConcurrentFirstUseRotatesOnce(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.IssueRefreshToken("rt-race", "cli", ""))

	// The invariant under test is the ATOMIC first-use claim: exactly one racer
	// may win and return RotateOK; the loser must be treated as a benign in-window
	// reuse (RotateOKReused), never a second first-use. The winner/loser contention
	// on the `used_at IS NULL` UPDATE is independent of the reuse-window size, so
	// widen the window here to make the loser's classification deterministic: on
	// Windows CI the gap between the winner's used_at write and the loser's re-read
	// can exceed the test default of 100ms, which would mis-classify the loser as
	// RotateReplay (untracked by the counters below) and flake the assertion.
	s.reuseWindow = time.Hour

	type result struct {
		status RotateStatus
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, _, st, _ := s.RotateRefreshToken("rt-race", "cli", "")
			results <- result{status: st}
		}()
	}
	close(start)
	ok := 0
	reused := 0
	for i := 0; i < 2; i++ {
		switch (<-results).status {
		case RotateOK:
			ok++
		case RotateOKReused:
			reused++
		}
	}
	require.Equal(t, 1, ok, "exactly one goroutine may win the first-use rotation")
	require.Equal(t, 1, reused, "the loser must be treated as a benign race")
}

// TestAccessTokenSurvivesReopen verifies the durability contract for access
// tokens: an access token saved in one process is readable after reopening the
// store, so a connector that does not refresh on 401 (Grok's rmcp) can resume
// with a still-valid token after a server restart instead of re-authorizing.
// It also checks DeleteAccessToken removes a single token and Reap drops
// expired entries.
func TestAccessTokenSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth.db")

	s1, err := Open(path, 30*24*time.Hour)
	require.NoError(t, err)
	require.NoError(t, s1.SaveAccessToken("at-1", "cli", "https://mcp.example.com/mcp", time.Now().Add(time.Hour)))
	require.NoError(t, s1.SaveAccessToken("at-expired", "cli", "https://mcp.example.com/mcp", time.Now().Add(-time.Minute)))
	require.NoError(t, s1.Close())

	// Reopening the store surfaces the saved (including not-yet-expired) tokens.
	s2, err := Open(path, 30*24*time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	all, err := s2.AccessTokens()
	require.NoError(t, err)
	require.Contains(t, all, "at-1")
	require.Contains(t, all, "at-expired")

	// Reap drops only the expired token; the live one survives.
	require.NoError(t, s2.Reap())
	all, err = s2.AccessTokens()
	require.NoError(t, err)
	require.Contains(t, all, "at-1")
	require.NotContains(t, all, "at-expired")

	// DeleteAccessToken removes a single token.
	require.NoError(t, s2.DeleteAccessToken("at-1"))
	all, err = s2.AccessTokens()
	require.NoError(t, err)
	require.NotContains(t, all, "at-1")
}
