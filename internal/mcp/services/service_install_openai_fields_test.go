//go:build !no_tunnel

package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// TestOpenAIFieldsShape guards the reference migration of the OpenAI provider
// to the shared field-resolution primitive: two clean promptable fields with
// the correct switch, env key, prompt, and validate.
func TestOpenAIFieldsShape(t *testing.T) {
	fields := openAIFields()
	require.Len(t, fields, 2)

	byName := map[string]*fieldform.Field[*ServiceInstallState, string]{}
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}

	tunnelID := byName["TunnelID"]
	require.NotNil(t, tunnelID)
	require.Equal(t, "tunnel-id", tunnelID.Flag)
	require.Equal(t, "MCP_TUNNEL_ID", tunnelID.EnvFileKey)
	require.NotNil(t, tunnelID.Prompt)
	validID := "tunnel_0123456789abcdef0123456789abcdef"
	require.True(t, tunnel.OpenAITunnelID.MatchString(validID))
	require.True(t, tunnelID.Validate(validID))

	apiKey := byName["ApiKey"]
	require.NotNil(t, apiKey)
	require.Equal(t, "api-key", apiKey.Flag)
	require.Equal(t, "*", apiKey.Prompt.Mask)
	require.NotNil(t, apiKey.Prompt.CurrentString)
}

// TestOpenAIFieldGatherPrepopulatesFromFlags verifies that a flag-supplied
// TunnelID and ApiKey are resolved by fieldform.Gather (precedence 1) without
// prompting — so a headless openai install with explicit flags never hard-errors
// and never reaches the prompter.
func TestOpenAIFieldGatherPrepopulatesFromFlags(t *testing.T) {
	// Explicit --tunnel-id / --api-key flags settle precedence 1.
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceTunnelIDFlag, "tunnel_0123456789abcdef0123456789abcdef"))
	require.NoError(t, cmd.Set(serviceApiKeyFlag, "sk-openai-key"))

	state := &ServiceInstallState{}
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	src := newServiceInstallValueSource(cmd, "")

	fields := append([]fieldform.Field[*ServiceInstallState, string]{}, openAIFields()...)
	auth := *installFieldByName("AuthToken")
	auth.Prompt = promptText("shared", "*")
	fields = append(fields, auth)

	// Provide the shared auth token via env-file-independent means: set it as a
	// flag so precedence 1 settles it (does not reach the prompter).
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "shared-token"))

	seeded, fullyDecided, err := fieldform.Gather(context.Background(), src, state, fields)
	require.NoError(t, err)
	require.True(t, fullyDecided)
	require.Contains(t, seeded, "tunnel-id")
	require.Equal(t, "tunnel_0123456789abcdef0123456789abcdef", state.TunnelID)
	require.Equal(t, "sk-openai-key", state.ApiKey)
	require.Equal(t, "shared-token", state.AuthToken)
}

// TestOpenAIFinalizePersistsCredentials verifies Finalize writes the supplied
// credentials to the last-resort config manager.
func TestOpenAIFinalizePersistsCredentials(t *testing.T) {
	cfgMgr := newTestConfigMgr(t)

	state := &ServiceInstallState{TunnelID: "tunnel_0123456789abcdef0123456789abcdef", ApiKey: "sk-openai-key"}
	require.NoError(t, openAIFinalize(context.Background(), nil, state, cfgMgr))

	require.Equal(t, "tunnel_0123456789abcdef0123456789abcdef", tunnel.TunnelCfgCredential(cfgMgr, "openai", "tunnel_id")())
	require.Equal(t, "sk-openai-key", tunnel.TunnelCfgCredential(cfgMgr, "openai", "api_key")())
}

// TestOpenAIFieldGatherInvalidIDHardErrors guards that a malformed OpenAI tunnel
// ID on a headless run surfaces as a hard error rather than silently accepting
// it (the field Validate gates precedence 1 too).
func TestOpenAIFieldGatherInvalidIDHardErrors(t *testing.T) {
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceTunnelIDFlag, "not-an-openai-id"))

	state := &ServiceInstallState{}
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	src := newServiceInstallValueSource(cmd, "")
	_, _, err := fieldform.Gather(context.Background(), src, state, openAIFields())
	require.Error(t, err, "invalid tunnel ID must hard-error on headless")
	require.Contains(t, err.Error(), "TunnelID")
}
