package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// stubNgrokAPI returns an *http.Client that routes api.ngrok.com requests to a
// handler scripted by handler, letting tests exercise the reserved_domains
// client without network access. It stubs the tunnel sub-package's shared HTTP
// client (duplicated here from the tunnel test package until Stage 1).
func stubNgrokAPI(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := tunnel.NgrokAPIHTTPClient
	tunnel.NgrokAPIHTTPClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		u := *r.URL
		u.Scheme = "http"
		u.Host = srv.Listener.Addr().String()
		r2 := r.Clone(r.Context())
		r2.URL = &u
		rr := httptest.NewRecorder()
		handler(rr, r2)
		return rr.Result(), nil
	})}
	t.Cleanup(func() { tunnel.NgrokAPIHTTPClient = orig })
	return tunnel.NgrokAPIHTTPClient
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

	// A provider already seeded (from flags/env) must not prompt again in a
	// HEADLESS run: the provider step early-returns inside Execute when the
	// provider is set and the run cannot prompt. Executing with a seeded
	// provider must return immediately without touching the interactive prompt
	// (which would block/fail in a non-TTY test), proving the seeded-path is
	// taken. (Interactive re-runs DO re-prompt with the current provider as an
	// editable default, so the operator can change it.)
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()
	state.Provider = tunnel.TunnelProviderCloudflared
	require.NoError(t, steps[0].Execute(context.Background(), state), "provider step should no-op when provider is already set")
	require.Equal(t, tunnel.TunnelProviderCloudflared, state.Provider, "provider must not be overwritten")

	// The write step persists the collected state to the env file.
	state.Domain = "mcp.example.com"
	require.NoError(t, steps[2].Execute(context.Background(), state))
	require.FileExists(t, envFile, "write step should persist the env file")
}

// recordingPrompter records prompter calls so a test can assert that prompts
// flow through the shared channel rather than private pterm widgets.
type recordingPrompter struct {
	selectLabels []string
	provider     string // value Select returns for the provider list
}

func (r *recordingPrompter) Select(label string, _ []string, _ string) (int, string, error) {
	r.selectLabels = append(r.selectLabels, label)
	return 0, r.provider, nil
}
func (r *recordingPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (r *recordingPrompter) Confirm(string, bool) (bool, error) { return false, nil }
func (r *recordingPrompter) Text(label, _, _ string) (string, error) {
	return "", errors.New("unexpected Text call")
}

// TestServiceInstallStepsPromptsThroughChannel guards the root fix for "we never
// get to pick the tunnel": the tunnel provider selection must be routed through
// the wizard's shared prompt channel (PrompterFrom(ctx)), NOT through a private
// raw-pterm widget. When the provider step runs with a bound prompter and an
// unset provider, the provider Select must hit the prompter — so a host wizard
// embedding these steps renders the provider pick on its own terminal channel.
func TestServiceInstallStepsPromptsThroughChannel(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}

	// Provider unset -> the provider step must prompt through the channel.
	state := &ServiceInstallState{EnvFile: envFile}
	steps := ServiceInstallSteps(state, cmd, envFile, nil)

	p := &recordingPrompter{provider: string(tunnel.TunnelProviderNgrok) + " - ngrok (free and easiest)"}
	ctx := fieldform.WithPrompter(context.Background(), p)

	// Execute ONLY the tunnel provider step (steps[0]) so the test is hermetic:
	// the token/config steps (which resolve the public URL, hitting the ngrok
	// API) are covered by their own tests. The provider pick is the fix under
	// test here.
	require.NoError(t, steps[0].Execute(ctx, state))

	require.Equal(t, []string{"MCP tunnel provider (exposes the remote MCP endpoint)"}, p.selectLabels,
		"tunnel provider pick must go through the shared prompter channel (so it renders inside a host wizard)")
	require.Equal(t, tunnel.TunnelProviderNgrok, state.Provider,
		"selected provider must be parsed into state from the channel selection")
}

