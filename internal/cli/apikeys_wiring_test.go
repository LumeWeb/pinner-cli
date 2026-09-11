package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// apikeys_wiring_test.go pins the api-keys domain command tree
// (internal/clicatalog/shapes_apikeys.go): the APIKeysDomainRoot parent and
// its compiled leaf children must preserve the historical nesting, aliases,
// positional args, flags and destructive gate exactly.

// compiledAPIKeysSubcommand returns the catalog-compiled "api-keys" parent's
// leaf with the given name, or fails the test if it is absent.
func compiledAPIKeysSubcommand(t *testing.T, leafName string) *cli.Command {
	t.Helper()
	cmd := newAccountAPIKeysCommand()
	require.Equal(t, "api-keys", cmd.Name, "api-keys parent name")
	leaf := findCommand(cmd.Commands, leafName)
	require.NotNil(t, leaf, "api-keys should compile a %q subcommand", leafName)
	return leaf
}

func TestNewAPIKeysCommand(t *testing.T) {
	cmd := newAccountAPIKeysCommand()

	assert.Equal(t, "api-keys", cmd.Name)
	assert.Equal(t, []string{"apikey", "api-key"}, cmd.Aliases)
	assert.Equal(t, "Management", cmd.Category)

	// Deterministic declaration order: list, create, delete.
	names := make([]string, len(cmd.Commands))
	for i, sub := range cmd.Commands {
		names[i] = sub.Name
	}
	require.Equal(t, []string{"list", "create", "delete"}, names, "api-keys leaf order/names")
}

func TestAPIKeysListFlags(t *testing.T) {
	leaf := compiledAPIKeysSubcommand(t, "list")
	assert.Equal(t, "list", leaf.Name)
	flagNames := flagNamesOf(leaf)
	assert.Contains(t, flagNames, "search", "api-keys list should compile the --search flag")
}

func TestAPIKeysCreateNameAndPositional(t *testing.T) {
	leaf := compiledAPIKeysSubcommand(t, "create")
	assert.Equal(t, "create", leaf.Name)
	assert.Equal(t, "<name>", leaf.ArgsUsage, "api-keys create keeps the positional <name>")

	flagNames := flagNamesOf(leaf)
	assert.Contains(t, flagNames, "name", "api-keys create should compile the --name flag")
	// The name arg must be relaxed so the positional <name> path
	// (ResolvePositional) can reach the handler before it runs.
	requireFlagNotRequired(t, leaf, "name")
}

func TestAPIKeysDeleteGateAndFlags(t *testing.T) {
	leaf := compiledAPIKeysSubcommand(t, "delete")
	assert.Equal(t, "delete", leaf.Name)
	assert.Equal(t, "<id>", leaf.ArgsUsage, "api-keys delete keeps the positional <id>")

	flagNames := flagNamesOf(leaf)
	// The destructive op gets the compiled --force confirm gate (wired via
	// GatePassthroughForce to the op's confirm arg by the shared adapter).
	assert.Contains(t, flagNames, "force", "api-keys delete should carry --force")
	// id is relaxed so the positional <id> path can reach the handler.
	requireFlagNotRequired(t, leaf, "id")
}

// flagNamesOf returns the primary name of every flag declared on a command.
func flagNamesOf(cmd *cli.Command) []string {
	out := make([]string, 0, len(cmd.Flags))
	for _, f := range cmd.Flags {
		out = append(out, f.Names()[0])
	}
	return out
}
