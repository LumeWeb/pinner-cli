package services

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// requireServiceBackend skips the test when the running platform has no service
// backend compiled yet. The systemd (linux), launchd (darwin), and SCM
// (windows) backends each register into the service registry; until a platform's
// backend lands, tests that construct a live backend through newManagedService
// (which calls service.New) cannot run there. This lets the backend-construction
// tests run on every platform as its backend arrives without a per-OS skip.
func requireServiceBackend(t *testing.T) {
	t.Helper()
	var zero service.Config
	if _, err := service.New(zero); err != nil {
		t.Skipf("no service backend for this platform: %v", err)
	}
}

func TestManagedServiceCommandHasLifecycleCommands(t *testing.T) {
	cmd := ManagedServiceCommand()
	require.Equal(t, "service", cmd.Name)
	for _, name := range []string{"validate", "install", "uninstall", "start", "stop", "restart", "status", "logs"} {
		found := false
		for _, child := range cmd.Commands {
			if child.Name == name {
				found = true
				break
			}
		}
		require.True(t, found, name)
	}
}

func TestExpandServicePath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".config", "pinner", "mcp.env"), expandServicePath("~/.config/pinner/mcp.env"))
}

func TestServicePort(t *testing.T) {
	require.Equal(t, 4321, servicePort(ServiceEnvironment{"MCP_PORT": "4321"}))
	require.Equal(t, 0, servicePort(ServiceEnvironment{"MCP_PORT": "invalid"}))
}

func TestServiceProviderRequirements(t *testing.T) {
	require.True(t, tunnel.OpenAITunnelID.MatchString("tunnel_0123456789abcdef0123456789abcdef"))
	require.False(t, tunnel.OpenAITunnelID.MatchString("tunnel_invalid"))
}

func TestResolveManagedServiceRejectsInsecureEnvironmentFile(t *testing.T) {
	// Group/world-readable enforcement is Unix-only; Windows reports 0666 for
	// every file regardless of chmod, so the check is skipped there.
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=openai\n"), 0644))
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	require.NoError(t, cmd.Set("env-file", path))
	_, err := resolveManagedService(context.Background(), cmd, true, false)
	require.ErrorContains(t, err, "group/world-readable")
}

func TestServiceEnvironmentPrecedence(t *testing.T) {
	old := getenv
	getenv = func(key string) string {
		if key == "MCP_TUNNEL_PROVIDER" {
			return "cloudflared"
		}
		return ""
	}
	defer func() { getenv = old }()

	require.Equal(t, "openai", serviceEnvValue(ServiceEnvironment{"MCP_TUNNEL_PROVIDER": "openai"}, "MCP_TUNNEL_PROVIDER", getenv("MCP_TUNNEL_PROVIDER")))
}

func TestInstallBootstrapsMissingEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "cloudflared"))
	require.NoError(t, cmd.Set(serviceDomainFlag, "mcp.example.com"))
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token-abc123"))
	require.NoError(t, cmd.Set(serviceTunnelNameFlag, "pinner-mcp"))
	require.NoError(t, cmd.Set(servicePublicURLFlag, "https://mcp.example.com"))
	require.NoError(t, cmd.Set(servicePortFlag, "4321"))

	// Bootstrap writes the file without requiring the tunnel binary on PATH.
	require.NoError(t, bootstrapServiceEnvironment(cmd, path, nil))

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Windows has no Unix permission bits: os.Chmod(0600) only toggles the
	// read-only attribute and Stat reports 0666 there. Skip the strict mode
	// assertion on Windows; the 0600 invariant is enforced on Unix platforms.
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "env file must be 0600")
	}

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "mcp.example.com", env["MCP_DOMAIN"])
	require.Equal(t, "test-auth-token-abc123", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "pinner-mcp", env["MCP_TUNNEL_NAME"])
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"])
	require.Equal(t, "4321", env["MCP_PORT"])
	// OAuth is the secure default for a public remote endpoint: a headless
	// --tunnel bootstrap must write MCP_OAUTH=true even when --oauth is unset.
	require.Equal(t, "true", env["MCP_OAUTH"], "headless bootstrap must default OAuth on")

	// Non-OpenAI tunnel providers expose the server over HTTP.
	cfg, err := serviceConfigForInstall(cmd, path, "cloudflared")
	require.NoError(t, err)
	require.Contains(t, cfg.Arguments, "--http")
}