// TestTunnelConfigStepSkipsNgrokTokenPromptWhenConfigured guards the
// "figure out the env ourselves" behavior: if the ngrok config file already
// carries a usable agent authtoken (the user ran `ngrok config add-authtoken`),
// the Tunnel-specific configuration step must resolve that token into state
// instead of prompting for it. It runs the step in non-interactive mode (where
// any surviving text prompt would error), so a passing run proves no prompt was
// reached for a value we could figure out from the existing config.
func TestTunnelConfigStepSkipsNgrokTokenPromptWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	ngrokCfg := filepath.Join(dir, "ngrok.yml")
	require.NoError(t, os.WriteFile(ngrokCfg, []byte(
		"version: 2\n"+
			"agent:\n"+
			"  authtoken: 2ABCdef123configured\n"), 0600))
	t.Setenv("NGROK_CONFIG", ngrokCfg)

	envFile := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token-abc"))
	// PublicURL is pre-set so the step's URL resolution short-circuits and this
	// test isolates the token-skip behavior (the URL path is covered by its own
	// tests).
	state := &ServiceInstallState{EnvFile: envFile, Provider: tunnel.TunnelProviderNgrok, TunnelName: "test", PublicURL: "https://you.ngrok-free.dev"}
	steps := ServiceInstallSteps(state, cmd, envFile, nil)

	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	require.NoError(t, steps[1].Execute(context.Background(), state),
		"tunnel-config step must not prompt for a token the ngrok config file already provides")
	require.Equal(t, "2ABCdef123configured", state.TunnelToken,
		"ngrok authtoken from the config file must be resolved into state (written to env)")
}

// TestTunnelConfigStepStillPromptsNgrokTokenWithoutConfig guards the inverse:
// with no ngrok config and no config-manager/fenv token, the step must fall
// through to the interactive prompt (verified by it erroring in non-interactive
// mode rather than silently writing a token-empty env that would fail
// validation).
func TestTunnelConfigStepStillPromptsNgrokTokenWithoutConfig(t *testing.T) {
	t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "absent.yml"))

	envFile := filepath.Join(t.TempDir(), "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	state := &ServiceInstallState{EnvFile: envFile, Provider: tunnel.TunnelProviderNgrok, TunnelName: "test"}
	steps := ServiceInstallSteps(state, cmd, envFile, nil)

	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	require.Error(t, steps[1].Execute(context.Background(), state),
		"without any existing ngrok credential the step must require an interactive prompt")
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
	_, _, err := CollectHTTPInstallWithCreated(context.Background(), cmd, created, false, true)
	require.Error(t, err, "invalid ngrok env must fail validation")
	require.Contains(t, err.Error(), "MCP_AUTH_TOKEN")
	require.NoFileExists(t, created, "a freshly-created-but-invalid env file must be removed (no secret left on disk)")

	// envFileCreated=false ↔ pre-existing file (standalone path): must keep it.
	dir2 := t.TempDir()
	preexisting := filepath.Join(dir2, "mcp.env")
	writeInvalid(preexisting)
	_, _, err = CollectHTTPInstallWithCreated(context.Background(), cmd, preexisting, false, false)
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
		"MCP_TUNNEL_PROVIDER": string(tunnel.TunnelProviderCloudflared),
		"MCP_DOMAIN":          "https://mcp.example.com",
		"MCP_AUTH_TOKEN":      "test-token",
	}
	require.NoError(t, service.WriteEnvironment(path, env))

	loaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "precondition: MCP_PUBLIC_URL unset")

	resolveServicePublicURL(path, loaded)
	require.Equal(t, "https://mcp.example.com", loaded["MCP_PUBLIC_URL"])

	// And it must be persisted back so later runs see it.
	reloaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", reloaded["MCP_PUBLIC_URL"])
}

func TestResolveServicePublicURLLeavesDynamicTunnelUnset(t *testing.T) {
	// Dynamic providers (ngrok free random subdomain, OpenAI) or a missing
	// domain have no stable derivable URL — MCP_PUBLIC_URL must stay unset so
	// the caller surfaces the limitation rather than writing a wrong URL.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{"MCP_TUNNEL_PROVIDER": string(tunnel.TunnelProviderNgrok)}
	require.NoError(t, service.WriteEnvironment(path, env))

	loaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	resolveServicePublicURL(path, loaded)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "no domain -> no derived URL")
}

