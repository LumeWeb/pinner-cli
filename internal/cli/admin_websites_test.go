package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdminWebsitesTree asserts the admin websites command compiled from the
// operation catalog exposes block and unblock, matching the MCP
// admin_websites_* tools.
func TestAdminWebsitesTree(t *testing.T) {
	cmd := newAdminWebsitesCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "websites", cmd.Name)

	names := getSubcommandNames(cmd)
	assert.Contains(t, names, "block")
	assert.Contains(t, names, "unblock")
}
