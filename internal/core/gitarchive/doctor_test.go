package gitarchive_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

func TestLocalDoctorNotBound(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	report, err := gitarchive.LocalDoctor(db, "/home/u/unbound")
	require.NoError(t, err)
	require.False(t, report.Bound)
	require.Empty(t, report.Locker)
}

func TestLocalDoctorHealthyAndCold(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	_, err := gitarchive.AddBind(db, "widgets", "/home/u/widgets")
	require.NoError(t, err)
	_, err = gitarchive.EnsureRepo(db, "widgets", "widgets", "lin")
	require.NoError(t, err)

	report, err := gitarchive.LocalDoctor(db, "/home/u/widgets")
	require.NoError(t, err)
	require.True(t, report.Bound)
	require.Equal(t, "widgets", report.Locker)
	require.True(t, report.RepoRegistered)
	require.True(t, report.Cold, "no objects ingested yet -> cold")
	require.False(t, report.HasTipObject)
	require.False(t, report.HasPackObject)
}

func TestLocalDoctorWithObjects(t *testing.T) {
	s, _, _ := testSession(t)
	_, err := gitarchive.AddBind(s.DB, "widgets", "/home/u/widgets")
	require.NoError(t, err)

	// Ingest one tip and one pack card (distinct object keys) so doctor sees
	// both kinds recorded.
	for _, tc := range []struct {
		key  string
		kind objmeta.Kind
		body string
	}{{
		key:  "aa00tipkey",
		kind: objmeta.KindGitTip,
		body: `{"locker":"widgets","gen":1}`,
	}, {
		key:  "bb00packkey",
		kind: objmeta.KindGitPack,
		body: `{"locker":"widgets","pack":"pack-1","full":true}`,
	}} {
		raw := card(t, tc.kind, tc.body)
		ok, err := gitarchive.IngestCard(s.DB, tc.key, raw)
		require.NoError(t, err)
		require.True(t, ok)
	}

	report, err := gitarchive.LocalDoctor(s.DB, "/home/u/widgets")
	require.NoError(t, err)
	require.True(t, report.Bound)
	require.False(t, report.Cold)
	require.True(t, report.HasTipObject)
	require.True(t, report.HasPackObject)
	require.EqualValues(t, 2, report.ObjectCount)
}