func TestResolveServicePublicURLFillsLocalhost(t *testing.T) {
	// A no-tunnel (localhost) http install has no MCP_PUBLIC_URL (the runtime
	// serves on the loopback address), but the agent config needs a stable URL.
	// resolveServicePublicURL must derive http://<host>:<port> and PIN the
	// deterministic default port (else the server binds port 0 — a free port
	// the agent config cannot know) so the written entry is reachable.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{
		"MCP_AUTH_TOKEN": "test-token", // no MCP_TUNNEL_PROVIDER => localhost
	}
	require.NoError(t, service.WriteEnvironment(path, env))

	loaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "precondition: MCP_PUBLIC_URL unset")

	resolveServicePublicURL(path, loaded)
	require.Equal(t, "http://127.0.0.1:38550", loaded["MCP_PUBLIC_URL"], "localhost URL must use the loopback address and default port")
	require.Equal(t, "38550", loaded["MCP_PORT"], "the default port must be pinned and persisted so the service binds it")

	// And both must be persisted back so later runs (and the running service)
	// see them.
	reloaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:38550", reloaded["MCP_PUBLIC_URL"])
	require.Equal(t, "38550", reloaded["MCP_PORT"])
}

func TestResolveServicePublicURLFillsLocalhostCustomPort(t *testing.T) {
	// An explicit MCP_PORT on a localhost install wins over the default and is
	// reflected in the derived URL.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{
		"MCP_HOST": "localhost",
		"MCP_PORT": "43047",
	}
	require.NoError(t, service.WriteEnvironment(path, env))

	loaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	resolveServicePublicURL(path, loaded)
	require.Equal(t, "http://localhost:43047", loaded["MCP_PUBLIC_URL"])
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
	require.NoError(t, cmd.Set(serviceTunnelFlag, string(tunnel.TunnelProviderNgrok)))
	require.NoError(t, cmd.Set(serviceDomainFlag, "https://mcp.example.com"))
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token"))
	require.NoError(t, cmd.Set(serviceTunnelTokenFlag, "test-ngrok-token"))

	env, err := CollectHTTPInstall(context.Background(), cmd, path, false)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"],
		"one-shot install should derive the named-tunnel public URL")

	// And it must be persisted so later runs/install reads see it.
	reloaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", reloaded["MCP_PUBLIC_URL"])
}

func TestResolveServicePublicURLPinsDefaultOnZeroPort(t *testing.T) {
	// An explicit MCP_PORT=0 is the "pick a free port" sentinel — bind's
	// auto-assign value, which can never be connected to. On a localhost
	// install it must be treated like empty and pinned to the default port, or
	// the derived MCP_PUBLIC_URL becomes the unreachable http://<host>:0.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{
		"MCP_HOST": "127.0.0.1",
		"MCP_PORT": "0",
	}
	require.NoError(t, service.WriteEnvironment(path, env))

	loaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	resolveServicePublicURL(path, loaded)
	require.Equal(t, "38550", loaded["MCP_PORT"], "explicit --port 0 must pin the default port")
	require.Equal(t, "http://127.0.0.1:38550", loaded["MCP_PUBLIC_URL"], "port 0 must not yield an unreachable http://host:0 URL")
}

