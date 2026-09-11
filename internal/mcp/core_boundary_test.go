package mcp

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cliWiringPrefix is the import-path prefix of every CLI assembly package.
// The canonical MCP implementation under internal/mcp must stay CLI-free so a
// hosted product can adopt it as an independent assembly.
// internal/cli already depends on internal/mcp; internal/mcp must never
// depend back on internal/cli.
const cliWiringPrefix = "go.lumeweb.com/pinner-cli/internal/cli"

// cliWiringLeafException is the single, documented exception: the
// shared step-runner at internal/cli/wizard, referenced by
// internal/mcp/services/service_install_wizard.go for the non-hosted service
// install path. It is a leaf package (imports only go.lumeweb.com/fieldcraft),
// so it carries no CLI wiring indirection. This allow-list keeps it the ONLY
// internal/cli edge in the canonical implementation.
var cliWiringLeafException = map[string]bool{
	"go.lumeweb.com/pinner-cli/internal/cli/wizard": true,
}

// TestCoreMCPDoesNotDependOnCLI enforces the import-graph invariant that the
// canonical implementation (internal/mcp/...) never reaches into the CLI
// wiring. It is a structural `go list -deps` check over the whole internal/mcp
// tree, so any NEW internal/cli package referenced by the core (directly or
// transitively) fails the test for review.
func TestCoreMCPDoesNotDependOnCLI(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	require.NoError(t, err, "go list -deps ./... failed:\n%s", out)

	for _, line := range strings.Split(string(out), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" || !strings.HasPrefix(pkg, cliWiringPrefix) {
			continue
		}
		require.Truef(t, cliWiringLeafException[pkg],
			"internal/mcp/... must not depend on CLI wiring package %q; "+
				"the canonical MCP implementation must stay independent of "+
				"internal/cli so a hosted product can assemble it on its own. "+
				"Only the documented leaf internal/cli/wizard exception is allowed.",
			pkg)
	}
}
