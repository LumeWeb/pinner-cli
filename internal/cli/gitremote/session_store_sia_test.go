//go:build gitarchive_sia

package gitremote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// siaFakeSDK is a minimal SDKClient for exercising the Sia-backed SessionStore
// publish/list/delete flow without a live indexer. Uploads are stored by
// content address.
//
// NOTE ON SCOPE: like the shared fakeSDK in the gitarchive package, this fake
// keys objects on siastorage.Object.ID(), which is a hash of the object's slabs.
// An in-memory Upload cannot fabricate real slabs, so every object collapses to
// the same empty-slab ID and "last write wins". That is sufficient to exercise
// the store's publish → list → delete path (the tip is always the object last
// written, so the tip reads back correctly), but NOT a multi-pack download
// reconstruction (which would need per-object keys). Multi-write download
// coverage would require a keyed fake the shared SDKClient seam does not provide
// — the same limitation the existing gitarchive fakeSDK has. Compile + vet +
// single-object publish/download digest coverage (download_test.go) plus this
// publish/list/delete coverage are the feasible gate for the tagged build.
type siaFakeSDK struct {
	mu      sync.Mutex
	objects map[types.Hash256]siaStored
}

type siaStored struct {
	meta json.RawMessage
	data []byte
}

func newSiaFakeSDK() *siaFakeSDK {
	return &siaFakeSDK{objects: map[types.Hash256]siaStored{}}
}

func (f *siaFakeSDK) Upload(ctx context.Context, obj *siastorage.Object, r io.Reader, _ ...siastorage.UploadOption) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[obj.ID()] = siaStored{meta: obj.Metadata(), data: data}
	return nil
}
func (f *siaFakeSDK) PinObject(ctx context.Context, obj siastorage.Object) error { return nil }
func (f *siaFakeSDK) Object(ctx context.Context, key types.Hash256) (siastorage.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.objects[key]
	if !ok {
		return siastorage.Object{}, fmt.Errorf("fake: object %s not found", key)
	}
	o := siastorage.NewEmptyObject()
	o.UpdateMetadata(fs.meta)
	return o, nil
}
func (f *siaFakeSDK) Download(obj siastorage.Object, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.objects[obj.ID()]
	if !ok {
		return nil, fmt.Errorf("fake: no data for object %s", obj.ID())
	}
	return io.NopCloser(bytes.NewReader(fs.data)), nil
}
func (f *siaFakeSDK) CreateSharedObjectURL(ctx context.Context, key types.Hash256, until time.Time) (string, error) {
	return "_shared_" + key.String(), nil
}
func (f *siaFakeSDK) DownloadSharedObject(ctx context.Context, url string, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	return nil, fmt.Errorf("fake: no shared download in this test")
}
func (f *siaFakeSDK) Close() error { return nil }

// siaTestStore builds a Session wired to a fresh migrated DB + fake SDK and a
// SessionStore scoped to locker.
func siaTestStore(t *testing.T, locker string) (*SessionStore, *siaFakeSDK, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, gitarchive.AutoMigrate(db))
	sdk := newSiaFakeSDK()
	s := &gitarchive.Session{Profile: "work", IndexerURL: "http://localhost:9980", DB: db, SDK: sdk}
	return NewSessionStoreFromSession(s).ForLocker(locker), sdk, db
}

// makeRepoWithCommit builds a repo containing a single commit with the given
// file content and returns its storer and the commit hash.
func makeRepoWithCommit(t *testing.T, name, content string) (*git.Repository, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	fname := name + ".txt"
	f, err := wt.Filesystem.Create(fname)
	require.NoError(t, err)
	_, err = f.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = wt.Add(fname)
	require.NoError(t, err)
	_, err = wt.Commit("add "+fname, &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	return repo, head.Hash()
}

func TestSiaStorePublishList(t *testing.T) {
	ctx := context.Background()

	repo, tip := makeRepoWithCommit(t, "a", "hello")
	pack, err := EncodeIncrementalPack(repo.Storer, []plumbing.Hash{tip}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, pack)

	store, _, db := siaTestStore(t, "widgets")

	// Mirror `pinner git watch`: the git_repos row is registered before the
	// store publishes (publish records the tip generation onto it).
	require.NoError(t, db.Create(&gitarchive.GitRepo{Locker: "widgets", Name: "widgets"}).Error)

	// First publish of refs/heads/master with a full pack.
	require.NoError(t, store.Upload(ctx, "refs/heads/master", tip.String(), bytes.NewReader(pack)))

	// The tip generation was recorded in git_repos.
	var repoRow gitarchive.GitRepo
	require.NoError(t, db.Where("locker = ?", "widgets").First(&repoRow).Error)
	require.Equal(t, 1, repoRow.TipGen)

	// List advertises the ref from the current tip.
	refs, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)
	require.Equal(t, tip, refs[0].Hash)
}

