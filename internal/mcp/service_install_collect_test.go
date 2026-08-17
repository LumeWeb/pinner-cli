package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestCollectHTTPInstallNonInteractiveMissingEnvErrors(t *testing.T) {
	// A --no-interactive http install with no pre-existing env file and no
	// --tunnel must fail fast with a clear error rather than falling through to
	// the interactive RunServiceInstallWizard (which would block in a headless
	// context).
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: append(managedServiceFlags(), &cli.BoolFlag{Name: "non-interactive"})}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set("non-interactive", "true"))

	_, err := CollectHTTPInstall(context.Background(), cmd, path, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pass --tunnel")
	require.NoFileExists(t, path, "no env file should be written when non-interactive setup is refused")
}

// TestServiceInstallStepsShape guards the extraction of the tunnel-config steps
// from RunServiceInstallWizard into the reusable ServiceInstallSteps list (the
// flatten refactor). It asserts the step names/order, that a pre-seeded provider
// skips the provider prompt, and that the write step writes the env file — the
// exact behavior RunServiceInstallWizard ran standalone, now shared with mcp
// install.
func TestServiceInstallStepsShape(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}

	state := &ServiceInstallState{EnvFile: envFile}
	steps := ServiceInstallSteps(state, cmd, envFile, nil)

	require.Len(t, steps, 3, "expected tunnel provider, tunnel config, and env-write steps")
	var names []string
	for _, s := range steps {
		names = append(names, s.Name())
	}
	require.Equal(t, []string{"Tunnel provider", "Tunnel-specific configuration", "Write service environment file"}, names)

	// A provider already seeded (from flags/env) must not prompt again: the
	// provider step has no SkipFunc but early-returns inside Execute. Executing
	// with a seeded provider must return immediately without touching the
	// interactive selectUI (which would block/fail in a non-TTY test), proving
	// the seeded-path is taken.
	state.Provider = TunnelProviderCloudflared
	require.NoError(t, steps[0].Execute(context.Background(), state), "provider step should no-op when provider is already set")
	require.Equal(t, TunnelProviderCloudflared, state.Provider, "provider must not be overwritten")

	// The write step persists the collected state to the env file.
	state.Domain = "mcp.example.com"
	require.NoError(t, steps[2].Execute(context.Background(), state))
	require.FileExists(t, envFile, "write step should persist the env file")
}

// TestSeedServiceFromFlagsAndEnv guards the flatten: an interactive `mcp install
// --transport http` with credentials supplied via flags/env (but no --tunnel and
// no existing env file) must NOT re-prompt for them. The tunnelConfigurer pre-
// seeds the embedded service state via SeedServiceFromFlagsAndEnv before running
// the steps, matching RunServiceInstallWizard; a missing pre-seed would re-prompt
// for secrets the user already provided.
func TestSeedServiceFromFlagsAndEnv(t *testing.T) {
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "flag-auth-token"))
	require.NoError(t, cmd.Set(serviceTunnelTokenFlag, "flag-ngrok-token"))
	require.NoError(t, cmd.Set(serviceDomainFlag, "flag.example.com"))
	require.NoError(t, cmd.Set(serviceEnvFileFlag, "mcp.env"))

	state := &ServiceInstallState{}
	SeedServiceFromFlagsAndEnv(cmd, state, "mcp.env")

	require.Equal(t, "flag-auth-token", state.AuthToken, "flag-provided auth token seeded")
	require.Equal(t, "flag-ngrok-token", state.TunnelToken, "flag-provided tunnel token seeded")
	require.Equal(t, "flag.example.com", state.Domain, "flag-provided domain seeded")

	// Values already present must not be overwritten by seeding.
	state2 := &ServiceInstallState{AuthToken: "pre-existing", Domain: "kept.example.com"}
	SeedServiceFromFlagsAndEnv(cmd, state2, "mcp.env")
	require.Equal(t, "pre-existing", state2.AuthToken, "seeding must not clobber explicit state")
	require.Equal(t, "kept.example.com", state2.Domain, "seeding must not clobber explicit state")
}

// TestCollectHTTPInstallWithCreatedCleanup guards the flattened mcp install
// path: the spliced tunnel-config steps write the env file (with the user's
// secret) BEFORE the collector runs, so CollectHTTPInstall sees it as
// pre-existing and would skip its validation-failure cleanup — leaving a
// partial env file holding MCP_AUTH_TOKEN on disk and dead-ending the next run
// (needsFreshTunnelPrompt reads file-exists). CollectHTTPInstallWithCreated
// with envFileCreated=true must remove the freshly-written-but-invalid file on
// validation failure, while envFileCreated=false (pre-existing) must keep it
// untouched — preserving the standalone "never touch a pre-existing file"
// invariant.
func TestCollectHTTPInstallWithCreatedCleanup(t *testing.T) {
	writeInvalid := func(path string) {
		// ngrok requires MCP_AUTH_TOKEN; omitting it fails validation
		// deterministically without depending on any tunnel binary on PATH.
		require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0o600))
	}

	cmd := &cli.Command{Flags: managedServiceFlags()}

	// envFileCreated=true ↔ flattened path freshly wrote the file: must remove.
	dir1 := t.TempDir()
	created := filepath.Join(dir1, "mcp.env")
	writeInvalid(created)
	_, err := CollectHTTPInstallWithCreated(context.Background(), cmd, created, false, true)
	require.Error(t, err, "invalid ngrok env must fail validation")
	require.Contains(t, err.Error(), "MCP_AUTH_TOKEN")
	require.NoFileExists(t, created, "a freshly-created-but-invalid env file must be removed (no secret left on disk)")

	// envFileCreated=false ↔ pre-existing file (standalone path): must keep it.
	dir2 := t.TempDir()
	preexisting := filepath.Join(dir2, "mcp.env")
	writeInvalid(preexisting)
	_, err = CollectHTTPInstallWithCreated(context.Background(), cmd, preexisting, false, false)
	require.Error(t, err, "invalid ngrok env must fail validation")
	require.FileExists(t, preexisting, "a pre-existing env file must never be removed")
}

