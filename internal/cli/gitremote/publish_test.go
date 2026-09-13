package gitremote

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublishAllFirstPublish pushes every local branch as a full pack into an
// empty archive (a bare go-git repo) so full history is available remotely.
func TestPublishAllFirstPublish(t *testing.T) {
	ctx := context.Background()

	srcDir := t.TempDir()
	src, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	c1 := commitFile(t, src, "a.txt", "a\n", "first")
	c2 := commitFile(t, src, "b.txt", "b\n", "second")

	remoteDir := t.TempDir()
	remote, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)
	store := &GoGitStore{Repo: remote}
	defer store.Close()

	refs, err := PublishAll(ctx, src.Storer, store)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)
	require.Equal(t, c2, refs[0].Hash)

	// The archive now serves the branch tip and full history (both commits).
	ref, err := remote.Storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	require.Equal(t, c2, ref.Hash())
	_, err = readCommit(remote.Storer, c2)
	require.NoError(t, err)
	_, err = readCommit(remote.Storer, c1)
	require.NoError(t, err)
}

func TestLocalBranches(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	commitFile(t, repo, "x.txt", "x\n", "init")

	refs, err := LocalBranches(repo.Storer)
	require.NoError(t, err)
	// Only branches are reported (HEAD symbolic + refs/heads/*).
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)
}

func TestDivergence(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	c1 := commitFile(t, repo, "a.txt", "a\n", "first")
	c2 := commitFile(t, repo, "b.txt", "b\n", "second")

	ahead, behind, err := Divergence(repo.Storer, c2, c1)
	require.NoError(t, err)
	assert.Equal(t, 1, ahead)
	assert.Equal(t, 0, behind)

	ahead, behind, err = Divergence(repo.Storer, c1, c2)
	require.NoError(t, err)
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 1, behind)

	ahead, behind, err = Divergence(repo.Storer, c2, c2)
	require.NoError(t, err)
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 0, behind)
}
