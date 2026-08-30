package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/credctx"
)

// TestVaultUploadCredentialPropagates verifies that a JWT captured at Mint time
// is stamped onto the context handed to the vault write when the minted
// endpoint receives its PUT, so a hosted (Portal-embedded) vault upload
// authenticates as the calling user.
func TestVaultUploadCredentialPropagates(t *testing.T) {
	var got atomic.Value
	vu := NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, _ map[string]any) (any, error) {
		b, _ := io.ReadAll(r)
		got.Store(struct {
			size int64
			jwt  string
		}{size, credctx.From(ctx)})
		_ = b
		return map[string]any{"status": "staged"}, nil
	}, 1<<20)

	mintURL, err := vu.Mint(credctx.With(context.Background(), "portal.jwt.vault"), "vault:/docs/cred.pdf", time.Minute, nil)
	require.NoError(t, err)
	require.NotEmpty(t, mintURL)

	resp := driveVaultPut(t, vu, mintURL, "vault-credentialed-body")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	v := got.Load().(struct {
		size int64
		jwt  string
	})
	require.Equal(t, "portal.jwt.vault", v.jwt, "vault write must receive the mint-time JWT via credctx")
}

// TestVaultUploadNoCredentialIsEmpty verifies that minting with a bare context
// yields an empty credctx credential on the vault write context.
func TestVaultUploadNoCredentialIsEmpty(t *testing.T) {
	var got atomic.Value
	vu := NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, _ map[string]any) (any, error) {
		_, _ = io.ReadAll(r)
		got.Store(credctx.From(ctx))
		return map[string]any{"status": "staged"}, nil
	}, 1<<20)

	mintURL, err := vu.Mint(context.Background(), "vault:/docs/plain.pdf", time.Minute, nil)
	require.NoError(t, err)
	require.NotEmpty(t, mintURL)

	resp := driveVaultPut(t, vu, mintURL, "vault-plain-body")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Equal(t, "", got.Load().(string), "no JWT minted => credctx.From must be empty")
}

// driveVaultPut mounts the vault-upload PUT route on an httptest server and PUTs
// the given body to the minted URL.
func driveVaultPut(t *testing.T, vu *VaultHTTPUpload, mintedURL, body string) *http.Response {
	t.Helper()
	mux := http.NewServeMux()
	vu.RegisterHandlers(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	token := mintedURL[strings.LastIndex(mintedURL, "/")+1:]
	require.NotEmpty(t, token)
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/vault-upload/"+token, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}
