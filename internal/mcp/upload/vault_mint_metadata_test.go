package upload

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

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	corevault "go.lumeweb.com/pinner/core/vault"
)

// TestMintSurfacesSealCanonicalStamp is the parity check for the shared
// mint-metadata assembly (transfer.StampedMCPMetadata, unit-pinned in the
// core/transfer package): both app-side vault mint surfaces — the
// open_vault_manager launcher and the vault_upload_submit app helper — must
// seal EXACTLY the canonical stamp (src=mcp, the request's lifted host type,
// the explicit profile pinned through ResolveMintProfile) into the metadata
// the presigned PUT delivers to the vault write. vault_put_file's twin check
// lives in the vault package.
func TestMintSurfacesSealCanonicalStamp(t *testing.T) {
	shared := model.Profile{HostType: "claude-desktop"}

	cases := []struct {
		name    string
		handler func(vu *transfer.VaultHTTPUpload) model.ToolDescriptor
		urlKey  string
	}{
		{
			name:    "open_vault_manager launcher",
			handler: NewOpenVaultManagerDescriptor,
			urlKey:  "presigned_url",
		},
		{
			name:    "vault_upload_submit app helper",
			handler: vaultUploadSubmitDescriptor,
			urlKey:  "url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMeta map[string]any
			vu := transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, meta map[string]any) (any, error) {
				gotMeta = meta
				return map[string]any{"status": "staged"}, nil
			}, 1<<20)
			defer vu.Stop(context.Background())

			desc := tc.handler(vu)
			res, err := desc.Handler(context.Background(), model.ToolRequest{
				Arguments: map[string]any{
					"vault_path": "vault:/uploads/report.pdf",
					"profile":    "work",
				},
				Caps: &model.RequestCaps{Profile: &shared},
			})
			require.NoError(t, err)
			require.False(t, res.IsError)
			sc, ok := res.StructuredContent.(map[string]any)
			require.True(t, ok)

			// Fulfill the presigned PUT exactly like the browser/iframe would.
			putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux := http.NewServeMux()
				vu.RegisterHandlers(mux)
				mux.ServeHTTP(w, r)
			}))
			defer putSrv.Close()
			u, err := url.Parse(sc[tc.urlKey].(string))
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPut, putSrv.URL+u.Path, strings.NewReader("picked-file-bytes"))
			require.NoError(t, err)
			resp, err := putSrv.Client().Do(req)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			require.Equal(t, "mcp", gotMeta[corevault.MetaKeySrc])
			require.Equal(t, "claude-desktop", gotMeta[corevault.MetaKeyHost])
			require.Equal(t, "work", gotMeta[corevault.MetaKeyProfile])
		})
	}
}
