package mcp

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

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// testPrompter is a wizard.Prompter stub for configurer tests. The configurers
// run with wizard.NonInteractive=true, so any prompt they reach must fail with
// an "interactive" error (mirroring the production pterm prompter under
// non-interactive mode) rather than blocking on a real TTY. Tests that expect a
// value to resolve from config/env without prompting therefore get a pass, while
// tests that assert a prompt IS required see the non-interactive error.
type testPrompter struct{}

func (testPrompter) Select(string, []string) (int, string, error) {
	return 0, "", errors.New("interactive prompt requested in non-interactive mode")
}
func (testPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, errors.New("interactive prompt requested in non-interactive mode")
}
func (testPrompter) Confirm(string, bool) (bool, error) {
	return false, errors.New("interactive prompt requested in non-interactive mode")
}
func (testPrompter) Text(string, string) (string, error) {
	return "", errors.New("interactive prompt requested in non-interactive mode")
}

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

	// A provider already seeded (from flags/env) must not prompt again: the
	// provider step has no SkipFunc but early-returns inside Execute. Executing
	// with a seeded provider must return immediately without touching the
	// interactive prompt (which would block/fail in a non-TTY test), proving
	// the seeded-path is taken.
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

func (r *recordingPrompter) Select(label string, _ []string) (int, string, error) {
	r.selectLabels = append(r.selectLabels, label)
	return 0, r.provider, nil
}
func (r *recordingPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (r *recordingPrompter) Confirm(string, bool) (bool, error) { return false, nil }
func (r *recordingPrompter) Text(label, _ string) (string, error) {
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
	ctx := wizard.WithPrompter(context.Background(), p)

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
	// PublicURL is pre-set so the step's URL resolution short-circuits and this
	// test isolates the token-skip behavior (the URL path is covered by its own
	// tests).
	state := &ServiceInstallState{EnvFile: envFile, Provider: tunnel.TunnelProviderNgrok, TunnelName: "test", AuthToken: "test-auth", PublicURL: "https://you.ngrok-free.dev"}
	steps := ServiceInstallSteps(state, cmd, envFile, nil)

	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

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

	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

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
		require.NotEqual(t, tunnel.TunnelProvider(""), prov, "option token %q must map to a known provider", token)
	}
}

// TestCloudflaredConfigurerResolvesNameWithoutHostname guards the cloudflared
// auto-fill: a provisioned tunnel state with a TunnelName but no Hostname yet
// (e.g. before the DNS route exists) must still resolve the tunnel name instead
// of re-prompting. The Hostname and TunnelName fills are gated independently —
// the whole block must not be skipped just because the hostname is absent.
func TestCloudflaredConfigurerResolvesNameWithoutHostname(t *testing.T) {
	dir := t.TempDir()
	// tunnel.TunnelStatePath is a package var; point it at a fixture with a TunnelName
	// but NO Hostname (pre-DNS-route provisioning state).
	statePath := filepath.Join(dir, "tunnel-state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(
		`{"provider":"cloudflared","tunnel_name":"provisioned-named-tunnel","account_id":"acct","tunnel_id":"tun","secret":"not-a-real-cred"}`+"\n"), 0600))
	orig := tunnel.TunnelStatePath
	tunnel.TunnelStatePath = func() (string, error) { return statePath, nil }
	defer func() { tunnel.TunnelStatePath = orig }()

	state := &ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, Domain: "mcp.example.com"}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	require.NoError(t, cloudflaredConfigurer(context.Background(), testPrompter{}, state, nil),
		"tunnel name must resolve from the provisioned state without prompting")
	require.Equal(t, "provisioned-named-tunnel", state.TunnelName,
		"tunnel name should come from the provisioned state even when the hostname is absent")
	require.Equal(t, "mcp.example.com", state.Domain, "pre-supplied domain must be preserved")
}

