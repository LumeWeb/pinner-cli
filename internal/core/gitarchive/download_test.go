package gitarchive_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func TestFetchObjectBytesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testSession(t)

	payload := []byte(`{"locker":"widgets","gen":3,"name":"widgets","lineage":[],"refs":{},"prev":"p"}`)
	uo, err := gitarchive.UploadTip(ctx, s, "widgets", 3, "prevkey", bytes.NewReader(payload))
	require.NoError(t, err)

	downloaded, data, err := gitarchive.FetchObjectBytes(ctx, s, uo.Key)
	require.NoError(t, err)
	require.Equal(t, payload, []byte(data))
	require.Equal(t, uo.Key, downloaded.Key)
	require.Equal(t, "git.tip", string(downloaded.Card.Kind))
}

func TestFetchObjectReader(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testSession(t)

	payload := []byte("PACK\x00reader-payload")
	uo, err := gitarchive.UploadPack(ctx, s, "widgets", "pack-1", false, bytes.NewReader(payload))
	require.NoError(t, err)

	downloaded, err := gitarchive.FetchObject(ctx, s, uo.Key)
	require.NoError(t, err)
	defer downloaded.Reader.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(downloaded.Reader)
	require.NoError(t, err)
	require.Equal(t, payload, buf.Bytes())
}

func TestFetchObjectNotFound(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testSession(t)

	// No object uploaded: fetching a key the fake has never seen must error.
	_, err := gitarchive.FetchObject(ctx, s, objectKey(t))
	require.Error(t, err)
}

func TestFetchObjectBytesVerifiesDigest(t *testing.T) {
	ctx := context.Background()
	s, sdk, _ := testSession(t)

	payload := []byte("PACK\x00integrity-payload")
	uo, err := gitarchive.UploadPack(ctx, s, "widgets", "pack-1", false, bytes.NewReader(payload))
	require.NoError(t, err)

	// Corrupt the stored payload so it no longer matches the card digest.
	sdk.mu.Lock()
	fs := sdk.objects[uo.Key]
	fs.data = append(append([]byte{}, fs.data...), []byte("TAMPER")...)
	sdk.objects[uo.Key] = fs
	sdk.mu.Unlock()

	_, _, err = gitarchive.FetchObjectBytes(ctx, s, uo.Key)
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest mismatch")
}
