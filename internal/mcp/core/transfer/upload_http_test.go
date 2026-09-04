package transfer

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/credctx"
)

// TestUploadCredentialPropagatesToExecutor verifies that a JWT captured at
// Prepare-mint time is stamped onto the context handed to the async upload
// executor when the minted endpoint receives its PUT, so a hosted
// (Portal-embedded) upload authenticates as the calling user.
func TestUploadCredentialPropagatesToExecutor(t *testing.T) {
	var got atomic.Value
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		b, _ := io.ReadAll(reader)
		got.Store(struct {
			body string
			jwt  string
		}{string(b), credctx.From(ctx)})
		return map[string]any{"cid": "QmCred", "bytes": len(b)}, nil
	}, 0)

	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	mintCtx := credctx.With(context.Background(), "portal.jwt.upload")
	url, handle := cu.Prepare(mintCtx, "cred.txt", time.Minute)
	require.NotEmpty(t, url)
	require.NotEmpty(t, handle)

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader("credentialed body"))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// The PUT returns 202 while the upload task executes on its own
	// goroutine, so the executor result races these assertions.
	var v struct {
		body string
		jwt  string
	}
	require.Eventually(t, func() bool {
		loaded, ok := got.Load().(struct {
			body string
			jwt  string
		})
		if !ok {
			return false
		}
		v = loaded
		return true
	}, 5*time.Second, 10*time.Millisecond, "executor must have run and stored its result")

	require.Equal(t, "credentialed body", v.body)
	require.Equal(t, "portal.jwt.upload", v.jwt, "executor must receive the mint-time JWT via credctx")
}

// TestUploadNoCredentialIsEmpty verifies that minting with a bare context (the
// CLI/local path) yields an empty credctx credential on the executor context.
func TestUploadNoCredentialIsEmpty(t *testing.T) {
	var got atomic.Value
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		b, _ := io.ReadAll(reader)
		got.Store(struct {
			body string
			jwt  string
		}{string(b), credctx.From(ctx)})
		return map[string]any{"cid": "QmPlain", "bytes": len(b)}, nil
	}, 0)

	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	url := cu.Mint(context.Background(), "plain.txt", time.Minute)
	require.NotEmpty(t, url)

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader("plain body"))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// The PUT returns 202 while the upload task executes on its own
	// goroutine, so the executor result races these assertions.
	var v struct {
		body string
		jwt  string
	}
	require.Eventually(t, func() bool {
		loaded, ok := got.Load().(struct {
			body string
			jwt  string
		})
		if !ok {
			return false
		}
		v = loaded
		return true
	}, 5*time.Second, 10*time.Millisecond, "executor must have run and stored its result")

	require.Equal(t, "plain body", v.body)
	require.Equal(t, "", v.jwt, "no JWT minted => credctx.From must be empty")
}
