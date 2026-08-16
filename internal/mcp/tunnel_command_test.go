package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTunnelCommandHasSubcommands ensures `pinner mcp tunnel` exposes the
// install and status subcommands.
func TestTunnelCommandHasSubcommands(t *testing.T) {
	cmd := tunnelCommand()
	require.Equal(t, "tunnel", cmd.Name)
	names := make([]string, 0, len(cmd.Commands))
	for _, c := range cmd.Commands {
		names = append(names, c.Name)
	}
	require.Contains(t, names, "install")
	require.Contains(t, names, "status")
}
