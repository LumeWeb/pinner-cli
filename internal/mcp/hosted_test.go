package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildHostedServerRejectsVaultSync guards the invariant that a hosted
// (Portal-embedded) server never registers the background vault scheduler
// tasks. The Sia vault is surface-disabled in hosted mode and the vault sync/
// upload loops live only in the CLI adapter Action; passing WithVaultSync into
// a hosted assembly must fail loudly rather than silently schedule vault work.
func TestBuildHostedServerRejectsVaultSync(t *testing.T) {
	_, _, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
		Options: []MCPServerOption{
			WithVaultSync(corevault.SyncLoopConfig{
				Service: func(string) (corevault.VaultService, error) { return nil, nil },
			}),
		},
	})
	require.Error(t, err, "hosted assembly with WithVaultSync must be rejected")
	assert.Contains(t, err.Error(), "vault scheduler")
}

// TestBuildHostedServerWithoutVaultSyncAssembles verifies the default hosted
// assembly (no vault sync and no vault surface) builds cleanly.
func TestBuildHostedServerWithoutVaultSyncAssembles(t *testing.T) {
	_, cat, ht, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
	})
	require.NoError(t, err, "hosted assembly must build without vault sync")
	require.NotNil(t, cat)
	// With no transfer executors wired, no IPFS byte-route coordinators exist.
	require.NotNil(t, ht)
	assert.Nil(t, ht.Upload, "no IPFS upload coordinator without a wired task manager")
	assert.Nil(t, ht.Download, "no IPFS download coordinator without a wired download executor")
}

// TestBuildHostedServerWiresIPFSTransfer verifies that wiring IPFS (never
// vault) transfer executors into a hosted assembly produces real IPFS
// byte-route coordinators, so the capabilities report advertises upload_file,
// download_file and host_file_input and the vault tools stay absent.
func TestBuildHostedServerWiresIPFSTransfer(t *testing.T) {
	uploadExec := func(ctx context.Context, r io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		return map[string]any{"cid": "QmTest"}, nil
	}
	tasks := transfer.NewUploadTaskManager(uploadExec, 0)
	downloadExec := func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil }

	_, cat, ht, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
		Options: []MCPServerOption{
			WithUploadTaskManager(tasks),
			WithIPFSDownload(transfer.IPFSDownloadHandler(downloadExec)),
		},
	})
	require.NoError(t, err, "hosted assembly with IPFS transfer executors must build")
	require.NotNil(t, cat)
	require.NotNil(t, ht, "IPFS transfer coordinators must be surfaced for route mounting")
	assert.NotNil(t, ht.Upload, "wired IPFS upload task manager must produce an upload coordinator")
	assert.NotNil(t, ht.Download, "wired IPFS download executor must produce a download coordinator")

	// The single decision used for tool registration is the same one the
	// capability report is derived from: with both executors wired over HTTP
	// transport (coLocated=false, tunnelOpenAI=false), both upload_file and
	// download_file are available.
	uploadOK := uploadFileAvailable(false, false, ht.Upload != nil, true, false)
	vaultOK := vaultPutFileAvailable(false, false, false, false, false)
	report := CurrentCapabilities(false, false, uploadOK, vaultOK, ht.Download != nil, false, ht.Download != nil, false, 0)
	require.True(t, report.UploadFile, "wired IPFS upload must report upload_file=true")
	require.True(t, report.DownloadFile, "wired IPFS download must report download_file=true")
	require.True(t, report.HostFileInput, "wired IPFS upload must report host_file_input=true")
	require.False(t, report.VaultPutFile, "hosted surface must never report vault_put_file")
	require.False(t, report.VaultGetFile, "hosted surface must never report vault_get_file")
}