func TestInstallBootstrapHonorsExplicitOAuthFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "cloudflared"))
	require.NoError(t, cmd.Set(serviceDomainFlag, "mcp.example.com"))
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token-abc123"))
	require.NoError(t, cmd.Set(servicePublicURLFlag, "https://mcp.example.com"))
	require.NoError(t, cmd.Set(serviceOAuthFlag, "false"))

	require.NoError(t, bootstrapServiceEnvironment(cmd, path, nil))

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	// An explicit --oauth=false must be persisted as MCP_OAUTH=false (not the
	// secure default true, and not silently dropped).
	require.Equal(t, "false", env["MCP_OAUTH"], "explicit --oauth=false must persist MCP_OAUTH=false on headless bootstrap")
}

// TestReconcileExplicitOAuthFlagOverExistingFile guards the mcp install skip
// path: re-running against an existing COMPLETE env file (which has a public
// URL and therefore never re-runs the interactive config) must still let an
// explicit --oauth flag override what a prior run persisted. Without this, the
// collector reads the existing file unchanged and an explicit --oauth=false is
// silently dropped, leaving the earlier MCP_OAUTH=true intact.
func TestReconcileExplicitOAuthFlagOverExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	// Existing complete file from a prior run: OAuth ON, full URL so the skip
	// path is taken.
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://mcp.example.com",
		"MCP_OAUTH":           "true",
	}))

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceOAuthFlag, "false"))

	require.NoError(t, ReconcileServiceEnvironmentFromFlags(cmd, path))

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "false", env["MCP_OAUTH"], "explicit --oauth=false on a re-run must override the persisted MCP_OAUTH=true")
	// Keys the operator did not touch must be preserved.
	require.Equal(t, "saved-token", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"])
}

// TestReconcilePreservesPersistedOAuthWhenFlagUnset guards that a re-run with
// NO --oauth flag leaves whatever MCP_OAUTH the file already has untouched — the
// secure-default is only applied when creating a fresh file, never as a clobber
// over saved config.
func TestReconcilePreservesPersistedOAuthWhenFlagUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_PUBLIC_URL":      "https://mcp.example.com",
		"MCP_OAUTH":           "false",
	}))
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	// No explicit --oauth: nothing to overlay, file must be byte-identical.
	require.NoError(t, ReconcileServiceEnvironmentFromFlags(cmd, path))
	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "false", env["MCP_OAUTH"], "re-run without --oauth must preserve the persisted MCP_OAUTH=false")
}

// TestReconcileDoesNotClobberWithExplicitEmpty guards that an explicitly-passed
// but EMPTY flag (e.g. --public-url "" or --auth-token "") does NOT overwrite a
// saved non-empty value in an existing env file. Writing an empty value would
// take a working install to a broken/unconfigurable state.
func TestReconcileDoesNotClobberWithExplicitEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://mcp.example.com",
	}))

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(servicePublicURLFlag, ""))    // explicitly empty
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "   ")) // whitespace-only

	require.NoError(t, ReconcileServiceEnvironmentFromFlags(cmd, path))
	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"], "explicit empty --public-url must not clobber the saved URL")
	require.Equal(t, "saved-token", env["MCP_AUTH_TOKEN"], "whitespace-only --auth-token must not clobber the saved token")
}