func TestSiaStoreDelete(t *testing.T) {
	ctx := context.Background()
	store, _, _ := siaTestStore(t, "widgets")

	repo, tip := makeRepoWithCommit(t, "one", "v1")
	pack, err := EncodeIncrementalPack(repo.Storer, []plumbing.Hash{tip}, nil)
	require.NoError(t, err)
	require.NoError(t, store.Upload(ctx, "refs/heads/master", tip.String(), bytes.NewReader(pack)))

	refs, err := store.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "refs/heads/master", refs[0].Name)

	// Delete publishes a new tip without the ref.
	require.NoError(t, store.Delete(ctx, "refs/heads/master"))
	refs, err = store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, refs, 0, "deleted ref must no longer be advertised")
}

func TestSiaStoreDispatchRemote(t *testing.T) {
	locker, ok := ParseLockedRemote("pinner::widgets")
	require.True(t, ok)
	require.Equal(t, "widgets", locker)

	_, ok = ParseLockedRemote("pinner::share/abc")
	require.False(t, ok, "a share remote is not an account locker remote")
	share, ok := ParseShareRemote("pinner::share/abc")
	require.True(t, ok)
	require.Equal(t, "abc", share)
}

// TestSiaStorePublishRefusesMovedGeneration exercises the generation CAS on the
// publish path: casPublish must refuse to publish when the archived tip moved
// after the caller read it (a concurrent publish), and must accept the current
// tip key.
func TestSiaStorePublishRefusesMovedGeneration(t *testing.T) {
	ctx := context.Background()
	store, sdk, db := siaTestStore(t, "widgets")

	// Two distinct valid 32-byte keys: keyA is the "current" archived tip key
	// the store's caller read; keyB is a stale/foreign key the caller based its
	// new tip on (i.e. a generation that has since moved).
	keyA, err := gitarchive.ParseObjectKey("01" + strings.Repeat("0", 62))
	require.NoError(t, err)
	keyB := "02" + strings.Repeat("0", 62)
	require.NotEqual(t, keyA.String(), keyB)

	// The tip payload loadTip must be able to fetch and parse.
	tipData, err := json.Marshal(&gitarchive.TipBytes{
		Locker: "widgets", Gen: 5,
		Refs:  map[string]string{"refs/heads/master": "abc"},
		Packs: []gitarchive.PackRef{},
	})
	require.NoError(t, err)

	// Seed the fake SDK with the tip object at keyA. An empty card digest skips
	// the download integrity check so the fixture need not self-verify.
	card := objmeta.New(objmeta.KindGitTip, int64(len(tipData)), "", time.Now().Unix(), nil)
	raw, err := objmeta.Encode(card)
	require.NoError(t, err)
	// The fake's Upload stores by empty-slab obj.ID() (a single collapsed key),
	// so store the payload under both that ID (for Download) and keyA (for
	// Object-by-key lookup) — mirroring how generalized-object stores behave.
	emptyObj := siastorage.NewEmptyObject()
	sdk.mu.Lock()
	fixture := siaStored{meta: raw, data: tipData}
	sdk.objects[emptyObj.ID()] = fixture
	sdk.objects[keyA] = fixture
	sdk.mu.Unlock()

	// Record the tip row in the local cache DB (the CAS coordination point).
	tipBody, err := json.Marshal(objmeta.TipBody{Locker: "widgets", Gen: 5})
	require.NoError(t, err)
	require.NoError(t, db.Create(&gitarchive.GitObject{
		ObjectKey: keyA.String(), Locker: "widgets", Kind: string(objmeta.KindGitTip), Body: tipBody,
	}).Error)

	// CAS against a stale key must refuse: the archive generation moved after
	// the caller read it.
	err = store.casPublish(ctx, keyB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "moved")
	require.Contains(t, err.Error(), "concurrent publish")

	// CAS against the current tip key passes.
	require.NoError(t, store.casPublish(ctx, keyA.String()))
}
