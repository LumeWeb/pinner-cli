//go:build !no_tunnel

package tunnel

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureTunnelState returns a CloudflareTunnelState populated with clearly
// fixture (non-real) values so the embedded wiring can be unit-tested without
// any Cloudflare account or network access. All credential strings are
// obviously synthetic.
func fixtureTunnelState() *CloudflareTunnelState {
	return &CloudflareTunnelState{
		Provider:   TunnelProviderCloudflared,
		AccountID:  "acct-0123456789abcdef0123456789abcdef",
		TunnelID:   "01234567-89ab-4def-8123-456789abcdef",
		TunnelName: "pinner-fixture",
		Secret:     "c2VjcmV0LWZpY3Rpb24tbm90LXJlYWw=", // base64 of a fixture string
		Token:      "tkn-fixture",
		Hostname:   "mcp.example.com",
	}
}

func TestBuildCloudflaredTunnelPropertiesNamed(t *testing.T) {
	props, err := buildCloudflaredTunnelProperties(fixtureTunnelState())
	require.NoError(t, err)

	// A named tunnel, not a quick tunnel: QuickTunnelUrl must stay empty and
	// the credential fields must map from the persisted state. state.Secret is
	// stored base64 and must be DECODED to the raw bytes cloudflared uses for
	// the edge-registration HMAC.
	state := fixtureTunnelState()
	rawSecret, _ := base64.StdEncoding.DecodeString(state.Secret)
	assert.Empty(t, props.QuickTunnelUrl)
	assert.Equal(t, state.AccountID, props.Credentials.AccountTag)
	assert.Equal(t, state.TunnelID, props.Credentials.TunnelID.String())
	assert.Equal(t, rawSecret, props.Credentials.TunnelSecret)
	assert.Empty(t, props.Credentials.Endpoint)
}

func TestBuildCloudflaredTunnelPropertiesInvalidTunnelID(t *testing.T) {
	state := fixtureTunnelState()
	state.TunnelID = "not-a-uuid"
	_, err := buildCloudflaredTunnelProperties(state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid UUID")
}

func TestBuildCloudflaredTunnelPropertiesInvalidSecret(t *testing.T) {
	state := fixtureTunnelState()
	state.Secret = "!!!not-base64!!!"
	_, err := buildCloudflaredTunnelProperties(state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid base64")
}

func TestBuildCloudflaredTunnelPropertiesNilState(t *testing.T) {
	_, err := buildCloudflaredTunnelProperties(nil)
	require.Error(t, err)
}

func TestBuildCloudflaredIngress(t *testing.T) {
	// The provisioned hostname must route to the local origin, with a 404
	// catch-all as the terminal rule.
	ing, err := buildCloudflaredIngress("mcp.example.com", "http://127.0.0.1:8893")
	require.NoError(t, err)

	rules := ing.Rules
	require.GreaterOrEqual(t, len(rules), 2)
	assert.Equal(t, "mcp.example.com", rules[0].Hostname)
	assert.Equal(t, "http://127.0.0.1:8893", rules[0].Service.String())

	// The specific hostname rule must match the provisioned hostname.
	assert.True(t, rules[0].Matches("mcp.example.com", ""))

	// The last rule is the terminal catch-all (empty hostname, http_status).
	last := rules[len(rules)-1]
	assert.Empty(t, last.Hostname)
	assert.Equal(t, "http_status:404", last.Service.String())
	assert.True(t, last.Matches("any.other.example", ""))
}