// TestReconcileFromInstallStateOverlaysPromptedTunnelValues guards the
// interactive re-run reconfiguration path: after the config step prompts,
// ReconcileServiceEnvironmentFromInstallState overlays the operator's
// kept-or-changed tunnel credentials onto the existing file while preserving
// every other key (MCP_OAUTH, MCP_PORT, unmodeled/secret keys).
func TestReconcileFromInstallStateOverlaysPromptedTunnelValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://old.example.com",
		"MCP_OAUTH":           "true",
		"MCP_CUSTOM":          "keep-me",
	}))

	// Operator re-ran interactively with the SAME provider (no switch): kept the
	// auth token, changed the domain and public URL, left OAuth custom unset.
	err := ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:  tunnel.TunnelProviderCloudflared,
		AuthToken: "saved-token",
		Domain:    "new.example.com",
		PublicURL: "https://new.example.com",
	}, tunnel.TunnelProviderCloudflared, false)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"], "provider must be preserved")
	require.Equal(t, "new.example.com", env["MCP_DOMAIN"], "changed domain must be persisted")
	require.Equal(t, "https://new.example.com", env["MCP_PUBLIC_URL"], "changed public URL must be persisted")
	require.Equal(t, "saved-token", env["MCP_AUTH_TOKEN"], "kept auth token must be preserved")
	require.Equal(t, "true", env["MCP_OAUTH"], "unmodeled/untouched key must be preserved")
	require.Equal(t, "keep-me", env["MCP_CUSTOM"], "custom unmodeled key must be preserved")
}

// TestReconcileFromInstallStateEmptyDoesNotClobber guards that a service state
// with no decided tunnel values is a no-op (file untouched).
func TestReconcileFromInstallStateEmptyDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_PUBLIC_URL":      "https://mcp.example.com",
	}))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	err = ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{}, "", false)
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "an empty service state must not rewrite the file")
}

// TestReconcileFromInstallStatePurgesStaleURLOnSwitch guards that switching the
// tunnel provider clears the PREVIOUS provider's resolved MCP_PUBLIC_URL and
// orphaned credential keys, so the collector re-derives the correct endpoint
// for the new provider instead of advertising the old provider's dead URL.
func TestReconcileFromInstallStatePurgesStaleURLOnSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	// Existing ngrok install with its resolved URL.
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "ngrok",
		"MCP_TUNNEL_TOKEN":    "old-tok",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://dead.ngrok-free.dev",
		"MCP_OAUTH":           "true",
	}))

	// Operator re-ran interactively and switched to cloudflared.
	err := ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:  tunnel.TunnelProviderCloudflared,
		Domain:    "mcp.new-example.com",
		AuthToken: "saved-token",
		PublicURL: "https://dead.ngrok-free.dev", // stale fold that must NOT survive
	}, tunnel.TunnelProviderNgrok, false)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	_, hasOldURL := env["MCP_PUBLIC_URL"]
	require.False(t, hasOldURL, "the previous provider's resolved MCP_PUBLIC_URL must be purged on a switch so the new provider derives its own")
	_, hasToken := env["MCP_TUNNEL_TOKEN"]
	require.False(t, hasToken, "ngrok's MCP_TUNNEL_TOKEN must be purged on a switch")
	require.Equal(t, "mcp.new-example.com", env["MCP_DOMAIN"])
	require.Equal(t, "saved-token", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "true", env["MCP_OAUTH"], "unmodeled key must be preserved")
}

