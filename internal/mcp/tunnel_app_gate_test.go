package mcp

// HIGH-severity constraint: the presigned-HTTP upload/vault MCP Apps
// (open_upload_manager + open_vault_manager launchers) must NOT be registered
// on the embedded OpenAI tunnel. That tunnel exposes no reachable HTTP mux —
// all RPC flows through the tunnel protocol — so the minted presigned URLs
// would point at an unreachable loopback and the app's Uppy XHR PUT could
// never land. The headless upload_file / vault_put_file primitives stay
// registered (their relay branches work through the tunnel); only the
// presigned-PUT App surfaces are gated.
//
// The test drives the production collectServerExtensions pipeline against a
// real buildCatalog catalog with the same fully-wired dependency bundle the
// startup server uses, flipping only the tunnelOpenAI axis, and compares the
// catalog's launcher membership.

import (
	"testing"

	"github.com/stretchr/testify/require"
	mctf "go.lumeweb.com/mcpplane/transfer"
)

// collectCatalogForTunnel builds the production catalog (real buildCatalog)
// and runs the production collection phase against a fully-wired dep bundle
// with the given tunnel axis, returning the plan's catalog (which holds the
// final searchable membership).
func collectCatalogForTunnel(t *testing.T, tunnelOpenAI bool) *ToolCatalog {
	t.Helper()
	restoreConstructionGuards(t)

	catalog, err := buildCatalog(nil, nil, nil, nil, nil, nil,
		withCatalogDeps(func() *CatalogDepsBundle { return fullTestBundle() }))
	require.NoError(t, err, "buildCatalog")

	deps := fullLocalDeps(t, nil, catalog)
	deps.tunnelOpenAI = tunnelOpenAI
	// Wire the shared upload relay executor too: on the OpenAI tunnel the
	// headless upload_file has ONLY its relay/data branch, so the primitive
	// (and therefore its catalog index) exists only when this is wired —
	// mirrors the CLI startup server, which always wires it.
	deps.opts.uploadHandler = mctf.UploadHandler(stubUploadExec)
	plan, err := collectServerExtensions(deps)
	require.NoError(t, err, "collectServerExtensions must succeed (tunnelOpenAI=%v)", tunnelOpenAI)
	cat := plan.Catalog()
	require.NotNil(t, cat)
	return cat
}

func TestOpenAITunnelDoesNotRegisterPresignedUploadApps(t *testing.T) {
	httpCat := collectCatalogForTunnel(t, false)
	openaiCat := collectCatalogForTunnel(t, true)

	t.Run("plain HTTP/stdio registers both presigned launchers", func(t *testing.T) {
		for _, name := range []string{"open_upload_manager", "open_vault_manager"} {
			_, ok := httpCat.Get(name)
			require.Truef(t, ok, "%s must be searchable on a transport with a reachable HTTP mux", name)
		}
	})

	t.Run("openai tunnel omits both presigned launchers", func(t *testing.T) {
		for _, name := range []string{"open_upload_manager", "open_vault_manager"} {
			_, ok := openaiCat.Get(name)
			require.Falsef(t, ok, "%s must NOT be registered on the embedded OpenAI tunnel: its presigned PUT route is unreachable there", name)
		}
	})

	t.Run("headless primitives stay registered on the openai tunnel", func(t *testing.T) {
		for _, name := range []string{"upload_file", "vault_put_file"} {
			_, ok := openaiCat.Get(name)
			require.Truef(t, ok, "%s must remain registered on the OpenAI tunnel (relay branch)", name)
		}
	})
}
