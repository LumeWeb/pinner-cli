package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// operations_wiring_test.go pins the operations domain command tree
// (internal/clicatalog/shapes_operations.go): the OperationsDomainRoot parent
// and its compiled leaf children must preserve the historical nesting,
// positional args, flags and behavior exactly.

// compiledOperationsSubcommand returns the catalog-compiled "operations"
// parent's leaf with the given name, or fails the test if it is absent.
func compiledOperationsSubcommand(t *testing.T, leafName string) *cli.Command {
	t.Helper()
	cmd := newOperationsCommandCatalog()
	require.Equal(t, "operations", cmd.Name, "operations parent name")
	leaf := findCommand(cmd.Commands, leafName)
	require.NotNil(t, leaf, "operations should compile a %q subcommand", leafName)
	return leaf
}

func TestOperationsCommand_Tree(t *testing.T) {
	cmd := newOperationsCommandCatalog()

	assert.Equal(t, "operations", cmd.Name)
	assert.Equal(t, "Management", cmd.Category)
	assert.Equal(t, "List and inspect account operations", cmd.Usage)

	// Preserved order: get, list (the catalog compiler's cat.Search emits
	// leaves alphabetically; the shape's explicit Order freezes exactly that).
	require.Equal(t, []string{"get", "list"}, commandNames(cmd.Commands),
		"operations leaf order/names")
}

func TestOperationsListLeaf(t *testing.T) {
	leaf := compiledOperationsSubcommand(t, "list")

	assert.Equal(t, "list", leaf.Name)
	// The list leaf is wired: it carries a catalog action adapter (the leaf
	// builder leaves Action nil; the mount attaches behavior).
	require.NotNil(t, leaf.Action, "operations list should have a catalog-wired Action")

	flags := getFlagNames(leaf)
	assert.Contains(t, flags, FlagWatch, "operations list should compile the --watch flag")
	assert.Contains(t, flags, "search", "operations list should compile the --search flag")
	assert.Contains(t, flags, "status", "operations list should compile the --status flag")
	assert.Contains(t, flags, "all", "operations list should compile the --all flag")
}

func TestOperationsGetLeaf_PositionalAndFlags(t *testing.T) {
	leaf := compiledOperationsSubcommand(t, "get")

	assert.Equal(t, "get", leaf.Name)
	assert.Equal(t, "<id>", leaf.ArgsUsage, "operations get keeps the positional <id>")
	require.NotNil(t, leaf.Action, "operations get should have a catalog-wired Action")

	flags := getFlagNames(leaf)
	assert.Contains(t, flags, "id", "operations get should compile the --id flag")
	assert.Contains(t, flags, FlagWatch, "operations get should compile the --watch flag")
	// The id flag must be relaxed so the positional <id> path
	// (ResolvePositional in operationsCatalogConfig) can reach the handler
	// before it runs. id is compiled as an *cli.IntFlag, so assert its
	// Required marker directly (the shared requireFlagNotRequired helper only
	// handles StringFlags).
	var idRelaxed bool
	for _, f := range leaf.Flags {
		if f.Names()[0] != "id" {
			continue
		}
		idf, ok := f.(*cli.IntFlag)
		require.True(t, ok, "--id should be an IntFlag")
		idRelaxed = !idf.Required
	}
	require.True(t, idRelaxed, "--id should not be urfave-required")
}
