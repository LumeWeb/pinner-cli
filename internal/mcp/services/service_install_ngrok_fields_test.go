package services

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// ngrokConfigIsolated points the ngrok config-file source at a path that does
// not exist, so a developer's real ~/.config/ngrok/ngrok.yml (a valid
// authtoken outranks the config-manager store in the resolution chain) cannot
// leak into a test that asserts on the config-manager store. Mirrors the
// NGROK_CONFIG escape hatch the production code honors.
func ngrokConfigIsolated(t *testing.T) {
	t.Helper()
	t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "ngrok.yml"))
}

// TestNgrokFieldsShape guards the migration of the ngrok provider to the shared
// field-resolution primitive: the authtoken (masked) and public URL fields with
// a Derived hook that resolves them instead of imperative prompting.
func TestNgrokFieldsShape(t *testing.T) {
	fields := ngrokFields(context.Background(), nil)
	require.Len(t, fields, 2)

	byName := map[string]*fieldform.Field[*ServiceInstallState, string]{}
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}

	token := byName["TunnelToken"]
	require.NotNil(t, token)
	require.Equal(t, "*", token.Prompt.Mask, "token prompt must be masked")
	require.NotNil(t, token.Derived)

	url := byName["PublicURL"]
	require.NotNil(t, url)
	require.NotNil(t, url.Prompt)
	require.NotNil(t, url.Derived)
}

// TestNgrokTokenDerivesFromConfigManager guards that the token Derived hook
// pre-resolves from the config-manager last-resort store (written by a prior
// install) so a re-run never re-prompts for a token it already knows.
func TestNgrokTokenDerivesFromConfigManager(t *testing.T) {
	ngrokConfigIsolated(t)
	cfgMgr := newTestConfigMgr(t)
	tunnel.PersistTunnelCredential(cfgMgr, "ngrok", "token", "cfg-manager-token")

	fields := ngrokFields(context.Background(), cfgMgr)
	token := fields[0]
	require.Equal(t, "TunnelToken", token.Name)

	v, ok := token.Derived(&ServiceInstallState{})
	require.True(t, ok)
	require.Equal(t, "cfg-manager-token", v, "token derives from the config-manager store")
}

// TestNgrokTokenDerivesNothingWhenAbsent guards that an ngrok install with no
// pre-existing token derives nothing and falls through to the prompt.
func TestNgrokTokenDerivesNothingWhenAbsent(t *testing.T) {
	// cfgMgr is fresh (no persisted ngrok token) and the ngrok config file is
	// isolated, so ResolveCredential yields nothing.
	ngrokConfigIsolated(t)
	fields := ngrokFields(context.Background(), newTestConfigMgr(t))
	v, ok := fields[0].Derived(&ServiceInstallState{})
	require.False(t, ok, "no token source -> not derived")
	require.Equal(t, "", v)
}

// TestNgrokPublicURLDerivesOperatorValue guards the operator-supplied URL
// short-circuit: PublicURL derives from s.PublicURL without any API call.
func TestNgrokPublicURLDerivesOperatorValue(t *testing.T) {
	fields := ngrokFields(context.Background(), newTestConfigMgr(t))
	url := fields[1]
	s := &ServiceInstallState{PublicURL: "https://you.ngrok-free.dev"}
	v, ok := url.Derived(s)
	require.True(t, ok)
	require.Equal(t, "https://you.ngrok-free.dev", v)
}

