package gitarchive_test

import (
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// initCommit creates a fresh non-bare repo and commits one file, returning the
// repo and the new commit hash.
func initCommit(t *testing.T, name, content string) (*git.Repository, plumbing.Hash) {
	t.Helper()
	repo, err := git.PlainInit(t.TempDir(), false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	f, err := wt.Filesystem.Create(name)
	require.NoError(t, err)
	_, werr := f.Write([]byte(content))
	require.NoError(t, werr)
	require.NoError(t, f.Close())
	_, err = wt.Add(name)
	require.NoError(t, err)
	h, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	return repo, h
}

func TestComputeLineageSingleRoot(t *testing.T) {
	repo, h := initCommit(t, "a.txt", "hello")

	wt, err := repo.Worktree()
	require.NoError(t, err)
	lineage, err := gitarchive.ComputeLineage(wt.Filesystem.Root())
	require.NoError(t, err)
	require.Len(t, lineage, 1)
	require.Equal(t, h, lineage[0])
}

func TestLineageKeyIsSortedFingerprint(t *testing.T) {
	a := initCommitT(t, "a.txt")
	b := initCommitT(t, "b.txt")

	key := gitarchive.LineageKey([]plumbing.Hash{b, a})
	key2 := gitarchive.LineageKey([]plumbing.Hash{a, b})
	require.Equal(t, key, key2, "LineageKey must be order-independent")
	// The canonical form is the lexicographically sorted join of both hashes.
	expected := a.String() + "," + b.String()
	if b.String() < a.String() {
		expected = b.String() + "," + a.String()
	}
	require.Equal(t, expected, key)
}

// initCommitT is a thin wrapper returning only the commit hash for tests that
// just need distinct root commits.
func initCommitT(t *testing.T, name string) plumbing.Hash {
	_, h := initCommit(t, name, "content")
	return h
}

func TestDefaultLockerName(t *testing.T) {
	require.Equal(t, "myproject", gitarchive.DefaultLockerName("/home/u/code/myproject"))
	require.Equal(t, ".", gitarchive.DefaultLockerName("."))
}
