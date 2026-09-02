package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdminSocialProvidersTree asserts the social-providers command compiled
// from the operation catalog exposes the seven subcommands (list, get, create,
// update, delete, enable, disable), matching the MCP admin_social_providers_*
// tools.
func TestAdminSocialProvidersTree(t *testing.T) {
	cmd := newAdminSocialProvidersCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "social-providers", cmd.Name)

	names := getSubcommandNames(cmd)
	for _, want := range []string{"list", "get", "create", "update", "delete", "enable", "disable"} {
		assert.Contains(t, names, want)
	}
}

// TestAdminSocialProvidersMountedUnderAdmin asserts the social-providers
// section is mounted on the admin parent command so the CLI tree and the
// catalog agree.
func TestAdminSocialProvidersMountedUnderAdmin(t *testing.T) {
	admin := newAdminCommand()
	names := map[string]bool{}
	for _, sub := range admin.Commands {
		names[sub.Name] = true
	}
	for _, want := range []string{"quota", "billing", "websites", "pprof", "platform-domains", "social-providers"} {
		if !names[want] {
			t.Fatalf("admin parent missing expected section %q", want)
		}
	}
}
