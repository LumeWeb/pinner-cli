package oauthstore

import (
	"fmt"
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
	rt1, rt2 := "refresh-1", "refresh-2"

	// Issue the root refresh token (chain root).
	require.NoError(t, s.SaveClient("cli", "Claude", []string{"https://claude.ai/api/mcp/auth_callback"}))
	require.NoError(t, s.IssueRefreshToken(rt1, "cli", ""))

	// First use: rotate → accept.
	client, status, err := s.RotateRefreshToken(rt1, "cli", "", rt2)
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)
	require.Equal(t, "cli", client)

	// Reuse the same token within the reuse window → benign, chain not revoked.
	_, status, err = s.RotateRefreshToken(rt1, "cli", "", "refresh-3")
	require.NoError(t, err)
	require.Equal(t, RotateOKReused, status)

	// A never-used token in the same chain (rt2, the first successor) still
	// rotates as a fresh first use.
	_, status, err = s.RotateRefreshToken(rt2, "cli", "", "refresh-4")
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)
}

func TestRefreshReplayRevokesChain(t *testing.T) {
	s := openTestStore(t)
	rt1, rt2 := "refresh-replay-1", "refresh-replay-2"
	require.NoError(t, s.IssueRefreshToken(rt1, "cli", ""))

	_, status, err := s.RotateRefreshToken(rt1, "cli", "", rt2)
	require.NoError(t, err)
	require.Equal(t, RotateOK, status)

	// Travel past the reuse window.
	require.NoError(t, s.db.Model(&RefreshToken{}).Where("token = ?", rt1).Update("used_at", time.Now().Add(-time.Minute)).Error)

	// Re-present the rotated token beyond the window → replay → chain revoked.
	_, status, err = s.RotateRefreshToken(rt1, "cli", "", "refresh-replay-3")
	require.NoError(t, err)
	require.Equal(t, RotateReplay, status)

	// The whole chain (including the successor) is now revoked.
	for _, tok := range []string{rt1, rt2} {
		var rt RefreshToken
		require.NoError(t, s.db.Where("token = ?", tok).First(&rt).Error)
		require.True(t, rt.Revoked, "token %s must be revoked with its chain", tok)
	}
}

func TestRefreshUnknownIsReject(t *testing.T) {
	s := openTestStore(t)
	_, status, err := s.RotateRefreshToken("does-not-exist", "", "", "x")
	require.NoError(t, err)
	require.Equal(t, RotateUnknown, status)
}

func TestClientBindingMismatchRejects(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.SaveClient("cli-a", "A", nil))
	require.NoError(t, s.IssueRefreshToken("rt-a", "cli-a", ""))
	// Presenting with a different client → replay/rejection.
	_, status, err := s.RotateRefreshToken("rt-a", "cli-b", "", "rt-b")
	require.NoError(t, err)
	require.Equal(t, RotateReplay, status)
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

	type result struct {
		status RotateStatus
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, st, _ := s.RotateRefreshToken("rt-race", "cli", "", fmt.Sprintf("succ-%d", i))
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