// TestBuildHostedServerBaseURLConnectOrigins guards the ordering invariant that
// a hosted server applies its externally reachable BaseURL to the IPFS upload
// coordinator BEFORE computing the app resource's CSP connectDomains. If the
// base URL were applied afterward, ConnectOrigins would capture the loopback
// origin and the sandbox CSP would block the cross-origin presigned PUT to the
// real origin.
func TestBuildHostedServerBaseURLConnectOrigins(t *testing.T) {
	tasks := transfer.NewUploadTaskManager(func(context.Context, io.Reader, int64, string, bool, string, bool) (any, error) {
		return map[string]any{"cid": "QmTest"}, nil
	}, 0)

	_, _, ht, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
		BaseURL:     "https://pinner.xyz",
		Options: []MCPServerOption{
			WithUploadTaskManager(tasks),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ht)
	require.NotNil(t, ht.Upload, "wired IPFS upload task manager must produce an upload coordinator")
	origins := ht.Upload.ConnectOrigins()
	require.NotEmpty(t, origins)
	require.Equal(t, "https://pinner.xyz", origins[0], "upload coordinator's CSP connectDomains must reflect the hosted BaseURL, not the loopback origin")
}

// TestBuildHostedServerViewDomainScopedToAssembly guards the process-wide leak
// invariant: the hosted assembly installs the view-domain resolver ONLY for
// its own app registration window. After BuildHostedServer returns the
// resolver is nil again, so a later assembly in the same process (a
// self-hosted CLI server, another embed, a test) registers its ui:// views
// with NO domain instead of this deployment's origin. It also pins that the
// views registered DURING the assembly carry the hosted origin.
func TestBuildHostedServerViewDomainScopedToAssembly(t *testing.T) {
	// Preserve whatever resolver is installed for other tests.
	previous := apps.ViewDomainResolver()
	apps.SetViewDomainResolver(nil)
	t.Cleanup(func() { apps.SetViewDomainResolver(previous) })

	tasks := transfer.NewUploadTaskManager(func(context.Context, io.Reader, int64, string, bool, string, bool) (any, error) {
		return map[string]any{"cid": "QmTest"}, nil
	}, 0)

	srv, _, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
		BaseURL:     "https://hosted.example.com",
		Options: []MCPServerOption{
			WithUploadTaskManager(tasks),
		},
	})
	require.NoError(t, err)

	// Assembly-scoped: the resolver did NOT survive past registration.
	require.Nil(t, apps.ViewDomainResolver(),
		"BuildHostedServer must reset the view-domain resolver so a later assembly never inherits the hosted origin")

	// Views registered during the assembly carry the hosted origin.
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()
	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	var uploadView *mcp.Resource
	for _, r := range res.Resources {
		if r.URI == upload.IPFSUploadAppURI {
			uploadView = r
			break
		}
	}
	require.NotNil(t, uploadView, "hosted upload app view registered")
	ui, ok := uploadView.Meta["ui"].(map[string]any)
	require.True(t, ok, "upload view carries _meta.ui")
	require.Equal(t, "https://hosted.example.com", ui["domain"],
		"views registered during the hosted assembly must attribute to the hosted BaseURL origin")

	// A subsequent plain assembly in the same process emits NO domain.
	appCatalog := NewToolCatalog()
	appCatalog.Add(&model.ToolEntry{
		Name:        auth.OpenAccountToolName,
		Title:       "Open Account",
		Description: "launcher",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler:     func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) { return model.ToolResult{}, nil },
	})
	plain := sdk.NewServer(nil)
	require.NoError(t, auth.RegisterAuthStatusApp(plain, appCatalog))
	cs2 := connectOfficialClient(t, plain)
	res2, err := cs2.ListResources(ctx, nil)
	require.NoError(t, err)
	var accountView *mcp.Resource
	for _, r := range res2.Resources {
		if r.URI == auth.AuthStatusAppURI {
			accountView = r
			break
		}
	}
	require.NotNil(t, accountView, "plain assembly's account view registered")
	stringMap := map[string]any{}
	for k, v := range accountView.Meta {
		if _, isString := v.(string); isString || strings.HasPrefix(k, "openai/widgetDomain") {
			stringMap[k] = v
		}
	}
	require.NotContains(t, stringMap, "openai/widgetDomain",
		"later assembly must not carry the hosted origin as widget domain alias")
	uiLater, ok := accountView.Meta["ui"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, uiLater, "domain",
		"later assembly's views must carry no domain at all (no foreign origin)")
}