// TestReconcileFromInstallStatePurgesOldProviderViaPassedPrev guards the
// --tunnel switch scenario from runMcpInstall: the flags reconcile already
// overwrote the file's MCP_TUNNEL_PROVIDER with the new provider before the
// install-state reconcile runs, so the purge must target the provider captured
// BEFORE that reconcile (passed in) rather than re-reading the file (which now
// says the NEW provider and would skip the purge).
func TestReconcileFromInstallStatePurgesOldProviderViaPassedPrev(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_DOMAIN":          "old.example.com",
		"MCP_TUNNEL_NAME":     "pin-mcp",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://old.example.com",
	}))

	// Simulate runMcpInstall ordering: the flags reconcile wrote the new
	// provider into the file first (--tunnel ngrok), so re-reading the file
	// gives the NEW provider.
	fc := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, fc.Set(serviceTunnelFlag, "ngrok"))
	require.NoError(t, ReconcileServiceEnvironmentFromFlags(fc, path))
	onDisk, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "ngrok", onDisk["MCP_TUNNEL_PROVIDER"], "precondition: flags reconcile set the provider to ngrok")

	// The install-state reconcile receives the provider captured BEFORE the
	// flags reconcile (cloudflared) — it must still purge cloudflared's keys
	// even though the file now claims ngrok.
	err = ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		AuthToken:   "saved-token",
		TunnelToken: "new-tok",
		PublicURL:   "https://new.ngrok-free.dev",
	}, tunnel.TunnelProviderCloudflared, false)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "ngrok", env["MCP_TUNNEL_PROVIDER"])
	_, hasDomain := env["MCP_DOMAIN"]
	require.False(t, hasDomain, "cloudflared's MCP_DOMAIN must be purged even after the flags reconcile set ngrok")
	_, hasName := env["MCP_TUNNEL_NAME"]
	require.False(t, hasName, "cloudflared's MCP_TUNNEL_NAME must be purged even after the flags reconcile set ngrok")
	require.Equal(t, "new-tok", env["MCP_TUNNEL_TOKEN"])
	require.Equal(t, "saved-token", env["MCP_AUTH_TOKEN"])
}

// TestReconcileFromInstallStatePreservesExplicitPublicURLOnSwitch guards that
// an explicit --public-url (explicitPublicURL=true), which is provider-agnostic,
// survives a provider switch — it must NOT be purged like a derived URL.
func TestReconcileFromInstallStatePreservesExplicitPublicURLOnSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "ngrok",
		"MCP_TUNNEL_TOKEN":    "old-tok",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://operator.example.com",
	}))

	// Switch to cloudflared while keeping an explicit --public-url.
	err := ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:  tunnel.TunnelProviderCloudflared,
		Domain:    "mcp.new-example.com",
		AuthToken: "saved-token",
		PublicURL: "https://operator.example.com",
	}, tunnel.TunnelProviderNgrok, true)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "https://operator.example.com", env["MCP_PUBLIC_URL"], "an explicit --public-url must survive a provider switch")
	_, hasToken := env["MCP_TUNNEL_TOKEN"]
	require.False(t, hasToken, "ngrok's MCP_TUNNEL_TOKEN must still be purged on a switch")
}

// TestReconcileFromInstallStatePurgesAlternateTokenAlias guards that a
// SAME-provider (ngrok) re-run clears the stale alternate-name key
// (NGROK_AUTHTOKEN) when the new config carries MCP_TUNNEL_TOKEN, so the old
// value cannot win via ResolveNgrokToken's NGROK_AUTHTOKEN preference — while
// still preserving the DISTINCT live NGROK_API_KEY credential, which is not an
// alternate spelling and is read at collect time for URL resolution.
func TestReconcileFromInstallStatePurgesAlternateTokenAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "ngrok",
		"NGROK_AUTHTOKEN":     "old-alias-token",
		"NGROK_API_KEY":       "live-api-key",
		"MCP_AUTH_TOKEN":      "saved-token",
		"MCP_PUBLIC_URL":      "https://ngrok-free.dev",
	}))

	// Same-provider ngrok re-run: the new config carries MCP_TUNNEL_TOKEN.
	err := ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "new-tok",
		AuthToken:   "saved-token",
		PublicURL:   "https://ngrok-free.dev",
	}, tunnel.TunnelProviderNgrok, false)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "new-tok", env["MCP_TUNNEL_TOKEN"])
	_, hasAlias := env["NGROK_AUTHTOKEN"]
	require.False(t, hasAlias, "the stale NGROK_AUTHTOKEN alias must be cleared when the new config carries MCP_TUNNEL_TOKEN")
	require.Equal(t, "live-api-key", env["NGROK_API_KEY"], "the distinct NGROK_API_KEY credential must survive a same-provider re-run")
}

