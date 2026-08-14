package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

// TestIPNSKeysDeleteConfirmFlagDefaultsTrue guards the CLI delete-without-force
// contract against the handler-side confirm gate. ipnsActionAdapter reads
// c.Bool("confirm") from the compiled --confirm flag and then runs the input
// through NormalizeOperationInput before Execute; the gate passes only if the
// flag (and normalized input) resolve confirm=true when the user does not pass
// --confirm. This asserts the compiled flag default is true, so a plain
// `ipns keys delete <id>` still deletes.
func TestIPNSKeysDeleteConfirmFlagDefaultsTrue(t *testing.T) {
	cmd := newIPNSCommandCatalog()
	keys := cmd.Command("keys")
	if keys == nil {
		t.Fatal("ipns keys parent command not found")
	}
	del := keys.Command("delete")
	if del == nil {
		t.Fatal("ipns keys delete command not found")
	}

	var found *cli.BoolFlag
	for _, f := range del.Flags {
		if bf, ok := f.(*cli.BoolFlag); ok && bf.Name == "confirm" {
			found = bf
			break
		}
	}
	if found == nil {
		t.Fatal("ipns keys delete must declare a --confirm BoolFlag")
	}
	assert.True(t, found.Value, "ipns keys delete --confirm must default to true so a confirm-less CLI delete satisfies the handler gate")
}
