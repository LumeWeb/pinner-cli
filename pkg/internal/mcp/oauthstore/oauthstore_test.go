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