func TestResolveServicePublicURLFillsCloudflaredDomain(t *testing.T) {
	// A named cloudflared tunnel with a custom domain has a deterministic
	// public URL after the service starts; resolveServicePublicURL must derive
	// and persist it rather than leaving MCP_PUBLIC_URL empty (which would make
	// the HTTP install fail despite a working tunnel).
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": string(TunnelProviderCloudflared),
		"MCP_DOMAIN":          "https://mcp.example.com",
		"MCP_AUTH_TOKEN":      "test-token",
	}
	require.NoError(t, WriteServiceEnvironment(path, env))

	loaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "precondition: MCP_PUBLIC_URL unset")

	resolveServicePublicURL(path, loaded)
	require.Equal(t, "https://mcp.example.com", loaded["MCP_PUBLIC_URL"])

	// And it must be persisted back so later runs see it.
	reloaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", reloaded["MCP_PUBLIC_URL"])
}

func TestResolveServicePublicURLLeavesDynamicTunnelUnset(t *testing.T) {
	// Dynamic providers (ngrok free random subdomain, OpenAI) or a missing
	// domain have no stable derivable URL — MCP_PUBLIC_URL must stay unset so
	// the caller surfaces the limitation rather than writing a wrong URL.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{"MCP_TUNNEL_PROVIDER": string(TunnelProviderNgrok)}
	require.NoError(t, WriteServiceEnvironment(path, env))

	loaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	resolveServicePublicURL(path, loaded)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "no domain -> no derived URL")
}

func TestCollectHTTPInstallOneShotResolvesNamedDomainURL(t *testing.T) {
	// A one-shot (non --service) http install with a named ngrok tunnel and a
	// custom domain but no explicit --public-url must derive MCP_PUBLIC_URL
	// from MCP_DOMAIN; otherwise runMcpInstall fails despite a valid tunnel.
	// validateServiceEnvironment requires the tunnel binary on PATH (LookPath),
	// so put a stub executable there (it is never executed in the one-shot
	// (wantService=false) path). Windows LookPath only matches executables with a
	// PATHEXT extension (.exe/.cmd/.bat), so use ngrok.cmd there instead of the
	// Unix shell stub.
	binDir := t.TempDir()
	stubName := "ngrok"
	stubContent := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		stubName = "ngrok.cmd"
		stubContent = []byte("@echo off\r\n")
	}
	stub := filepath.Join(binDir, stubName)
	require.NoError(t, os.WriteFile(stub, stubContent, 0o755))
	t.Setenv("PATH", binDir)

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, string(TunnelProviderNgrok)))
	require.NoError(t, cmd.Set(serviceDomainFlag, "https://mcp.example.com"))
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token"))
	require.NoError(t, cmd.Set(serviceTunnelTokenFlag, "test-ngrok-token"))

	env, err := CollectHTTPInstall(context.Background(), cmd, path, false)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"],
		"one-shot install should derive the named-tunnel public URL")

	// And it must be persisted so later runs/install reads see it.
	reloaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", reloaded["MCP_PUBLIC_URL"])
}

// TestTunnelProviderChoiceLabelsDefaultIsNgrok guards the tunnel provider select
// defaults and copy: ngrok must be listed FIRST so the interactive select
// highlights it as the default — not cloudflared. It also verifies every
// option's leading token (before " - ") parses back to a valid provider, so the
// select -> parse round-trip can never silently fail, and that the descriptions
// are end-user friendly (no internal jargon like "embedded" / "external binary").
func TestTunnelProviderChoiceLabelsDefaultIsNgrok(t *testing.T) {
	labels := tunnelProviderChoiceLabels()
	require.NotEmpty(t, labels, "must present at least one provider")
	require.True(t, strings.HasPrefix(labels[0], "ngrok - "),
		"ngrok must be the first (default) provider option, got %q", labels[0])

	jargon := []string{"embedded", "external binary"}
	for _, label := range labels {
		for _, j := range jargon {
			require.NotContains(t, label, j,
				"provider description %q should not surface the implementation detail %q", label, j)
		}
		sep := strings.Index(label, " - ")
		require.Positive(t, sep, "option %q must use the 'token - descriptor' form", label)
		token := label[:sep]
		prov, err := parseTunnelProvider(token)
		require.NoError(t, err, "option token %q should parse to a valid provider", token)
		require.NotEqual(t, TunnelProvider(""), prov, "option token %q must map to a known provider", token)
	}
}
