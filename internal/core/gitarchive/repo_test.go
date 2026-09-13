package gitarchive_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func TestEnsureRepoUpsertAndMatch(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	lineage := "aaa123,bbb456"
	repo, err := gitarchive.EnsureRepo(db, "widgets", "widgets", lineage)
	require.NoError(t, err)
	require.Equal(t, "widgets", repo.Locker)
	require.Equal(t, lineage, repo.Lineage)

	// Match by lineage returns the locker.
	matches, err := gitarchive.FindReposByLineage(db, lineage)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, "widgets", matches[0].Locker)

	// Upserting the same locker updates the lineage without duplicating rows.
	_, err = gitarchive.EnsureRepo(db, "widgets", "widgets", "newlineage")
	require.NoError(t, err)
	repos, err := gitarchive.ListRepos(db)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "newlineage", repos[0].Lineage)

	// Matches for an unknown lineage are empty.
	matches, err = gitarchive.FindReposByLineage(db, "zzz")
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestFindReposByLineageAmbiguity(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	lineage := "sharedroot"
	_, err := gitarchive.EnsureRepo(db, "widgets-a", "a", lineage)
	require.NoError(t, err)
	_, err = gitarchive.EnsureRepo(db, "widgets-b", "b", lineage)
	require.NoError(t, err)

	matches, err := gitarchive.FindReposByLineage(db, lineage)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	// Ordered by locker name.
	require.Equal(t, "widgets-a", matches[0].Locker)
	require.Equal(t, "widgets-b", matches[1].Locker)
}

func TestAddBindAndFindByRepo(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	_, err := gitarchive.AddBind(db, "widgets", "/home/u/widgets")
	require.NoError(t, err)

	bind, err := gitarchive.FindBindByRepo(db, "/home/u/widgets")
	require.NoError(t, err)
	require.NotNil(t, bind)
	require.Equal(t, "widgets", bind.Locker)
	require.Equal(t, gitarchive.RemoteName, bind.Remote)

	// Not-bound path returns nil, not an error.
	bind, err = gitarchive.FindBindByRepo(db, "/home/u/other")
	require.NoError(t, err)
	require.Nil(t, bind)

	// Rebinding the same locker updates the path without a duplicate row.
	_, err = gitarchive.AddBind(db, "widgets", "/home/u/new-location")
	require.NoError(t, err)
	bind, err = gitarchive.FindBindByRepo(db, "/home/u/new-location")
	require.NoError(t, err)
	require.NotNil(t, bind)

	var count int64
	require.NoError(t, db.Model(&gitarchive.GitBind{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestGitRemoteURL(t *testing.T) {
	require.Equal(t, "pinner::widgets", gitarchive.GitRemoteURL("widgets"))
}