// TestReconcileFromInstallStatePreservesLegacyOnlyToken guards that a SAME-
// provider ngrok reconcile never clears the only token: when the env file's
// only ngrok credential is the legacy NGROK_AUTHTOKEN spelling and the install
// state carries no TunnelToken (so the overlay has no MCP_TUNNEL_TOKEN to
// replace it with), the alias must be left intact.
func TestReconcileFromInstallStatePreservesLegacyOnlyToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, service.WriteEnvironment(path, service.Environment{
		"MCP_TUNNEL_PROVIDER": "ngrok",
		"NGROK_AUTHTOKEN":     "legacy-token",
		"MCP_AUTH_TOKEN":      "saved-token",
	}))

	// Same-provider ngrok reconcile with an EMPTY TunnelToken in state: the
	// overlay carries no MCP_TUNNEL_TOKEN, so NGROK_AUTHTOKEN must survive.
	err := ReconcileServiceEnvironmentFromInstallState(path, &ServiceInstallState{
		Provider:  tunnel.TunnelProviderNgrok,
		AuthToken: "saved-token",
	}, tunnel.TunnelProviderNgrok, false)
	require.NoError(t, err)

	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "legacy-token", env["NGROK_AUTHTOKEN"], "a legacy-only NGROK_AUTHTOKEN must not be cleared without a canonical MCP_TUNNEL_TOKEN replacement")
	_, hasCanonical := env["MCP_TUNNEL_TOKEN"]
	require.False(t, hasCanonical)
}

func TestInstallBootstrapRequiresProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	err := bootstrapServiceEnvironment(cmd, path, nil)
	require.ErrorContains(t, err, "--tunnel")
	require.NoFileExists(t, path, "no env file should be written without a provider")
}

func TestInstallRejectsExistingInsecureEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=openai\n"), 0644))
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	// The group/world-readable rejection is a Unix-only invariant; Windows does
	// not enforce Unix permission bits (WriteServiceEnvironment protects the file
	// via the read-only attribute + the 0600 mode it requests).
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.ErrorContains(t, err, "group/world-readable")
}

func TestValidateOpenAIRequiresKeyInFileNotJustEnv(t *testing.T) {
	// Kody regression: validation must NOT accept a key that exists only in the
	// caller's process environment. The installed systemd unit reads ONLY the
	// env file, so a key present solely via os.Getenv would let install report
	// success while the running service gets an empty value. The key must be
	// persisted in the file itself.
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	// The missing-key path calls openTunnelDeepLink to open the OpenAI API keys
	// page. Stub the opener so test execution never spawns a real browser,
	// keeping CI hermetic.
	origOpener := tunnel.TunnelDeepLinkOpener
	defer func() { tunnel.TunnelDeepLinkOpener = origOpener }()
	tunnel.TunnelDeepLinkOpener = func(string) error { return nil }

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=openai\n"+
			"MCP_TUNNEL_ID=tunnel_0123456789abcdef0123456789abcdef\n"), 0600))

	// The key is present in the environment but absent from the file.
	t.Setenv("CONTROL_PLANE_API_KEY", "env-only-key")

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.ErrorContains(t, err, "CONTROL_PLANE_API_KEY", "env-only key must not satisfy file validation")
}

func TestValidateHeadlessDoesNotSpawnBrowser(t *testing.T) {
	// Kody regression: validateServiceEnvironment reached by the headless
	// `pinner mcp service validate` command must NOT open a browser for missing
	// credentials. The nonInteractive flag must keep it print-only; the
	// interactive path (nonInteractive=false) may open one.
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	t.Setenv("CONTROL_PLANE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	// Force the interactive global off so the interactive-path assertion below
	// is deterministic regardless of any other test's global mutation.
	oldNonInteractive := fieldform.NonInteractive
	defer func() { fieldform.NonInteractive = oldNonInteractive }()
	fieldform.NonInteractive = false

	opened := false
	origOpener := tunnel.TunnelDeepLinkOpener
	defer func() { tunnel.TunnelDeepLinkOpener = origOpener }()
	tunnel.TunnelDeepLinkOpener = func(string) error { opened = true; return nil }

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=openai\n"+
			"MCP_TUNNEL_ID=tunnel_0123456789abcdef0123456789abcdef\n"), 0600))

	// Headless validation: missing API key, but the browser must stay closed.
	_, err := validateServiceEnvironment(path, true)
	require.Error(t, err)
	require.False(t, opened, "headless validate must not open a browser")

	// Interactive validation of the same file MAY open the browser.
	opened = false
	_, err = validateServiceEnvironment(path, false)
	require.Error(t, err)
	require.True(t, opened, "interactive validate opens the deep link")
}

