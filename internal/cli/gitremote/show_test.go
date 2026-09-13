package gitremote

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/require"
)

// TestEnsureArchiveRefsAndRender drives `git show` end-to-end with only go-git:
// it pushes two commits to a bare GoGitStore, ensures the archive refs into a
// fresh bare mirror, and renders refs + commit log + tree files. No Sia, no git
// subprocess.
func TestEnsureArchiveRefsAndRender(t *testing.T) {
	ctx := context.Background()

	// Push two commits into an archive backed by a bare go-git store.
	srcDir := t.TempDir()
	src, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	c1 := commitFile(t, src, "a.txt", "hello a\n", "first")
	c2 := commitFile(t, src, "b.txt", "hello b\n", "second")

	remoteDir := t.TempDir()
	remote, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)
	store := &GoGitStore{Repo: remote}
	defer func() { _ = store.Close() }()

	out := runHandle(t, ctx, src.Storer, store,
		"capabilities\nlist for-push\npush refs/heads/master:refs/heads/master\n\n")
	require.Contains(t, out, "ok refs/heads/master\n")

	// Fresh bare mirror becomes the private show view.
	mirrorDir := t.TempDir()
	mirror, err := git.PlainInit(mirrorDir, true)
	require.NoError(t, err)

	refs, err := EnsureArchiveRefs(ctx, mirror.Storer, store)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)
	require.Equal(t, c2, refs[0].Hash)

	report, err := Render(mirror.Storer, 0)
	require.NoError(t, err)

	// Refs surfaced.
	require.Len(t, report.Refs, 1)
	require.Equal(t, "refs/heads/master", report.Refs[0].Name)

	// Commit log is newest-first and covers both commits.
	require.Len(t, report.Commits, 2)
	require.Equal(t, c2.String(), report.Commits[0].Hash)
	require.Equal(t, c1.String(), report.Commits[1].Hash)

	// Tree listing covers both files, sorted by path.
	require.Len(t, report.Tree, 2)
	require.Equal(t, "a.txt", report.Tree[0].Path)
	require.Equal(t, "b.txt", report.Tree[1].Path)
	require.Greater(t, report.Tree[0].Size, int64(0))
	require.NotEmpty(t, report.Tree[0].Mode)
	require.NotEmpty(t, report.Tree[0].Hash)
}

// TestRenderEmptyMirror confirms an empty (never ensured) mirror renders with no
// refs, no commits and no files rather than erroring.
func TestRenderEmptyMirror(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, true)
	require.NoError(t, err)

	report, err := Render(repo.Storer, 0)
	require.NoError(t, err)
	require.Empty(t, report.Refs)
	require.Empty(t, report.Commits)
	require.Empty(t, report.Tree)
}

// TestShowReportOmitsObjectKeys guards the default-output contract: the show
// report may carry only git-side identifiers (sha1 hashes, paths, sizes), never
// Sia object/slab/pin keys. It reflects over the exported JSON field names of
// every type rendered by `git show` and asserts none is named like a backend
// key. This is a structural guard so a future refactor cannot quietly add a
// slab/object key to the default output.
func TestShowReportOmitsObjectKeys(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(ShowReport{}),
		reflect.TypeOf(CommitLine{}),
		reflect.TypeOf(TreeLine{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.ToLower(field.Name)
			for _, banned := range []string{"objectkey", "slab", "pin", "key"} {
				require.False(t, strings.Contains(name, banned),
					"%s.%s must not expose a backend %q key in default output",
					typ.Name(), field.Name, banned)
			}
		}
	}
}
