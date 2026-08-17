package mcp

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
)

// TestFlattenedNgrokWritesPublicURL reproduces the reported glitch end-to-end
// at the flattened-sub-step level (the path `mcp install`'s Configure Tunnel
// step runs): the ngrok configurer resolves MCP_PUBLIC_URL from the ngrok API
// and the "Write service environment file" step must persist it, so a later
// collector re-reading the file sees MCP_PUBLIC_URL (not empty -> no failure).
func TestFlattenedNgrokWritesPublicURL(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`))
	})

	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}

	// Non-interactive so the URL-resolution API path (which needs no prompt) is
	// the only path exercised; the API key is supplied via the seeded state.
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	state := &ServiceInstallState{
		EnvFile:     envFile,
		Provider:    TunnelProviderNgrok,
		NgrokAPIKey: "ngrok_key", // API key present -> resolve URL from API
		AuthToken:   "auth",
		OAuth:       true,
	}
	cfgMgr := ServiceConfigManager()
	steps := ServiceInstallSteps(state, cmd, envFile, cfgMgr)
	for _, step := range steps {
		if step.ShouldSkip(state) {
			continue
		}
		require.NoError(t, step.Execute(context.Background(), state))
	}

	// The written env file must carry MCP_PUBLIC_URL.
	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "MCP_PUBLIC_URL=https://you.ngrok-free.dev",
		"flattened ngrok path must persist the resolved public URL to the env file")

	// And loading it back must yield a non-empty MCP_PUBLIC_URL (the collector's
	// precondition), not the reported "no MCP_PUBLIC_URL" failure.
	env, err := LoadServiceEnvironment(envFile)
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", env["MCP_PUBLIC_URL"])
	require.Equal(t, "true", env["MCP_OAUTH"], "OAuth choice must be written")
}

// TestFlattenedNgrokCollectorHandoff reproduces the FULL Configure Tunnel step
// of `mcp install`: run the flattened tunnel-config sub-steps (which write the
// env file with the resolved MCP_PUBLIC_URL), THEN hand the file to the
// production HTTP collector exactly as the wizard's collectHTTP does, and assert
// the collector returns MCP_PUBLIC_URL (the precondition mcp_install.go:198
// checks). This is the regeneration-guard for the reported glitch:
// "tunnel collection produced no MCP_PUBLIC_URL".
func TestFlattenedNgrokCollectorHandoff(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`))
	})

	dir := t.TempDir()
	// Point the OS config dir at the temp root on every platform so
	// resolveServiceEnvFile (used by both the sub-steps and the production
	// collector, which passes envFile="") resolves to the same file the
	// sub-steps write — faithful to production. os.UserConfigDir() honors
	// XDG_CONFIG_HOME on Linux, $HOME/Library/Application Support on macOS, and
	// %AppData% on Windows.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))

	// A real command shadow carrying the shared service flags, as production
	// builds for `mcp install` and `mcp service`.
	realCmd := &cli.Command{Flags: managedServiceFlags()}
	// Derive the env file from the same resolver the collector uses so the two
	// phases agree on the path regardless of platform.
	envFile, err := ResolveServiceEnvFile(realCmd)
	require.NoError(t, err)

	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	// Phase 1: the flattened sub-steps (what w.tunnelConfigurer runs).
	state := &ServiceInstallState{
		EnvFile:     envFile,
		Provider:    TunnelProviderNgrok,
		NgrokAPIKey: "ngrok_key_123",
		AuthToken:   "auth",
		OAuth:       true,
	}
	cfgMgr := ServiceConfigManager()
	for _, step := range ServiceInstallSteps(state, realCmd, envFile, cfgMgr) {
		if step.ShouldSkip(state) {
			continue
		}
		require.NoError(t, step.Execute(context.Background(), state))
	}

	// Phase 2: the production HTTP collector (what w.collectHTTP runs). It must
	// see the MCP_PUBLIC_URL the sub-steps wrote, not return it empty.
	env, err := collectHTTPInstall(context.Background(), realCmd, "", false, true)
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", env["MCP_PUBLIC_URL"],
		"the collector must surface the URL the configurer wrote (no 'no MCP_PUBLIC_URL' failure)")
	require.Equal(t, "true", env["MCP_OAUTH"])
	require.Equal(t, string(TunnelProviderNgrok), env["MCP_TUNNEL_PROVIDER"])
}