func TestValidateNgrokDoesNotRequireBinaryOnPath(t *testing.T) {
	// ngrok is embedded via the Go SDK, so `pinner mcp service validate` must
	// succeed for ngrok even when no `ngrok` binary is installed. The binary
	// check is now reserved for cloudflared, which still runs as a subprocess.
	// Assert via validateServiceEnvironment (the pure validator) so this runs on
	// every platform regardless of which service backend is compiled.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=ngrok\n"+
			"MCP_AUTH_TOKEN=test-auth-token-abc123\n"+
			"MCP_TUNNEL_TOKEN=test-ngrok-token-xyz789\n"), 0600))
	if runtime.GOOS != "windows" {
		t.Setenv("PATH", t.TempDir()) // no ngrok binary reachable
	}

	_, err := validateServiceEnvironment(path, false)
	require.NoError(t, err)
}

func TestValidateCloudflaredRequiresBinaryOnPath(t *testing.T) {
	// cloudflared remains a subprocess provider, so validation must still fail
	// when the binary is unreachable.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=cloudflared\n"+
			"MCP_AUTH_TOKEN=test-auth-token-abc123\n"+
			"MCP_DOMAIN=mcp.example.com\n"), 0600))
	if runtime.GOOS != "windows" {
		t.Setenv("PATH", t.TempDir()) // no cloudflared binary reachable
	}

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	_, err := resolveManagedService(context.Background(), cmd, true, false)
	require.ErrorContains(t, err, "executable not found on PATH")
}

func TestParseTunnelProviderEnum(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want tunnel.TunnelProvider
		err  bool
	}{
		{raw: "openai", want: tunnel.TunnelProviderOpenAI},
		{raw: "OPENAI", want: tunnel.TunnelProviderOpenAI},
		{raw: " openai ", want: tunnel.TunnelProviderOpenAI},
		{raw: "ngrok", want: tunnel.TunnelProviderNgrok},
		{raw: "cloudflared", want: tunnel.TunnelProviderCloudflared},
		{raw: "clouflare", err: true},
		{raw: "", err: true},
	} {
		got, err := parseTunnelProvider(tc.raw)
		if tc.err {
			require.Error(t, err, tc.raw)
		} else {
			require.NoError(t, err, tc.raw)
			require.Equal(t, tc.want, got)
		}
	}
}

func TestServiceInstallStateToEnv(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider:   tunnel.TunnelProviderCloudflared,
		Domain:     "mcp.example.com",
		TunnelName: "pinner-mcp",
		AuthToken:  "test-auth-token-abc123",
		PublicURL:  "https://mcp.example.com",
		Host:       "127.0.0.1",
		OAuth:      new(true),
		Port:       new(4321),
	})
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "mcp.example.com", env["MCP_DOMAIN"])
	require.Equal(t, "pinner-mcp", env["MCP_TUNNEL_NAME"])
	require.Equal(t, "test-auth-token-abc123", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"])
	require.Equal(t, "127.0.0.1", env["MCP_HOST"])
	require.Equal(t, "true", env["MCP_OAUTH"])
	require.Equal(t, "4321", env["MCP_PORT"])
}

func TestServiceInstallStateToEnvWritesNgrokToken(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelName:  "pinner-mcp",
		AuthToken:   "test-auth-token-abc123",
		TunnelToken: "test-ngrok-token-xyz789",
	})
	require.Equal(t, "ngrok", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "test-ngrok-token-xyz789", env["MCP_TUNNEL_TOKEN"], "ngrok credential must be written as MCP_TUNNEL_TOKEN")
}

