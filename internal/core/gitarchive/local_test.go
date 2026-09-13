package gitarchive_test

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// TestDeleteBindAndRepo confirms unwatch bookkeeping: a bound path is removed
// (and re-removing is a no-op), and the git_repos row for the locker gone.
func TestDeleteBindAndRepo(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	_, err := gitarchive.AddBind(db, "widgets", "/home/u/widgets")
	require.NoError(t, err)
	_, err = gitarchive.EnsureRepo(db, "widgets", "widgets", "lin")
	require.NoError(t, err)

	removed, err := gitarchive.DeleteBind(db, "/home/u/widgets")
	require.NoError(t, err)
	require.True(t, removed)

	// Re-delete is a clean no-op.
	removed, err = gitarchive.DeleteBind(db, "/home/u/widgets")
	require.NoError(t, err)
	require.False(t, removed)

	removed, err = gitarchive.DeleteRepoByLocker(db, "widgets")
	require.NoError(t, err)
	require.True(t, removed)

	removed, err = gitarchive.DeleteRepoByLocker(db, "widgets")
	require.NoError(t, err)
	require.False(t, removed)

	repos, err := gitarchive.ListRepos(db)
	require.NoError(t, err)
	require.Empty(t, repos)
}

// TestRemoveArchiveRemoteOnlyWhenOurs: an archive remote pointing at the locker
// URL is removed; a same-named remote pointing elsewhere is left untouched.
func TestRemoveArchiveRemoteOnlyWhenOurs(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	setRemote := func(url string) {
		cfg, err := repo.Config()
		require.NoError(t, err)
		cfg.Remotes[gitarchive.RemoteName] = &config.RemoteConfig{
			Name: gitarchive.RemoteName,
			URLs: []string{url},
		}
		require.NoError(t, repo.Storer.SetConfig(cfg))
	}

	// Not ours -> untouched.
	setRemote("https://example.com/other")
	removed, err := gitarchive.RemoveArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.False(t, removed)

	cfg, err := repo.Config()
	require.NoError(t, err)
	_, ok := cfg.Remotes[gitarchive.RemoteName]
	require.True(t, ok, "foreign remote must remain")

	// Ours -> removed.
	setRemote(gitarchive.GitRemoteURL("widgets"))
	removed, err = gitarchive.RemoveArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.True(t, removed)

	cfg, err = repo.Config()
	require.NoError(t, err)
	_, ok = cfg.Remotes[gitarchive.RemoteName]
	require.False(t, ok, "our remote must be removed")

	// Removing again is a no-op (remote already gone).
	removed, err = gitarchive.RemoveArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.False(t, removed)
}

// TestRepairArchiveRemote installs/corrects the archive remote to the canonical
// URL, and reports no repair when it already matches.
func TestRepairArchiveRemote(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	// Missing remote -> repaired.
	repaired, err := gitarchive.RepairArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.True(t, repaired)

	cfg, err := repo.Config()
	require.NoError(t, err)
	require.Equal(t, gitarchive.GitRemoteURL("widgets"), cfg.Remotes[gitarchive.RemoteName].URLs[0])

	// Already correct -> no repair.
	repaired, err = gitarchive.RepairArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.False(t, repaired)

	// Wrong URL -> repaired.
	cfg.Remotes[gitarchive.RemoteName] = &config.RemoteConfig{
		Name: gitarchive.RemoteName,
		URLs: []string{"https://bad.example/x"},
	}
	require.NoError(t, repo.Storer.SetConfig(cfg))
	repaired, err = gitarchive.RepairArchiveRemote(dir, "widgets")
	require.NoError(t, err)
	require.True(t, repaired)

	cfg, err = repo.Config()
	require.NoError(t, err)
	require.Equal(t, gitarchive.GitRemoteURL("widgets"), cfg.Remotes[gitarchive.RemoteName].URLs[0])
}

// TestPrivateBareOpenCreateAndReopen verifies the private mirror is created as a
// bare repo on first open and reopens cleanly, and that its path lives under
// the profile git dir.
func TestPrivateBareOpenCreateAndReopen(t *testing.T) {
	overrideHome(t, t.TempDir())

	// The profile git dir helpers resolve under the profile dir.
	profileDir := vault.ProfileDir("work")
	repoDir := gitarchive.PrivateRepoDir("work", "widgets")
	require.Contains(t, repoDir, filepath.Join(profileDir, "git"))
	require.Equal(t, "widgets.git", filepath.Base(repoDir))

	dir := t.TempDir()
	repo, err := gitarchive.OpenPrivateBare(dir)
	require.NoError(t, err)
	require.NotNil(t, repo)

	// Reopen an existing mirror.
	repo2, err := gitarchive.OpenPrivateBare(dir)
	require.NoError(t, err)
	require.NotNil(t, repo2)
}
