package gitremote

import (
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeIncrementalPackDifference(t *testing.T) {
	srcDir := t.TempDir()
	repo, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	c1 := commitFile(t, repo, "a.txt", "a\n", "one")
	c2 := commitFile(t, repo, "b.txt", "b\n", "two")

	// whole = everything reachable from c2; delta = c2 minus c1 (only c2's new
	// objects). The delta must be strictly smaller than whole.
	whole, err := EncodeIncrementalPack(repo.Storer, []plumbing.Hash{c2}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, whole)
	delta, err := EncodeIncrementalPack(repo.Storer, []plumbing.Hash{c2}, []plumbing.Hash{c1})
	require.NoError(t, err)
	t.Logf("whole=%dB delta=%dB", len(whole), len(delta))
	// The set-difference is a real optimization: the incremental pack for a second
	// commit is much smaller than the full history pack.
	assert.Less(t, len(delta), len(whole))
	assert.NotEmpty(t, delta)

	// Nothing new → empty pack.
	none, err := EncodeIncrementalPack(repo.Storer, []plumbing.Hash{c2}, []plumbing.Hash{c2})
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestResolveRevision(t *testing.T) {
	srcDir := t.TempDir()
	repo, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	h := commitFile(t, repo, "a.txt", "a\n", "one")

	// Full ref name and short branch name.
	got, err := resolveRevision(repo.Storer, "refs/heads/master")
	require.NoError(t, err)
	require.Equal(t, h, got)

	got, err = resolveRevision(repo.Storer, "master")
	require.NoError(t, err)
	require.Equal(t, h, got)

	// HEAD is a symbolic reference and must be followed.
	got, err = resolveRevision(repo.Storer, "HEAD")
	require.NoError(t, err)
	require.Equal(t, h, got)

	// Raw sha1.
	got, err = resolveRevision(repo.Storer, h.String())
	require.NoError(t, err)
	require.Equal(t, h, got)

	// Unknown revision errors.
	_, err = resolveRevision(repo.Storer, "does-not-exist")
	require.Error(t, err)
}