// TestServiceInstallStateToEnvWritesOAuthFalse guards the skip/fresh symmetry:
// an EXPLICIT --oauth=false must be persisted as MCP_OAUTH=false, not dropped —
// the other two writer paths (bootstrap and reconcile) both persist the false
// value explicitly, so this one must too.
func TestServiceInstallStateToEnvWritesOAuthFalse(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider: tunnel.TunnelProviderCloudflared,
		OAuth:    new(false),
	})
	require.Equal(t, "false", env["MCP_OAUTH"], "explicit --oauth=false must be persisted as MCP_OAUTH=false")
}

// TestServiceInstallStateToEnvOmitsOAuthWhenUndecided guards the standalone
// wizard path: when OAuth was never decided (nil tri-state — no explicit
// --oauth, no secure default-on for a remote install), serviceInstallStateToEnv
// must OMIT the MCP_OAUTH key entirely so the runtime secure default (on)
// applies. Writing MCP_OAUTH=false here would diverge from the mcp install
// path's default-on doctrine.
func TestServiceInstallStateToEnvOmitsOAuthWhenUndecided(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider: tunnel.TunnelProviderCloudflared,
	})
	require.NotContains(t, env, "MCP_OAUTH", "undecided OAuth must omit the key, not force MCP_OAUTH=false")
}

// TestServiceInstallStateToEnvWritesPort guards that an explicit --port 0
// ("pick a free port") is persisted as MCP_PORT=0 on the fresh re-config path.
// A non-nil tri-state Port carries the explicit decision even when it is zero;
// were it treated as "not set", serviceInstallStateToEnv would drop the key and
// re-apply the saved port on a later run.
func TestServiceInstallStateToEnvWritesPort(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider: tunnel.TunnelProviderCloudflared,
		Port:     new(0),
	})
	require.Equal(t, "0", env["MCP_PORT"], "explicit --port 0 must be persisted as MCP_PORT=0, not dropped")
	// A nil port is the "no decision" case and writes nothing.
	env2 := serviceInstallStateToEnv(&ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared})
	require.NotContains(t, env2, "MCP_PORT", "undecided port must not write MCP_PORT")
}

func TestServiceInstallWizardNonInteractiveErrors(t *testing.T) {
	// In non-interactive mode (e.g. --agent), the wizard must not block on stdin.
	old := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.Error(t, err)
	require.NoFileExists(t, path)
}

func TestSeedFromFlagsAndEnvSourcesFlagsAndEnv(t *testing.T) {
	// The framework resolves each flag's declared env Sources (needs a real run,
	// not a bare cmd.Set), so exercise the full command to confirm env values are
	// honored and handed to the wizard seeding path.
	t.Setenv("MCP_AUTH_TOKEN", "env-secret")
	t.Setenv("MCP_PUBLIC_URL", "https://env.example.com")
	t.Setenv("MCP_OAUTH", "true")
	t.Setenv("MCP_PORT", "5555")

	var captured *ServiceInstallState
	cmd := &cli.Command{
		Flags: managedServiceFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			s := &ServiceInstallState{}
			seedServiceFromFlagsAndEnv(c, s, "")
			captured = s
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{
		"pinner", "mcp", "service", "install",
		"--tunnel", "cloudflared",
		"--domain", "mcp.example.com",
		"--host", "127.0.0.1",
	}))

	require.NotNil(t, captured)
	require.Equal(t, tunnel.TunnelProviderCloudflared, captured.Provider)
	require.Equal(t, "mcp.example.com", captured.Domain)
	require.Equal(t, "127.0.0.1", captured.Host)
	require.Equal(t, "env-secret", captured.AuthToken, "auth token must source from MCP_AUTH_TOKEN env")
	require.Equal(t, "https://env.example.com", captured.PublicURL)
	require.NotNil(t, captured.OAuth, "MCP_OAUTH env must seed OAuth")
	require.True(t, *captured.OAuth, "MCP_OAUTH env must seed OAuth")
	require.NotNil(t, captured.Port, "MCP_PORT env must seed port")
	require.Equal(t, 5555, *captured.Port, "MCP_PORT env must seed port")
}

