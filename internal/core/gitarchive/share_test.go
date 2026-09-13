package gitarchive_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// seedArchivedLocker uploads one pack + one tip (whose payload references that
// pack) for "widgets" and returns the tip object key string and pack key string.
func seedArchivedLocker(t *testing.T, s *gitarchive.Session, gen int) (tipKey, packKey string) {
	t.Helper()
	ctx := context.Background()

	pack, err := gitarchive.UploadPack(ctx, s, "widgets", "pack-1", true,
		bytes.NewReader([]byte("PACK\x00fake-pack-data")))
	require.NoError(t, err)
	packKey = pack.Key.String()

	tipRefs := map[string]string{"refs/heads/master": strings.Repeat("a", 40)}
	tipPayload, err := json.Marshal(gitarchive.TipBytes{
		Locker:  "widgets",
		Gen:     gen,
		Name:    "widgets",
		Lineage: []string{strings.Repeat("b", 40)},
		Refs:    tipRefs,
		Packs:   []gitarchive.PackRef{{Key: packKey, Full: true, Digest: "abc"}},
	})
	require.NoError(t, err)

	tip, err := gitarchive.UploadTip(ctx, s, "widgets", gen, "prev", bytes.NewReader(tipPayload))
	require.NoError(t, err)
	tipKey = tip.Key.String()
	return tipKey, packKey
}

func TestParseShareDocument(t *testing.T) {
	doc, err := gitarchive.ParseShareDocument([]byte(
		`{"locker":"w","gen":1,"name":"w","tip_url":"u1","pack_urls":["p1"],"until":999}`))
	require.NoError(t, err)
	require.Equal(t, "w", doc.Locker)
	require.Equal(t, 1, doc.Gen)
	require.Equal(t, "u1", doc.TipURL)
	require.Equal(t, []string{"p1"}, doc.PackURLs)
	require.Equal(t, int64(999), doc.Until)
}

func TestParseShareDocumentMissingTipURL(t *testing.T) {
	_, err := gitarchive.ParseShareDocument([]byte(`{"locker":"w"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing locker or tip_url")
}

func TestParseTipBytes(t *testing.T) {
	tip, err := gitarchive.ParseTipBytes([]byte(
		`{"locker":"w","gen":2,"name":"w","lineage":["x"],"refs":{"refs/heads/master":"aaa"},"packs":[{"key":"pk1","full":true}]}`))
	require.NoError(t, err)
	require.Equal(t, 2, tip.Gen)
	require.Len(t, tip.Packs, 1)
	require.Equal(t, "pk1", tip.Packs[0].Key)
}

func TestMintShareMintsTipAndPackURLs(t *testing.T) {
	ctx := context.Background()
	s, sdk, _ := testSession(t)
	seedArchivedLocker(t, s, 1)

	until := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	res, err := gitarchive.MintShare(ctx, s, "widgets", until)
	require.NoError(t, err)
	require.Equal(t, "widgets", res.Locker)
	require.Equal(t, 1, res.Gen)
	require.NotEmpty(t, res.URL, "share document URL must be minted")
	require.NotEmpty(t, res.TipURL)
	require.Len(t, res.PackURLs, 1)
	require.Equal(t, until.Unix(), res.Document.Until)

	// The share document itself was uploaded as a git.share object and pinned.
	var shareRows int64
	require.NoError(t, s.DB.Table("git_objects").Where("kind = ?", "git.share").Count(&shareRows).Error)
	require.Equal(t, int64(1), shareRows)

	// URLs were minted for tip + one pack + the share document (3 calls),
	// and the minted URL is a self-contained bearer (not a profile-scoped key).
	require.Len(t, sdk.mintLog, 3)
	require.NotEqual(t, "", res.URL)
	require.NotContains(t, res.URL, "profile=")
}

func TestMintShareNoTip(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testSession(t)
	_, err := gitarchive.MintShare(ctx, s, "nothing", time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no archived tip")
}