func TestCollectHTTPInstallOneShotResolvesLocalhostURL(t *testing.T) {
	// A one-shot (non --service) http install with NO tunnel provider
	// (localhost) must derive the loopback MCP_PUBLIC_URL and pin MCP_PORT to
	// defaultLocalhostPort — otherwise runMcpInstall fails with "tunnel
	// collection produced no MCP_PUBLIC_URL" despite a valid localhost server,
	// and the service would bind an unpredictable free port.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	// Pre-create the env file so the one-shot collector treats it as an existing
	// localhost config (no tunnel provider) instead of launching the interactive
	// wizard, which would block on a prompt.
	require.NoError(t, service.WriteEnvironment(path, ServiceEnvironment{"MCP_AUTH_TOKEN": "test"}))
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	// No MCP_TUNNEL_PROVIDER flag => localhost mode; no tunnel credentials.

	env, err := CollectHTTPInstall(context.Background(), cmd, path, false)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:38550", env["MCP_PUBLIC_URL"],
		"one-shot localhost install should derive the loopback public URL")

	// And the pinned port + URL must be persisted so the running service binds
	// the same port the agent config references.
	reloaded, err := service.LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "38550", reloaded["MCP_PORT"])
	require.Equal(t, "http://127.0.0.1:38550", reloaded["MCP_PUBLIC_URL"])
}

// TestTunnelProviderChoiceLabelsDefaultIsLocalhost guards the tunnel provider
// select defaults and copy: localhost must be listed FIRST so the interactive
// select highlights it as the default — the simplest path needs no tunnel. It
// also verifies every option's leading token (before " - ") parses back to a
// valid provider or the localhost marker, so the select -> parse round-trip can
// never silently fail, and that the descriptions are end-user friendly (no
// internal jargon like "embedded" / "external binary").
func TestTunnelProviderChoiceLabelsDefaultIsLocalhost(t *testing.T) {
	labels := tunnelProviderChoiceLabels()
	require.NotEmpty(t, labels, "must present at least one provider")
	require.True(t, strings.HasPrefix(labels[0], "localhost - "),
		"localhost must be the first (default) provider option, got %q", labels[0])

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
		// localhost is the marker for "no tunnel": it parses to the empty
		// provider without error, which is the expected (not failure) case.
		if token == "localhost" {
			require.Equal(t, tunnel.TunnelProvider(""), prov, "localhost token must map to the empty (no-tunnel) provider")
			continue
		}
		require.NotEqual(t, tunnel.TunnelProvider(""), prov, "option token %q must map to a known provider", token)
	}
}

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
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token"))

	// Non-interactive so the URL-resolution API path (which needs no prompt) is
	// the only path exercised; the API key is supplied via the seeded state.
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	state := &ServiceInstallState{
		EnvFile:     envFile,
		Provider:    tunnel.TunnelProviderNgrok,
		NgrokAPIKey: "api-key", // API key present -> resolve URL from API
		OAuth:       new(true),
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
	env, err := service.LoadEnvironment(envFile)
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", env["MCP_PUBLIC_URL"])
	require.Equal(t, "true", env["MCP_OAUTH"], "OAuth choice must be written")
}

// TestFlattenedNgrokCollectorHandoff reproduces the FULL Configure Tunnel step
// of `mcp install`: run the flattened tunnel-config sub-steps (which write the
// env file with the resolved MCP_PUBLIC_URL), THEN hand the file to the
// production HTTP collector exactly as the wizard's collectHTTP does, and assert
// the collector returns MCP_PUBLIC_URL (the precondition mcp_install.go checks).
// This is the regeneration-guard for the reported glitch:
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
	require.NoError(t, realCmd.Set(serviceAuthTokenFlag, "test-auth-token"))
	// Derive the env file from the same resolver the collector uses so the two
	// phases agree on the path regardless of platform.
	envFile, err := ResolveServiceEnvFile(realCmd)
	require.NoError(t, err)

	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	// The flattened sub-steps (what w.tunnelConfigurer runs) resolve the URL
	// from the API and write it as MCP_PUBLIC_URL.
	state := &ServiceInstallState{
		EnvFile:     envFile,
		Provider:    tunnel.TunnelProviderNgrok,
		NgrokAPIKey: "api-key", // API key present -> resolve URL from API
		OAuth:       new(true),
	}
	cfgMgr := ServiceConfigManager()
	for _, step := range ServiceInstallSteps(state, realCmd, envFile, cfgMgr) {
		if step.ShouldSkip(state) {
			continue
		}
		require.NoError(t, step.Execute(context.Background(), state))
	}

	// Then the production HTTP collector (what w.collectHTTP runs) must see the
	// MCP_PUBLIC_URL the sub-steps wrote, not return it empty.
	env, _, err := collectHTTPInstall(context.Background(), realCmd, "", false, true)
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", env["MCP_PUBLIC_URL"],
		"the collector must surface the URL the configurer wrote (no 'no MCP_PUBLIC_URL' failure)")
	require.Equal(t, "true", env["MCP_OAUTH"])
	require.Equal(t, string(tunnel.TunnelProviderNgrok), env["MCP_TUNNEL_PROVIDER"])
}