func TestInstallRemovesFreshFileOnValidationFailure(t *testing.T) {
	// Bootstrap writes a cloudflared env, then validation fails because MCP_DOMAIN
	// and the auth token are unset; the freshly created file must be removed (not
	// stranded) so a re-run can recover.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	// Provide --tunnel so we go through the flags bootstrap, but omit MCP_DOMAIN
	// and the auth token so validation fails.
	require.NoError(t, cmd.Set(serviceTunnelFlag, "cloudflared"))

	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.Error(t, err)
	require.NoFileExists(t, path, "freshly created env file must be removed when validation fails")
}

func TestResolveManagedServiceLifecycleSkipsProviderValidation(t *testing.T) {
	requireServiceBackend(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0600))
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	require.NoError(t, cmd.Set("env-file", path))
	_, err := resolveManagedService(context.Background(), cmd, false, false)
	require.NoError(t, err)
}

func TestInstallBootstrapOpenAIPassesProvider(t *testing.T) {
	requireServiceBackend(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "openai"))
	require.NoError(t, cmd.Set(serviceTunnelIDFlag, "tunnel_0123456789abcdef0123456789abcdef"))
	// The control-plane API key must be persisted to the env file (not just the
	// process environment) because the installed service reads ONLY the
	// env file at runtime.
	require.NoError(t, cmd.Set(serviceApiKeyFlag, "test-cp-api-key-abc123"))

	envFile, svc, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, path, envFile)
	require.NotNil(t, svc)
	// OpenAI runs an embedded tunnel, so the unit must NOT pass --http.
	cfg, err := serviceConfigForInstall(cmd, path, tunnel.TunnelProviderOpenAI)
	require.NoError(t, err)
	require.NotContains(t, cfg.Arguments, "--http")

	// The bootstrap must have persisted the API key into the file the service
	// will actually read.
	env, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "test-cp-api-key-abc123", env["CONTROL_PLANE_API_KEY"])
}

func TestServiceConfigForInstallPassesEnvFileUntouched(t *testing.T) {
	// The shared config builder is pure path-passing: it references the env
	// file by path and does NOT parse or fail on its contents. Env handling
	// (and any strictness) is per-backend in each platform file — systemd
	// (EnvironmentFile=), launchd (sources it via a wrapper), Windows SCM
	// (loads it into the registry). So a corrupt env file must not error here,
	// and EnvVars must stay empty.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("NOT A KEY VALUE LINE\n"), 0600))
	cmd := &cli.Command{}

	cfg, err := serviceConfigForInstall(cmd, path, tunnel.TunnelProviderOpenAI)
	require.NoError(t, err)
	require.Equal(t, path, cfg.EnvFile)
	require.Nil(t, cfg.EnvVars)
	require.FileExists(t, path)
}

// TestRestartManagedServiceNoOp guards that RestartManagedService is a safe
// no-op when the install state carries no backing service to restart (nil
// state, or no env file / provider). This keeps the MCP install password path
// from touching a live service when there is none to reload.
func TestRestartManagedServiceNoOp(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	ctx := context.Background()
	require.NoError(t, RestartManagedService(ctx, cmd, nil))
	require.NoError(t, RestartManagedService(ctx, cmd, &ServiceInstallState{}))
	// An env file without a provider, or a provider without an env file, is
	// still a no-op — neither can identify a managed service to restart.
	require.NoError(t, RestartManagedService(ctx, cmd, &ServiceInstallState{EnvFile: "mcp.env"}))
	require.NoError(t, RestartManagedService(ctx, cmd, &ServiceInstallState{Provider: tunnel.TunnelProviderNgrok}))
}
