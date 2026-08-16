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
