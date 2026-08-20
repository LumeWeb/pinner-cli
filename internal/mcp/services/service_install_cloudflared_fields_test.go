package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// TestCloudflaredFieldsShape guards the migration of the cloudflared provider
// to the shared field-resolution primitive: two promptable fields (Domain +
// TunnelName) with the correct switch, env key, prompt, and a Derived hook that
// folds the provisioned named-tunnel state.
func TestCloudflaredFieldsShape(t *testing.T) {
	fields := cloudflaredFields()
	require.Len(t, fields, 2)

	byName := map[string]*fieldform.Field[*ServiceInstallState, string]{}
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}

	domain := byName["Domain"]
	require.NotNil(t, domain)
	require.Equal(t, serviceDomainFlag, domain.Flag)
	require.Equal(t, "MCP_DOMAIN", domain.EnvFileKey)
	require.NotNil(t, domain.Prompt)
	require.NotNil(t, domain.Derived, "Domain must derive from provisioned state")

	name := byName["TunnelName"]
	require.NotNil(t, name)
	require.Equal(t, serviceTunnelNameFlag, name.Flag)
	require.Equal(t, "MCP_TUNNEL_NAME", name.EnvFileKey)
	require.NotNil(t, name.Prompt)
	require.NotNil(t, name.Derived, "TunnelName must derive from provisioned state")
}

// TestCloudflaredFieldsDeriveProvisionedState verifies both Derived hooks fold
// the provisioned named-tunnel state (hostname + tunnel name) into the
// operational value when the operator has not supplied them.
func TestCloudflaredFieldsDeriveProvisionedState(t *testing.T) {
	statePath := writeCloudflareState(t, `{
		"provider":"cloudflared","tunnel_name":"provisioned-named-tunnel",
		"account_id":"acct","tunnel_id":"tun","hostname":"prov.example.com","secret":"not-a-real-cred"}`)
	orig := tunnel.TunnelStatePath
	tunnel.TunnelStatePath = func() (string, error) { return statePath, nil }
	defer func() { tunnel.TunnelStatePath = orig }()

	fields := cloudflaredFields()
	byName := map[string]*fieldform.Field[*ServiceInstallState, string]{}
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}

	s := &ServiceInstallState{}
	v, ok := byName["Domain"].Derived(s)
	require.True(t, ok)
	require.Equal(t, "prov.example.com", v, "hostname derived from provisioned state")

	v, ok = byName["TunnelName"].Derived(s)
	require.True(t, ok)
	require.Equal(t, "provisioned-named-tunnel", v, "tunnel name derived from provisioned state")
}

// TestCloudflaredTunnelNameDefaults guards that TunnelName derives the
// pinner-mcp default when there is no provisioned state, and that Domain
// derives nothing (falls through to the prompt) in that case.
func TestCloudflaredTunnelNameDefaults(t *testing.T) {
	statePath := writeCloudflareState(t, `{
		"provider":"cloudflared","account_id":"acct","tunnel_id":"tun","secret":"not-a-real-cred"}`)
	orig := tunnel.TunnelStatePath
	tunnel.TunnelStatePath = func() (string, error) { return statePath, nil }
	defer func() { tunnel.TunnelStatePath = orig }()

	fields := cloudflaredFields()
	byName := map[string]*fieldform.Field[*ServiceInstallState, string]{}
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}

	s := &ServiceInstallState{}
	v, ok := byName["Domain"].Derived(s)
	require.False(t, ok, "no hostname in state -> nothing to derive")
	require.Equal(t, "", v)

	v, ok = byName["TunnelName"].Derived(s)
	require.True(t, ok)
	require.Equal(t, "pinner-mcp", v, "tunnel name defaults when nothing else resolves")
}

// TestCloudflaredGatherNoPromptWhenDerived verifies a headless Gather over
// cloudflaredFields fully decides from the provisioned state (plus an explicit
// auth token flag) without reaching the prompter — the migrated declarative
// path replacing the imperative configurer.
func TestCloudflaredGatherNoPromptWhenDerived(t *testing.T) {
	statePath := writeCloudflareState(t, `{
		"provider":"cloudflared","tunnel_name":"provisioned-named-tunnel",
		"account_id":"acct","tunnel_id":"tun","hostname":"prov.example.com","secret":"not-a-real-cred"}`)
	origPath := tunnel.TunnelStatePath
	tunnel.TunnelStatePath = func() (string, error) { return statePath, nil }
	defer func() { tunnel.TunnelStatePath = origPath }()

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "shared-token"))

	state := &ServiceInstallState{}
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	src := newServiceInstallValueSource(cmd, "")
	fields := append([]fieldform.Field[*ServiceInstallState, string]{}, cloudflaredFields()...)
	auth := *installFieldByName("AuthToken")
	auth.Prompt = promptText("shared", "*")
	fields = append(fields, auth)

	seeded, fullyDecided, err := fieldform.Gather(context.Background(), src, state, fields)
	require.NoError(t, err)
	require.True(t, fullyDecided)
	// Domain/TunnelName were provider-derived (precedence 0), so they carry no
	// "seeded from <flag>" banner; only the explicit auth flag is seeded.
	require.Equal(t, []string{"auth-token"}, seeded)
	require.Equal(t, "prov.example.com", state.Domain)
	require.Equal(t, "provisioned-named-tunnel", state.TunnelName)
	require.Equal(t, "shared-token", state.AuthToken)
}

func writeCloudflareState(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tunnel-state.json")
	require.NoError(t, os.WriteFile(p, []byte(body+"\n"), 0600))
	return p
}

// TestCloudflaredFinalizeHeadlessFailsWhenDomainUnresolved guards the fail-fast
// contract: a headless cloudflared install with no --domain and no provisioned
// state must error at Finalize rather than silently writing an env file with an
// empty MCP_DOMAIN (the legacy configurer errored at the prompt under
// non-interactive mode).
func TestCloudflaredFinalizeHeadlessFailsWhenDomainUnresolved(t *testing.T) {
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	err := cloudflaredFinalize(context.Background(), nil, &ServiceInstallState{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "domain")

	// A resolved domain passes.
	require.NoError(t, cloudflaredFinalize(context.Background(), nil, &ServiceInstallState{Domain: "mcp.example.com"}, nil))
}

// TestCloudflaredFinalizeInteractiveAllowsMissing verifies the fail-fast does
// NOT fire on an interactive run (Domain is gathered by the prompt).
func TestCloudflaredFinalizeInteractiveAllowsMissing(t *testing.T) {
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = false
	defer func() { fieldform.NonInteractive = prior }()

	require.NoError(t, cloudflaredFinalize(context.Background(), nil, &ServiceInstallState{}, nil))
}
