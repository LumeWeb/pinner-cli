package gitremote

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This test exercises the full fetch/push remote-helper protocol through go-git
// only — no Sia, no network, no git subprocess. It:
//
//  1. builds a source repo with two commits,
//  2. pushes both commits (in two separate pushes so the second one is an
//     incremental pack against the archived tip),
//  3. verifies the remote bare-repo store now serves the ref + full history, and
//  4. fetches into a brand new repo and confirms the objects arrived intact.
func TestGoGitPushThenFetch(t *testing.T) {
	ctx := context.Background()

	// 1. Source repo with two commits on refs/heads/master.
	srcDir := t.TempDir()
	src, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	c1 := commitFile(t, src, "a.txt", "hello from a\n", "first")
	c2 := commitFile(t, src, "b.txt", "hello from b\n", "second")

	// 2. Remote is a bare go-git repo wrapped in GoGitStore.
	remoteDir := t.TempDir()
	remote, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)
	store := &GoGitStore{Repo: remote}
	defer store.Close()

	// First push: full pack of everything reachable from c2.
	out1 := runHandle(t, ctx, src.Storer, store,
		"capabilities\nlist for-push\npush refs/heads/master:refs/heads/master\n\n")
	assert.Contains(t, out1, "ok refs/heads/master\n")

	ref, err := remote.Storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	require.Equal(t, c2, ref.Hash())

	// Second push: add a third commit; the incremental pack must only carry the
	// new objects not already archived.
	c3 := commitFile(t, src, "c.txt", "hello from c\n", "third")
	out2 := runHandle(t, ctx, src.Storer, store,
		"capabilities\nlist for-push\npush refs/heads/master:refs/heads/master\n\n")
	assert.Contains(t, out2, "ok refs/heads/master\n")
	ref, err = remote.Storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	require.Equal(t, c3, ref.Hash())

	// 3. Fresh empty repo fetches the advertised tip and gets full history.
	dstDir := t.TempDir()
	dst, err := git.PlainInit(dstDir, false)
	require.NoError(t, err)

	outList := runHandle(t, ctx, dst.Storer, store, "capabilities\nlist\n")
	assert.Contains(t, outList, c3.String()+" refs/heads/master\n")

	fetchIn := "capabilities\nlist\nfetch " + c3.String() + " refs/heads/master\n\n"
	runHandle(t, ctx, dst.Storer, store, fetchIn)

	// 4. Objects are present in the fresh store with full lineage.
	c3got, err := readCommit(dst.Storer, c3)
	require.NoError(t, err)
	require.Equal(t, c2, c3got.ParentHashes[0])
	// c2 (and transitively c1) transferred because Download sent a full pack.
	_, err = readCommit(dst.Storer, c2)
	require.NoError(t, err)
	_, err = readCommit(dst.Storer, c1)
	require.NoError(t, err)

	tree, err := c3got.Tree()
	require.NoError(t, err)
	names := map[string]bool{}
	_ = tree.Files().ForEach(func(f *object.File) error {
		names[f.Name] = true
		return nil
	})
	assert.True(t, names["a.txt"])
	assert.True(t, names["b.txt"])
	assert.True(t, names["c.txt"])
}

// runHandle runs the protocol engine against local + store with the given stdin
// and returns the stdout text.
func runHandle(t *testing.T, ctx context.Context, local storage.Storer, store Store, in string) string {
	t.Helper()
	var out, errw bytes.Buffer
	err := Handle(ctx, local, store, strings.NewReader(in), &out, &errw)
	require.NoError(t, err, "Handle failed: %v", err)
	return out.String()
}

// commitFile writes a file into the worktree, stages it, and commits it on
// refs/heads/master (go-git sets that ref for a non-bare repo), returning the
// new commit hash.
func commitFile(t *testing.T, repo *git.Repository, name, content, msg string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	f, err := wt.Filesystem.Create(name)
	require.NoError(t, err)
	_, werr := f.Write([]byte(content))
	require.NoError(t, werr)
	require.NoError(t, f.Close())
	_, err = wt.Add(name)
	require.NoError(t, err)
	h, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	return h
}