// TestNgrokPublicURLDerivesFromAPI guards the "identify what the user has"
// path: with an ngrok API key present and the account API reporting a free dev
// domain, PublicURL derives it (so the interactive install no longer fails with
// "no MCP_PUBLIC_URL").
func TestNgrokPublicURLDerivesFromAPI(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`))
	})

	fields := ngrokFields(context.Background(), newTestConfigMgr(t))
	s := &ServiceInstallState{TunnelToken: "tun-token", NgrokAPIKey: "api-key"}
	v, ok := fields[1].Derived(s)
	require.True(t, ok)
	require.Equal(t, "https://you.ngrok-free.dev", v, "public URL derived from the account API")
}

// TestNgrokPublicURLDerivesFromAuthtoken guards the temp-tunnel fallback: with
// no API key but an authtoken present, PublicURL derives the stable free dev
// domain via a short-lived embedded tunnel — no API key required, no manual
// paste.
func TestNgrokPublicURLDerivesFromAuthtoken(t *testing.T) {
	orig := tunnel.ResolveNgrokSDKURL
	tunnel.ResolveNgrokSDKURL = func(_ context.Context, token string) (string, error) {
		require.Equal(t, "tun-token", token)
		return "https://you.ngrok-free.dev", nil
	}
	defer func() { tunnel.ResolveNgrokSDKURL = orig }()

	fields := ngrokFields(context.Background(), newTestConfigMgr(t))
	s := &ServiceInstallState{TunnelToken: "tun-token"}
	v, ok := fields[1].Derived(s)
	require.True(t, ok)
	require.Equal(t, "https://you.ngrok-free.dev", v)
}

// TestNgrokPublicURLRejectsUnstableDerivedURL guards that an ephemeral
// *.ngrok-free.app subdomain (rotates every session) is NOT persisted as the
// public URL — installing a rotating URL writes a dead endpoint.
func TestNgrokPublicURLRejectsUnstableDerivedURL(t *testing.T) {
	orig := tunnel.ResolveNgrokSDKURL
	tunnel.ResolveNgrokSDKURL = func(_ context.Context, token string) (string, error) {
		return "https://ephemeral-xyz.ngrok-free.app", nil
	}
	defer func() { tunnel.ResolveNgrokSDKURL = orig }()

	fields := ngrokFields(context.Background(), newTestConfigMgr(t))
	s := &ServiceInstallState{TunnelToken: "tun-token"}
	v, ok := fields[1].Derived(s)
	require.False(t, ok, "unstable URL must not be derived/persisted")
	require.Equal(t, "", v)
}

// TestNgrokFinalizePersistsToken verifies Finalize writes the resolved
// authtoken to the last-resort config manager. (The API key is deliberately
// not persisted: there is no supported ngrok.api_key config pair.)
func TestNgrokFinalizePersistsToken(t *testing.T) {
	cfgMgr := newTestConfigMgr(t)
	state := &ServiceInstallState{TunnelToken: "tun-token", NgrokAPIKey: "api-key"}
	require.NoError(t, ngrokFinalize(context.Background(), nil, state, cfgMgr))

	require.Equal(t, "tun-token", tunnel.TunnelCfgCredential(cfgMgr, "ngrok", "token")())
}

// TestNgrokFinalizeHeadlessFailsWhenUnresolved guards the fail-fast contract: a
// headless ngrok install whose public URL could not be derived must error at
// Finalize rather than silently writing an incomplete env file (the legacy
// configurer errored at the prompt under non-interactive mode). The token is
// NOT independently required — an API/operator-resolved URL needs no token.
func TestNgrokFinalizeHeadlessFailsWhenUnresolved(t *testing.T) {
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	// Empty token AND empty URL -> error naming the required public URL.
	err := ngrokFinalize(context.Background(), nil, &ServiceInstallState{}, newTestConfigMgr(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "public base URL")

	// A token with no URL is still unresolved -> the URL requirement fires.
	err = ngrokFinalize(context.Background(), nil, &ServiceInstallState{TunnelToken: "tok"}, newTestConfigMgr(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "public base URL")

	// Resolved URL passes even with no token (API-key path needs no token).
	require.NoError(t, ngrokFinalize(context.Background(), nil, &ServiceInstallState{PublicURL: "https://you.ngrok-free.dev"}, newTestConfigMgr(t)))
}

// TestNgrokFinalizeInteractiveAllowsMissing verifies the fail-fast does NOT fire
// on an interactive run (the URL is gathered by the prompt after Gather), so
// interactive installs keep working.
func TestNgrokFinalizeInteractiveAllowsMissing(t *testing.T) {
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = false
	defer func() { fieldform.NonInteractive = prior }()

	require.NoError(t, ngrokFinalize(context.Background(), nil, &ServiceInstallState{}, newTestConfigMgr(t)))
}
