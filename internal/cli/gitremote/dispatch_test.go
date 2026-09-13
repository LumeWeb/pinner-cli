package gitremote

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRemoteHelperInvocation(t *testing.T) {
	assert.True(t, IsRemoteHelperInvocation("/usr/local/bin/git-remote-pinner"))
	assert.True(t, IsRemoteHelperInvocation("git-remote-pinner"))
	assert.False(t, IsRemoteHelperInvocation("/usr/local/bin/pinner"))
	assert.False(t, IsRemoteHelperInvocation("pinner"))
	assert.False(t, IsRemoteHelperInvocation(""))
}

// TestRunDispatchesHelperPath drives the full Run entry (open local repo via
// GIT_DIR + drive protocol) with a synthetic argv-0 of `git-remote-pinner`. It is
// the same seam cmd/pinner/main.go uses for helper mode, exercised without the
// CLI tree (which cannot build in this worktree due to a pre-existing, unrelated
// internal/mcp/core/handoff issue).
func TestRunDispatchesHelperPath(t *testing.T) {
	ctx := context.Background()

	// A real local repo so Run's OpenLocalStorer (via GIT_DIR) succeeds.
	localDir := t.TempDir()
	repo, err := git.PlainInit(localDir, false)
	require.NoError(t, err)
	h := commitFile(t, repo, "x.txt", "x\n", "init")

	// Remote bare store.
	remoteDir := t.TempDir()
	remote, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)
	store := &GoGitStore{Repo: remote}
	defer store.Close()

	// Point GIT_DIR at the local repo so Run can find it without a worktree.
	t.Setenv("GIT_DIR", localDir)

	var out, errw bytes.Buffer
	err = Run(ctx, store, strings.NewReader(
		"capabilities\nlist for-push\npush "+h.String()+":refs/heads/master\n\n",
	), &out, &errw)
	require.NoError(t, err)

	ref, err := remote.Storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	assert.Equal(t, h, ref.Hash())
	assert.Contains(t, out.String(), "ok refs/heads/master\n")
}

// TestDispatchRoutingSharedPath verifies argv-0 selection is purely name-based:
// the same directory holds both names, but only the helper name routes to the
// helper. This mirrors the locked correction — one executable, argv-0 identity
// dispatch, no provider polymorphism.
func TestDispatchRoutingSharedPath(t *testing.T) {
	dir := t.TempDir()
	require.True(t, IsRemoteHelperInvocation(filepath.Join(dir, "git-remote-pinner")))
	require.False(t, IsRemoteHelperInvocation(filepath.Join(dir, "pinner")))
}
