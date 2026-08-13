package mcp

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// compilerRoot builds a minimal CLI command tree with one walkable command
// (pinner_pins) so the legacy RegisterFromCommand would surface it in the
// no-deps fallback path.
func compilerRoot245() *cli.Command {
	return &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{Name: "pins", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
}

// TestOfficialMCPServerForwardsCatalogDeps guards the production wiring that
// Kody flagged as dead code: MCPCommand's Action must thread a WithCatalogOps
// bundle into buildCatalog (via OfficialMCPServer with withCatalogDeps) so the
// compiler surface is actually live in the running server. It exercises the
// public official server entry point with and without the option.
func TestOfficialMCPServerForwardsCatalogDeps(t *testing.T) {
	root := compilerRoot245()

	// Without the option, the compiled surface is absent and the legacy walk
	// runs (baseline: the option is what turns the compiler surface on).
	srv, cat, err := OfficialMCPServer(root, true, nil, false, nil, nil, nil, NewHandoffRegistry(), NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions))
	require.NoError(t, err)
	require.NotNil(t, srv)
	_, ok := cat.Get("auth.status")
	require.False(t, ok, "compiled op must be absent without withCatalogDeps")
	_, ok = cat.Get("pinner_pins")
	require.True(t, ok, "legacy walk must run without withCatalogDeps")

	// With the option, the compiled surface is live on top of the legacy walk.
	srv2, cat2, err := OfficialMCPServer(root, true, nil, false, nil, nil, nil, NewHandoffRegistry(), NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions),
		withCatalogDeps(func() *CatalogDepsBundle { return &CatalogDepsBundle{Auth: catalogops.AuthDeps{}} }))
	require.NoError(t, err)
	require.NotNil(t, srv2)
	_, ok = cat2.Get("auth.status")
	require.True(t, ok, "compiled op must be present when withCatalogDeps is supplied to OfficialMCPServer")
}

// TestCompiledVaultCreateHonorsOOBHandoff verifies that the compiled
// vault.create tool, as wired by buildCatalog, routes through the out-of-band
// setup handler so a model receives the full create_url + resume-handle +
// needs_human hand-off its AgentDescription promises, rather than a bare
// JSON-serialized VaultCreateHandoff{Profile} plaintext. This guards the Kody
// critical finding on the compiled vault-setup surface.
func TestCompiledVaultCreateHonorsOOBHandoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	oob, _, _ := buildCreateServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()

	srv, cat, err := OfficialMCPServer(compilerRoot245(), true, nil, false, nil, nil, oob, reg, handles,
		withCatalogDeps(func() *CatalogDepsBundle { return &CatalogDepsBundle{Auth: catalogops.AuthDeps{}} }))
	require.NoError(t, err)
	require.NotNil(t, srv)

	// The compiled vault.create entry must be present and routed through the
	// OOB setup handler (its handler is not the generic compiledHandler).
	createEntry, ok := cat.Get(compiledVaultCreateToolName)
	require.True(t, ok, "compiled vault.create must be present in compiler mode")
	require.NotNil(t, createEntry.Handler)
	require.Equal(t, InteractionAgentSafe, createEntry.Interaction)

	res, err := createEntry.Handler(context.Background(), ToolRequest{
		Name: compiledVaultCreateToolName,
		Arguments: map[string]any{
			"profile": "aliasdev",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "compiled vault.create must produce a hand-off, not an error: %s", res.Text)

	// The hand-off must mint a one-time create_url and carry a resume handle
	// (the OOB contract), not a bare {"profile":"..."} serialization.
	sc, okc := res.StructuredContent.(map[string]any)
	require.True(t, okc, "compiled vault.create hand-off must carry structured content")
	createURL, _ := sc["create_url"].(string)
	require.Contains(t, createURL, "/create/", "compiled vault.create must mint a one-time create_url")
	require.NotEmpty(t, sc["handle"], "compiled vault.create hand-off must carry a resume handle")
	require.NotEmpty(t, sc["resume_tool"], "compiled vault.create hand-off must name its resume tool")
}
