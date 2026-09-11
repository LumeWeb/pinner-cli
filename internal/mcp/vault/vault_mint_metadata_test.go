package vault

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	corevault "go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
)

// TestVaultPutFileMintSealsCanonicalStamp is the parity check for the shared
// mint-metadata assembly (transfer.StampedMCPMetadata, unit-pinned in the
// core/transfer package): the vault_put_file surface's mint branch must seal
// EXACTLY the canonical stamp — src=mcp, the request's lifted host type, the
// ResolveMintProfile-pinned profile, and the caller KV (agent) merged under
// it — into the metadata the presigned PUT delivers to the vault write.
func TestVaultPutFileMintSealsCanonicalStamp(t *testing.T) {
	shared := hostenv.PlatformProfile{HostType: hostenv.HostCodex}.Shared()

	var gotMeta map[string]any
	vu := transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, meta map[string]any) (any, error) {
		gotMeta = meta
		return map[string]any{"status": "staged"}, nil
	}, 1<<20)
	defer vu.Stop(context.Background())

	desc := vaultPutDescriptor(false, false, nil, vu, nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{
		Arguments: map[string]any{
			"source":     map[string]any{"mode": "mint"},
			"vault_path": "vault:/uploads/report.pdf",
			"profile":    "work",
			"agent":      "orchestrator-a",
		},
		Caps: &model.RequestCaps{Profile: &shared},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)

	// Fulfill the presigned PUT exactly like the browser would.
	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux := http.NewServeMux()
		vu.RegisterHandlers(mux)
		mux.ServeHTTP(w, r)
	}))
	defer putSrv.Close()
	u, err := url.Parse(sc["url"].(string))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, putSrv.URL+u.Path, strings.NewReader("put-bytes"))
	require.NoError(t, err)
	resp, err := putSrv.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Equal(t, "mcp", gotMeta[corevault.MetaKeySrc])
	require.Equal(t, string(hostenv.HostCodex), gotMeta[corevault.MetaKeyHost])
	require.Equal(t, "work", gotMeta[corevault.MetaKeyProfile])
	require.Equal(t, "orchestrator-a", gotMeta["agent"])
}

// TestVaultPutFileCurlCommandPinned pins the vault mint branch's curl_command
// runtime shape to the ONE shared builder (mintcontract.CurlUploadCommand),
// so it cannot drift from upload_file's mint branch.
func TestVaultPutFileCurlCommandPinned(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/report.pdf",
	}})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, mintcontract.CurlUploadCommand(sc["url"].(string)), sc["curl_command"])
	require.Equal(t, `curl -sS -T <your-file> "`+sc["url"].(string)+`"`, sc["curl_command"])
}
