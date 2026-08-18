package mcp

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalogops"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// compilerRoot builds a minimal CLI command tree with one walkable command
// (pinner_pins). Because the legacy RegisterFromCommand walk is not run at all
// in the MCP surface, this command is only surfaced when the compiled pins
// domain is present (if at all); it is primarily a fixture for asserting the
// walk is absent.
func compilerRoot() *cli.Command {
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
// compiler surface is actually live in the running server. Without the option
// buildCatalog fails fast (there is no fallback surface), and with it the
// compiler surface is live.
func TestOfficialMCPServerForwardsCatalogDeps(t *testing.T) {
	root := compilerRoot()

	// Without the option, buildCatalog fails fast: there is no legacy walk and
	// no compiler surface, so a caller that forgot WithCatalogOps gets an
	// explicit error instead of a silently-empty model catalog.
	_, _, err := OfficialMCPServer(root, true, nil, false, nil, nil, nil, NewHandoffRegistry(), session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions))
	require.Error(t, err, "missing catalog-deps bundle must fail fast, not silently serve an empty surface")

	// With the option, the compiler surface is live.
	srv2, cat2, err := OfficialMCPServer(root, true, nil, false, nil, nil, nil, NewHandoffRegistry(), session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions),
		withCatalogDeps(func() *CatalogDepsBundle { return &CatalogDepsBundle{Auth: catalogops.AuthDeps{}} }))
	require.NoError(t, err)
	require.NotNil(t, srv2)
	_, ok := cat2.Get("auth_status")
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
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := NewHandoffRegistry()

	srv, cat, err := OfficialMCPServer(compilerRoot(), true, nil, false, nil, nil, oob, reg, handles,
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

// TestCompilerModeProvidesCompiledSurface verifies that when a catalog-deps
// bundle is supplied, buildCatalog is compiler-backed: the compiled catalogops
// surface is present and the legacy CLI-tree walk is not run (so no pinner_*
// tools are produced for any domain).
func TestCompilerModeProvidesCompiledSurface(t *testing.T) {
	tc, err := buildCatalog(compilerRoot(), true, nil, nil, nil, nil, nil, nil,
		withCatalogDeps(func() *CatalogDepsBundle {
			return &CatalogDepsBundle{
				Auth: catalogops.AuthDeps{}, // nil services fine at registration
			}
		}))
	require.NoError(t, err)

	// Compiled op present and discoverable via the compiler-backed surface.
	entry, ok := tc.Get("auth_status")
	require.True(t, ok, "compiled auth.status should be present in compiler mode")
	require.NotNil(t, entry.Handler)

	// The legacy walk is entirely absent: the CLI-tree tool for the `pins`
	// command (would be pinner_pins) must NOT be registered.
	_, ok = tc.Get("pinner_pins")
	require.False(t, ok, "legacy pinner_pins must not be registered (walk is removed)")

	// The compiled op dispatches through the catalog gate without a hard error
	// (missing required args surface as a clean ToolResult error, not a panic).
	res, err := entry.Handler(context.Background(), ToolRequest{Name: "auth_status", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError, "executing auth.status with nil deps should fail cleanly, not panic")
}

// TestNoLegacyWalkFailsFastWithoutDeps verifies the compiler-only contract:
// with no catalog-deps bundle the legacy walk is never run, and buildCatalog
// fails fast with an explicit error rather than silently serving an empty
// model surface (there is no fallback).
func TestNoLegacyWalkFailsFastWithoutDeps(t *testing.T) {
	_, err := buildCatalog(compilerRoot(), true, nil, nil, nil, nil, nil, nil)
	require.Error(t, err, "buildCatalog without a resolving deps bundle must fail fast")
	require.ErrorContains(t, err, "withCatalogDeps", "error should point at the required option")
}

// TestCompilerModeConsistentWhenDepsResolveNil guards the Kody finding that the
// compiler mode must originate from one resolved source. When the factory is
// supplied but resolves to nil there is no model surface, so buildCatalog
// fails fast instead of silently serving an empty catalog.
func TestCompilerModeConsistentWhenDepsResolveNil(t *testing.T) {
	_, err := buildCatalog(compilerRoot(), true, nil, nil, nil, nil, nil, nil,
		withCatalogDeps(func() *CatalogDepsBundle { return nil }))
	require.Error(t, err, "resolved-nil bundle must fail fast, not silently serve an empty surface")
}
