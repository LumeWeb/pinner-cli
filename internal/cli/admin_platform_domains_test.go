package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdminPlatformDomainsTree asserts the platform-domains command compiled
// from the operation catalog exposes the five subcommands (list, register,
// update, delete, bind), matching the MCP admin_platform_domains_* tools.
func TestAdminPlatformDomainsTree(t *testing.T) {
	cmd := newAdminPlatformDomainsCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "platform-domains", cmd.Name)

	names := getSubcommandNames(cmd)
	for _, want := range []string{"list", "register", "update", "delete", "bind"} {
		assert.Contains(t, names, want)
	}
}
