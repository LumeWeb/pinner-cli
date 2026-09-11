package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// installedAppsCatalog builds a small ToolCatalog whose open_*/app entries
// stand in for the app views a completed assembly installed, so the open_app
// description derivation can be checked against a KNOWN inventory instead of
// the historical hard-coded one.
//
// The derivation's fallback path consults the process-global app registry
// (apps.AppInfoForTool) for every open_* catalog entry, so each seeded app is
// ALSO registered there (and removed again on cleanup, matching the cleanup
// pattern of TestResolveOpenAppRejectsStaleGlobalLauncher): an isolated
// -run invocation of these tests must behave exactly like the full suite —
// neither reading a stale registry entry a previous test leaked, nor leaking
// this test's entries to the next one.
func installedAppsCatalog(t testing.TB, names ...string) *ToolCatalog {
	t.Helper()
	catalog := NewToolCatalog()
	for _, n := range names {
		catalog.Add(&model.ToolEntry{Name: n})
		screen := strings.TrimPrefix(n, "open_")
		apps.SetAppViewInfo(n, apps.AppViewInfo{
			URI:   "ui://" + screen + ".html",
			Name:  screen,
			Title: screen,
		})
		t.Cleanup(func() { apps.DeleteAppViewInfo(n) })
	}
	return catalog
}

func TestOpenAppDescriptionProfileAware(t *testing.T) {
	// The description derives from the catalog's installed app views: a
	// restricted assembly that installed only the vault browser can only name
	// vault_browser, regardless of what the server's handlers could support.
	full := openAppAppList(installedAppsCatalog(t, "open_vault_browser", "open_sso_signin", "open_pin_creator"))
	restricted := openAppAppList(installedAppsCatalog(t, "open_vault_browser"))
	empty := openAppAppList(NewToolCatalog())
	require.NotEqual(t, full, restricted, "the description must derive from the installed app views")
	require.Equal(t, "none", empty, "a no-app assembly must get a truthful empty inventory")

	guiProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioMCPApps,
		hostenv.ProfileClaudeHTTP,
		hostenv.ProfileOpenAIHTTP,
		hostenv.ProfileOpenAITunnel,
	}
	for _, p := range guiProfiles {
		desc := openAppDescriptionFor(p, installedAppsCatalog(t, "open_vault_browser", "open_sso_signin", "open_pin_creator", "open_upload_manager", "open_pin_list", "open_account", "open_vault_create", "open_vault_restore", "open_account_password", "open_account_email"))
		require.Contains(t, desc, "renders the returned ui:// view as an iframe",
			"%s: GUI description must mention iframe rendering", p.HostType)
		require.Contains(t, desc, "vault_browser",
			"%s: GUI description must list available app names", p.HostType)
		require.NotContains(t, desc, "does not render MCP Apps",
			"%s: GUI description must not say it does not render", p.HostType)
	}

	agentProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioGeneric,
		hostenv.ProfileHTTPGeneric,
		hostenv.ProfileGrokHTTP,
		hostenv.ProfileGrokStdio,
	}
	for _, p := range agentProfiles {
		desc := openAppDescriptionFor(p, installedAppsCatalog(t, "open_vault_browser", "open_sso_signin"))
		require.Contains(t, desc, "does not render MCP Apps",
			"%s: agent description must state no rendering", p.HostType)
		require.Contains(t, desc, "vault_browser",
			"%s: agent description must list available app names", p.HostType)
		require.NotContains(t, desc, "renders the returned ui:// view as an iframe",
			"%s: agent description must not mention iframe rendering", p.HostType)
	}
}

func TestOpenAppTargetsCarryDescFunc(t *testing.T) {
	catalog := installedAppsCatalog(t, "open_vault_browser", "open_sso_signin")
	targets := openAppTargets(catalog)
	require.Len(t, targets, 1)
	require.True(t, targets[0].Visible)
	require.NotNil(t, targets[0].DescFunc, "open_app target must carry a DescFunc for profile-aware resolution")

	guiDesc := targets[0].DescFunc(hostenv.ProfileStdioMCPApps.Shared())
	require.Contains(t, guiDesc, "iframe")
	require.Contains(t, guiDesc, "sso_signin", "DescFunc resolution must keep deriving the installed app list")
	agentDesc := targets[0].DescFunc(hostenv.ProfileStdioGeneric.Shared())
	require.Contains(t, agentDesc, "does not render MCP Apps")
}

// TestResolveOpenAppRejectsStaleGlobalLauncher pins the server-local
// resolution contract: open_app resolves app names ONLY against the apps this
// server actually installed. A process-global app-registry entry for a
// launcher THIS catalog never declared (left over from a previous, fuller
// assembly in the same process) must never resolve — neither by full launcher
// name nor by bare screen name.
func TestResolveOpenAppRejectsStaleGlobalLauncher(t *testing.T) {
	// Pollute the process-global app registry exactly like a previous full
	// assembly would: the vault browser launcher is registered globally but
	// the catalog under test never declared it.
	apps.SetAppViewInfo("open_vault_browser", apps.AppViewInfo{URI: "ui://vault/browser.html", Name: "vault-browser", Title: "Vault Browser"})
	apps.SetAppViewInfo("open_pin_list", apps.AppViewInfo{URI: "ui://pins/list.html", Name: "pin-list", Title: "Pin List"})
	t.Cleanup(func() {
		apps.DeleteAppViewInfo("open_vault_browser")
		apps.DeleteAppViewInfo("open_pin_list")
	})

	// A bare catalog entry WITHOUT the seeded global-registry entry: this
	// test manages the registry itself (below), so it does not use the
	// conflict-free installedAppsCatalog helper.
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{Name: "open_pin_list"})

	_, _, ok := resolveOpenApp(catalog, "open_vault_browser")
	require.False(t, ok, "a full launcher name absent from this server's catalog must never resolve via the global registry")

	_, _, ok = resolveOpenApp(catalog, "vault_browser")
	require.False(t, ok, "a bare screen name absent from this server's catalog must never resolve via the global registry")

	// Control: an installed launcher still resolves by either form.
	name, uri, ok := resolveOpenApp(catalog, "open_pin_list")
	require.True(t, ok)
	require.Equal(t, "open_pin_list", name)
	require.Equal(t, "ui://pins/list.html", uri)
}