func TestIsServiceInstallSeeded(t *testing.T) {
	cases := []struct {
		name   string
		svc    *ServiceInstallState
		seeded bool
	}{
		{"nil stays undecided", nil, false},
		{"no provider stays undecided", &ServiceInstallState{}, false},
		{"openai complete", &ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), ApiKey: "k", AuthToken: "a"}, true},
		{"openai no api key", &ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), AuthToken: "a"}, false},
		{"openai malformed tunnel id", &ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "t_1", ApiKey: "k", AuthToken: "a"}, false},
		{"openai no auth token", &ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), ApiKey: "k"}, false},
		{"cloudflared complete", &ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, Domain: "d.example", TunnelName: "pin", AuthToken: "a"}, true},
		{"cloudflared no auth token", &ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, Domain: "d.example", TunnelName: "pin"}, false},
		{"cloudflared no domain", &ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, TunnelName: "pin", AuthToken: "a"}, false},
		{"ngrok complete", &ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, true},
		{"ngrok no auth token", &ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", PublicURL: "https://u.ngrok-free.dev"}, false},
		{"ngrok no token", &ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, false},
		{"ngrok no public url", &ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsServiceInstallSeeded(tc.svc); got != tc.seeded {
				t.Errorf("IsServiceInstallSeeded = %v, want %v", got, tc.seeded)
			}
		})
	}
}

// fakeManagedService records lifecycle calls so installManagedService's
// stop-if-installed → install → start sequence can be asserted without a live
// init system. It embeds service.Service so unimplemented methods panic rather
// than silently succeed.
type fakeManagedService struct {
	service.Service
	status      service.Status
	statusErr   error
	stopErr     error
	installErr  error
	startErr    error
	calledStop  bool
	calledStart bool
}

func (f *fakeManagedService) Status(_ context.Context) (service.Status, error) { return f.status, f.statusErr }
func (f *fakeManagedService) Stop(_ context.Context) error                     { f.calledStop = true; return f.stopErr }
func (f *fakeManagedService) Install(_ context.Context) error                  { return f.installErr }
func (f *fakeManagedService) Start(_ context.Context) error                    { f.calledStart = true; return f.startErr }

func TestStopManagedServiceIfInstalled(t *testing.T) {
	t.Run("not installed is a no-op", func(t *testing.T) {
		fake := &fakeManagedService{} // Status returns zero: not installed
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		stopped, err := StopManagedServiceIfInstalled(context.Background())
		require.NoError(t, err)
		require.False(t, stopped, "no stop reported when service is not installed")
		require.False(t, fake.calledStop, "no Stop on a service that is not installed")
	})

	t.Run("installed service is stopped", func(t *testing.T) {
		fake := &fakeManagedService{status: service.Status{Installed: true}}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		stopped, err := StopManagedServiceIfInstalled(context.Background())
		require.NoError(t, err)
		require.True(t, stopped, "stop must be reported when service was running")
		require.True(t, fake.calledStop, "an installed service must be stopped")
	})

	t.Run("status error propagates", func(t *testing.T) {
		fake := &fakeManagedService{statusErr: errors.New("probe failed")}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		stopped, err := StopManagedServiceIfInstalled(context.Background())
		require.Error(t, err)
		require.False(t, stopped)
		require.False(t, fake.calledStop)
	})

	t.Run("stop error propagates", func(t *testing.T) {
		fake := &fakeManagedService{
			status:  service.Status{Installed: true},
			stopErr: errors.New("stop failed"),
		}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		stopped, err := StopManagedServiceIfInstalled(context.Background())
		require.Error(t, err)
		require.False(t, stopped, "stop not reported when Stop failed")
		require.True(t, fake.calledStop)
	})
}

