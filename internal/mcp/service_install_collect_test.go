package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