// TestNgrokConfigurerResolvesPublicURLFromAPI guards the "identify what the
// user has" path: when an ngrok API key is supplied and the API reports a free
// dev domain, the configurer must resolve MCP_PUBLIC_URL from that (so the
// interactive install no longer fails with "no MCP_PUBLIC_URL") without
// prompting. This is the regression guard for the free-ngrok install failure.
func TestNgrokConfigurerResolvesPublicURLFromAPI(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`))
	})

	state := &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "tun-token", // pre-supplied so the token prompt is skipped
		NgrokAPIKey: "ngrok_key",
		AuthToken:   "auth",
	}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	require.NoError(t, ngrokConfigurer(context.Background(), testPrompter{}, state, nil),
		"ngrok configurer must resolve the URL from the API without prompting")
	require.Equal(t, "https://you.ngrok-free.dev", state.PublicURL,
		"MCP_PUBLIC_URL must be the account's free dev domain from the ngrok API")
	require.Equal(t, "", state.TunnelName,
		"ngrok must NOT populate a tunnel resource name (no such prompt)")
}

// TestNgrokConfigurerHonorsOperatorDomain guards that the operator's explicit
// --domain (MCP_DOMAIN), when it exists in the account's reserved-domain set,
// is published as MCP_PUBLIC_URL ahead of the free dev domain. This is the
// paid-account path that previously resolved to the free dev domain because
// s.Domain was never threaded into the ngrok URL resolution.
func TestNgrokConfigurerHonorsOperatorDomain(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[
			{"id":"rd_1","domain":"my-app.ngrok.app","cname_target":"tunnel.ngrok.io"},
			{"id":"rd_2","domain":"you.ngrok-free.dev","cname_target":null}
		],"next_page_uri":null}`))
	})

	state := &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "tun-token",
		NgrokAPIKey: "ngrok_key",
		AuthToken:   "auth",
		Domain:      "my-app.ngrok.app", // operator's explicit --domain
	}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	require.NoError(t, ngrokConfigurer(context.Background(), testPrompter{}, state, nil))
	require.Equal(t, "https://my-app.ngrok.app", state.PublicURL,
		"operator's --domain present in the reserved set must be chosen over the free dev domain")
}

// TestNgrokConfigurerResolvesURLFromAuthtoken guards the new temp-tunnel
// fallback: with no API key but an authtoken present, the configurer resolves
// MCP_PUBLIC_URL from a short-lived embedded ngrok tunnel (the account's stable
// free dev domain) — no API key required and no manual paste. This is the
// free-account path that previously stranded the install with "no MCP_PUBLIC_URL".
func TestNgrokConfigurerResolvesURLFromAuthtoken(t *testing.T) {
	// Stub the SDK URL resolver: no network, deterministic free dev URL.
	orig := tunnel.ResolveNgrokSDKURL
	tunnel.ResolveNgrokSDKURL = func(_ context.Context, token string) (string, error) {
		require.Equal(t, "tun-token", token, "the authtoken must be passed to the SDK resolver")
		return "https://you.ngrok-free.dev", nil
	}
	defer func() { tunnel.ResolveNgrokSDKURL = orig }()

	state := &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "tun-token",
		AuthToken:   "auth",
		// No NgrokAPIKey, no NGROK_API_KEY env -> authtoken fallback must fire.
	}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	require.NoError(t, ngrokConfigurer(context.Background(), testPrompter{}, state, nil))
	require.Equal(t, "https://you.ngrok-free.dev", state.PublicURL,
		"no-API-key free-account install must resolve the URL from the authtoken")
}