func TestStartManagedServiceIfInstalled(t *testing.T) {
	t.Run("not installed is a no-op", func(t *testing.T) {
		fake := &fakeManagedService{}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		err := StartManagedServiceIfInstalled(context.Background())
		require.NoError(t, err)
		require.False(t, fake.calledStart, "no Start on a service that is not installed")
	})

	t.Run("installed service is started", func(t *testing.T) {
		fake := &fakeManagedService{status: service.Status{Installed: true}}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		err := StartManagedServiceIfInstalled(context.Background())
		require.NoError(t, err)
		require.True(t, fake.calledStart, "an installed service must be started")
	})

	t.Run("status error propagates", func(t *testing.T) {
		fake := &fakeManagedService{statusErr: errors.New("probe failed")}
		orig := newServiceForControl
		newServiceForControl = func() (service.Service, error) { return fake, nil }
		defer func() { newServiceForControl = orig }()

		err := StartManagedServiceIfInstalled(context.Background())
		require.Error(t, err)
		require.False(t, fake.calledStart)
	})
}

func TestInstallManagedService(t *testing.T) {
	t.Run("fresh install skips stop then installs and starts", func(t *testing.T) {
		svc := &fakeManagedService{} // Status returns zero-value: not installed
		err := installManagedService(context.Background(), svc)
		require.NoError(t, err)
		require.False(t, svc.calledStop, "no Stop expected on a service that is not installed")
		require.True(t, svc.calledStart, "Start must run because Install does not auto-start")
	})

	t.Run("installed service is stopped before reinstall then started", func(t *testing.T) {
		svc := &fakeManagedService{status: service.Status{Installed: true}}
		err := installManagedService(context.Background(), svc)
		require.NoError(t, err)
		require.True(t, svc.calledStop, "an installed service must be stopped before reinstall")
		require.True(t, svc.calledStart, "Start must run after Install")
	})

	t.Run("status error propagates before any lifecycle call", func(t *testing.T) {
		svc := &fakeManagedService{statusErr: errors.New("probe failed")}
		err := installManagedService(context.Background(), svc)
		require.Error(t, err)
		require.False(t, svc.calledStop)
		require.False(t, svc.calledStart)
	})

	t.Run("stop error propagates and aborts install", func(t *testing.T) {
		svc := &fakeManagedService{status: service.Status{Installed: true}, stopErr: errors.New("stop failed")}
		err := installManagedService(context.Background(), svc)
		require.Error(t, err)
		require.True(t, svc.calledStop)
		require.False(t, svc.calledStart, "Start must not run when Stop fails")
	})

	t.Run("only start-error aborts and other install errors propagate", func(t *testing.T) {
		svc := &fakeManagedService{installErr: errors.New("other failure")}
		err := installManagedService(context.Background(), svc)
		require.Error(t, err)
		require.False(t, svc.calledStart, "Start must not run on a non-already-exists install error")
	})

	t.Run("already-installed install error restarts the stopped service", func(t *testing.T) {
		svc := &fakeManagedService{status: service.Status{Installed: true}, installErr: service.ErrServiceAlreadyExists}
		err := installManagedService(context.Background(), svc)
		require.NoError(t, err)
		require.True(t, svc.calledStop, "the installed service must be stopped before the failed reinstall")
		require.True(t, svc.calledStart, "the stopped service must be restarted so its endpoint stays up")
	})

	t.Run("already-installed install error without stop propagates", func(t *testing.T) {
		// A backend that reports ErrServiceAlreadyExists without the caller
		// having stopped anything must not silently succeed.
		svc := &fakeManagedService{installErr: service.ErrServiceAlreadyExists}
		err := installManagedService(context.Background(), svc)
		require.Error(t, err)
		require.False(t, svc.calledStart)
	})
}
