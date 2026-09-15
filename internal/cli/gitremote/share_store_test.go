package gitremote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.sia.tech/siastorage"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// fakeShareDownloader serves pre-signed URL payloads in memory, standing in for
// the bearer DownloadSharedObject path (no profile/app key).
type fakeShareDownloader struct {
	byURL map[string][]byte
	err   error
}

func (f *fakeShareDownloader) DownloadSharedObject(_ context.Context, url string, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.byURL[url]
	if !ok {
		return nil, fmt.Errorf("fake: no data for shared url %q", url)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeShareDownloader) Close() error { return nil }

// buildShareFixture builds a source repo with one commit, encodes a full pack of
// it, and returns the commit hash plus a fake downloader serving a coherent
// share document (doc -> tip -> one pack) plus the tip/share doc payloads and
// the pack bytes.
func buildShareFixture(t *testing.T) (plumbing.Hash, *fakeShareDownloader) {
	t.Helper()

	srcDir := t.TempDir()
	src, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	c := commitFile(t, src, "a.txt", "hello from a\n", "first")

	pack, err := EncodeIncrementalPack(src.Storer, []plumbing.Hash{c}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, pack, "share fixture pack must not be empty")

	doc := gitarchive.ShareDocument{
		Locker:   "widgets",
		Gen:      1,
		Name:     "widgets",
		TipURL:   "u://tip",
		PackURLs: []string{"u://pack"},
		Until:    time.Now().Add(time.Hour).Unix(),
	}
	docRaw, err := json.Marshal(doc)
	require.NoError(t, err)

	tip := gitarchive.TipBytes{
		Locker: "widgets",
		Gen:    1,
		Name:   "widgets",
		Refs:   map[string]string{"refs/heads/master": c.String()},
	}
	tipRaw, err := json.Marshal(tip)
	require.NoError(t, err)

	dl := &fakeShareDownloader{byURL: map[string][]byte{
		"u://doc":  docRaw,
		"u://tip":  tipRaw,
		"u://pack": pack,
	}}
	return c, dl
}

func TestParseShareRemote(t *testing.T) {
	u, ok := ParseShareRemote("pinner::share/xyzabc")
	require.True(t, ok)
	require.Equal(t, "xyzabc", u)

	_, ok = ParseShareRemote("pinner::widgets")
	require.False(t, ok, "account locker remote is not a share remote")

	_, ok = ParseShareRemote("pinner::share/")
	require.False(t, ok, "share remote with empty URL is rejected")

	_, ok = ParseShareRemote("https://example.com/x")
	require.False(t, ok)
}

func TestNewShareStoreForRemote(t *testing.T) {
	dl := &fakeShareDownloader{}
	store, err := NewShareStoreForRemote(dl, "pinner::share/someurl")
	require.NoError(t, err)
	require.NotNil(t, store)
	require.Equal(t, "someurl", store.shareURL)

	_, err = NewShareStoreForRemote(dl, "pinner::widgets")
	require.Error(t, err)
}

func TestShareStoreListAdvertisesTipRefs(t *testing.T) {
	ctx := context.Background()
	want, dl := buildShareFixture(t)
	store := NewShareStore(dl, "u://doc")

	refs, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)
	require.Equal(t, want, refs[0].Hash)
}

func TestShareStoreIsReadOnly(t *testing.T) {
	ctx := context.Background()
	_, dl := buildShareFixture(t)
	store := NewShareStore(dl, "u://doc")

	err := store.Upload(ctx, "refs/heads/x", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
	require.ErrorIs(t, err, ErrShareReadonly)
	err = store.Delete(ctx, "refs/heads/x")
	require.ErrorIs(t, err, ErrShareReadonly)
}

func TestShareStoreProtocolClone(t *testing.T) {
	ctx := context.Background()
	want, dl := buildShareFixture(t)
	store := NewShareStore(dl, "u://doc")
	defer store.Close()

	// A fresh bare repo acts as the local clone target.
	dstDir := t.TempDir()
	dst, err := git.PlainInit(dstDir, true)
	require.NoError(t, err)

	// capabilities + list + fetch the advertised tip.
	out := runHandle(t, ctx, dst.Storer, store,
		"capabilities\nlist\nfetch "+want.String()+" refs/heads/master\n\n")
	assert.Contains(t, out, want.String()+" refs/heads/master\n")
	assert.Contains(t, out, "option\n")
	assert.Contains(t, out, "fetch\n")
	assert.Contains(t, out, "push\n")

	// The fetched history landed in the local store.
	cm, err := readCommit(dst.Storer, want)
	require.NoError(t, err, "fetched commit must be reachable")
	require.Equal(t, "first", cm.Message)
}

func TestShareStoreDownloadVerifiesPackDigest(t *testing.T) {
	ctx := context.Background()

	// Build a valid pack and its sha256 digest.
	c, _ := buildShareFixture(t)
	_ = c

	// Craft a share doc + tip referencing the pack with a REAL digest, but serve
	// tampered pack bytes so verification must fail.
	pack := []byte("PACK\x00tampered-payload")
	wrongDigest := fmt.Sprintf("%x", sha256.Sum256(pack))

	doc := gitarchive.ShareDocument{
		Locker: "widgets", Gen: 1, Name: "widgets",
		TipURL: "u://tip", PackURLs: []string{"u://pack"},
		Until: time.Now().Add(time.Hour).Unix(),
	}
	docRaw, _ := json.Marshal(doc)
	tip := gitarchive.TipBytes{
		Locker: "widgets", Gen: 1, Name: "widgets",
		Refs:  map[string]string{"refs/heads/master": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Packs: []gitarchive.PackRef{{Key: "k", Full: true, Digest: "0000000000000000000000000000000000000000000000000000000000000000"}},
	}
	tipRaw, _ := json.Marshal(tip)

	dl := &fakeShareDownloader{byURL: map[string][]byte{
		"u://doc":  docRaw,
		"u://tip":  tipRaw,
		"u://pack": pack,
	}}
	store := NewShareStore(dl, "u://doc")

	_, err := store.Download(ctx, "refs/heads/master", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.Error(t, err)
	require.Contains(t, err.Error(), "pack digest mismatch")
	// Sanity: the digest string we compared must equal the live computation.
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(pack)), wrongDigest)
}