// TestNgrokConfigurerRejectsEphemeralAuthtokenURL guards finding 3: a bare
// tunnel on some free accounts is assigned an EPHEMERAL *.ngrok-free.app URL
// that rotates per session. Such a URL must NOT be persisted as MCP_PUBLIC_URL
// (it would point at a dead endpoint), so the configurer falls through to the
// interactive prompt instead of installing a rotating URL.
func TestNgrokConfigurerRejectsEphemeralAuthtokenURL(t *testing.T) {
	orig := tunnel.ResolveNgrokSDKURL
	tunnel.ResolveNgrokSDKURL = func(_ context.Context, _ string) (string, error) {
		return "https://abc123.ngrok-free.app", nil
	}
	defer func() { tunnel.ResolveNgrokSDKURL = orig }()

	state := &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "tun-token",
		AuthToken:   "auth",
	}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	err := ngrokConfigurer(context.Background(), testPrompter{}, state, nil)
	require.Error(t, err,
		"an ephemeral ngrok URL must not be persisted; the configurer must fall through to the prompt")
	require.Contains(t, err.Error(), "interactive")
	require.Empty(t, state.PublicURL, "ephemeral URL must not be stored")
}

// TestIsStableNgrokDevURL pins the stability guard: only *.ngrok-free.dev hosts
// (the account's persistent reserved dev domain) are accepted; ephemeral
// *.ngrok-free.app, custom domains, and malformed input are rejected.
func TestIsStableNgrokDevURL(t *testing.T) {
	stable := []string{
		"https://unlagging-overtheatrically-ayesha.ngrok-free.dev",
		"https://example.ngrok-free.dev",
		"http://example.ngrok-free.dev",
	}
	for _, u := range stable {
		require.Truef(t, tunnel.IsStableNgrokDevURL(u), "want stable: %s", u)
	}
	ephemeral := []string{
		"https://abc123.ngrok-free.app",
		"https://abc123.ngrok.app",
		"https://mcp.example.com",
		"",
		"not-a-url",
		"https://ngrok-free.dev", // bare TLD, no subdomain
	}
	for _, u := range ephemeral {
		require.Falsef(t, tunnel.IsStableNgrokDevURL(u), "want rejected: %q", u)
	}
}

// TestNgrokConfigurerPromptsForURLWithoutAPIKey guards the last-resort fallback:
// with no API key AND no resolvable authtoken, the configurer cannot discover the
// URL and must fall back to a URL prompt (errors in non-interactive mode) rather
// than silently leaving MCP_PUBLIC_URL empty.
func TestNgrokConfigurerPromptsForURLWithoutAPIKey(t *testing.T) {
	// Both the API path and the authtoken path yield nothing -> must prompt.
	orig := tunnel.ResolveNgrokSDKURL
	tunnel.ResolveNgrokSDKURL = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("no account")
	}
	defer func() { tunnel.ResolveNgrokSDKURL = orig }()

	state := &ServiceInstallState{
		Provider:    tunnel.TunnelProviderNgrok,
		TunnelToken: "", // no authtoken at all
		AuthToken:   "auth",
		// No NgrokAPIKey, no NGROK_API_KEY env -> must prompt for the URL.
	}
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	err := ngrokConfigurer(context.Background(), testPrompter{}, state, nil)
	require.Error(t, err,
		"without an ngrok API key or authtoken the configurer must require an interactive URL prompt")
	require.Contains(t, err.Error(), "interactive",
		"with no API key and no discovered domain the configurer must request an interactive prompt for the URL")
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

	// Non-interactive so the URL-resolution API path (which needs no prompt) is
	// the only path exercised; the API key is supplied via the seeded state.
	prior := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = prior }()

	state := &ServiceInstallState{
		EnvFile:     envFile,
		Provider:    tunnel.TunnelProviderNgrok,
		NgrokAPIKey: "api-key", // API key present -> resolve URL from API
		AuthToken:   "auth",
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
		Provider:    tunnel.TunnelProviderNgrok,
		NgrokAPIKey: "api-key",
		AuthToken:   "auth",
		OAuth:       new(true),
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
	require.Equal(t, string(tunnel.TunnelProviderNgrok), env["MCP_TUNNEL_PROVIDER"])
}
