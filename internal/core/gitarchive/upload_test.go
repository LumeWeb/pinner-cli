package gitarchive_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// testSession returns a Session wired to a fresh migrated DB and a fake SDK.
func testSession(t *testing.T) (*gitarchive.Session, *fakeSDK, *gorm.DB) {
	t.Helper()
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))
	sdk := newFakeSDK()
	return &gitarchive.Session{
		Profile:    "work",
		IndexerURL: "http://localhost:9980",
		DB:         db,
		SDK:        sdk,
	}, sdk, db
}

func TestUploadPackSDKRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, sdk, db := testSession(t)

	payload := []byte("PACK\x00fixture-data")
	uo, err := gitarchive.UploadPack(ctx, s, "widgets", "pack-1", true, bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), uo.Size)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(payload)), uo.Digest)
	require.Equal(t, string(objmeta.KindGitPack), string(uo.Card.Kind))
	require.Equal(t, objmeta.KindGitPack, uo.Card.Kind)

	// The SDK was driven: one upload, one pin, no other SDK calls.
	require.Len(t, sdk.pinned, 1)
	require.Len(t, sdk.objects, 1)

	// The uploaded object's metadata is the sealed card (routable as a git card).
	var keys []string
	for k := range sdk.objects {
		keys = append(keys, k.String())
	}
	require.Len(t, keys, 1)
	stored := sdk.objects[uo.Key]
	require.True(t, objmeta.IsCard(stored.meta), "object metadata must be a pinner card")

	// Exactly one git_objects row, matching the card.
	var gobjs []gitarchive.GitObject
	require.NoError(t, db.Find(&gobjs).Error)
	require.Len(t, gobjs, 1)
	require.Equal(t, "git.pack", gobjs[0].Kind)
	require.Equal(t, "widgets", gobjs[0].Locker)
	require.Equal(t, int64(len(payload)), gobjs[0].Size)
	require.Equal(t, uo.Digest, gobjs[0].Digest)
	require.Equal(t, uo.Key.String(), gobjs[0].ObjectKey)
}

func TestUploadTipSDKRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _, db := testSession(t)

	payload := []byte(`{"locker":"widgets","gen":3,"name":"widgets","lineage":[],"refs":{},"prev":"p"}`)
	uo, err := gitarchive.UploadTip(ctx, s, "widgets", 3, "prevkey", bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), uo.Size)
	require.Equal(t, objmeta.KindGitTip, uo.Card.Kind)

	var gobjs []gitarchive.GitObject
	require.NoError(t, db.Find(&gobjs).Error)
	require.Len(t, gobjs, 1)
	require.Equal(t, "git.tip", gobjs[0].Kind)
	require.Equal(t, "widgets", gobjs[0].Locker)
	require.Equal(t, uo.Digest, gobjs[0].Digest)
}

func TestUploadRejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testSession(t)

	_, err := gitarchive.UploadObject(ctx, s, objmeta.KindVaultFile, nil, bytes.NewReader([]byte("x")))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid card kind")
}
