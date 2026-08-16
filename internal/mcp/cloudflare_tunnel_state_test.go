package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBareHostname covers the hostname normalization used when comparing
// --domain against the provisioned hostname and when emitting cloudflared
// ingress hosts / readiness URLs, so an https:// prefix or trailing fragment
// on either side cannot cause a false mismatch or an invalid config.
func TestBareHostname(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"mcp.example.com", "mcp.example.com"},
		{"https://mcp.example.com", "mcp.example.com"},
		{"HTTP://mcp.example.com", "mcp.example.com"},
		{"http://mcp.example.com/", "mcp.example.com"},
		{"https://mcp.example.com/path#frag", "mcp.example.com"},
		{"  https://mcp.example.com  ", "mcp.example.com"},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, bareHostname(tc.in), "bareHostname(%q)", tc.in)
	}
}

// TestConfigYAMLRejectsMaliciousHostname verifies the YAML-injection guard:
// a hostname containing whitespace/control characters or YAML-significant
// characters is rejected rather than emitted raw into the cloudflared
// config.yml (which could otherwise inject extra ingress rules).
func TestConfigYAMLRejectsMaliciousHostname(t *testing.T) {
	state := &CloudflareTunnelState{
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: "s3cr3t", Token: "t", Hostname: "mcp.example.com\n  - service: http://evil",
	}
	_, err := state.configYAML("localhost:8080")
	require.Error(t, err, "expected a hostname with newlines to be rejected")
	require.Contains(t, err.Error(), "invalid tunnel hostname")
}

// TestConfigYAMLQuotesValues verifies the generated config.yml quotes every
// interpolated value so a safe hostname round-trips as a valid YAML string and
// the output does not break the document structure.
func TestConfigYAMLQuotesValues(t *testing.T) {
	state := &CloudflareTunnelState{
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: "s3cr3t", Token: "t", Hostname: "mcp.example.com",
	}
	out, err := state.configYAML("localhost:8080")
	require.NoError(t, err)
	s := string(out)
	require.Contains(t, s, `hostname: "mcp.example.com"`)
	require.Contains(t, s, `service: "localhost:8080"`)
	require.Contains(t, s, `tunnel: "tun-1"`)
	// A valid bare hostname must not be rejected by the validator.
	require.True(t, validIngressHostname("mcp.example.com"))
	require.False(t, validIngressHostname("mcp.example.com\n"))
	require.False(t, validIngressHostname("evil:9080"))
	require.False(t, validIngressHostname(""))
}
