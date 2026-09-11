package mcpembed

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cliWiringPrefixes names every go.lumeweb.com/pinner-cli package that carries
// CLI assembly/wiring. The canonical hosted composition must not grow NEW edges
// into these beyond the documented factory seam below.
const cliWiringPrefix = "go.lumeweb.com/pinner-cli/internal/cli"
const clicatalogPkg = "go.lumeweb.com/pinner-cli/internal/clicatalog"

// cliWiringAllowlist is the closed set of CLI-wiring packages that mcpembed is
// currently permitted to depend on, transitively.
//
// WHY these remain:
//   - internal/cli            — the two hosted composition factories
//     cli.BuildCatalogOpsDepsForHosted (catalogdeps.go) and
//     cli.BuildHostedTransferOptions (server.go). These are thin wrappers over
//     the CLI's full operation-catalog / auth / pinning / upload / download
//     service-factory graph (buildCatalogOpsDeps, defaultUploadServiceFactory,
//     ...). That graph fundamentally lives in internal/cli; relocating it would
//     be a package/module-boundary move, not a rename, so the factory seam
//     stays here rather than forcing a bad abstraction.
//   - internal/cli/internal   — pulled transitively by internal/cli itself.
//   - internal/cli/wizard     — leaf shared step-runner reached transitively
//     via internal/mcp/services (the non-hosted install path). It imports only
//     fieldcraft, so it does not add CLI wiring.
//   - internal/clicatalog     — shared catalog package pulled by internal/cli.
//
// If a NEW CLI-wiring package appears in mcpembed's graph, this test fails to
// flag the coupling so a reviewer must consciously widen the seam.
var cliWiringAllowlist = map[string]bool{
	"go.lumeweb.com/pinner-cli/internal/cli":          true,
	"go.lumeweb.com/pinner-cli/internal/cli/internal": true,
	"go.lumeweb.com/pinner-cli/internal/cli/wizard":   true,
	"go.lumeweb.com/pinner-cli/internal/clicatalog":   true,
}

// isCLIWiring reports whether pkg is part of the CLI wiring graph.
func isCLIWiring(pkg string) bool {
	return pkg == clicatalogPkg || strings.HasPrefix(pkg, cliWiringPrefix)
}

// TestMCPEmbedCLIDependencyBoundary pins mcpembed's coupling to the CLI wiring
// to exactly the documented factory seam. It deliberately shells out to
// `go list -deps` (a structural package-graph check) rather than scanning
// source, so it also catches indirect edges introduced anywhere in the tree.
func TestMCPEmbedCLIDependencyBoundary(t *testing.T) {
	deps := goListDeps(t, "./...")

	var cliDeps []string
	for _, pkg := range deps {
		if isCLIWiring(pkg) {
			cliDeps = append(cliDeps, pkg)
		}
	}

	for _, pkg := range cliDeps {
		require.Truef(t, cliWiringAllowlist[pkg],
			"mcpembed gained an unapproved CLI-wiring dependency %q; "+
				"mcpembed must couple to internal/cli ONLY through the two "+
				"documented hosted composition factories (BuildCatalogOpsDepsForHosted "+
				"and BuildHostedTransferOptions). Widen mcpembed/import_boundary_test.go "+
				"deliberately, or the step-4 seam has grown.", pkg)
	}

	// The two factories are still the sanctioned entry points; the seam must
	// not disappear silently either (a future hero that claims to have removed
	// the coupling while only hiding it under a new package would break this).
	require.Contains(t, cliDeps, "go.lumeweb.com/pinner-cli/internal/cli",
		"expected the documented BuildCatalogOpsDepsForHosted / "+
			"BuildHostedTransferOptions factory seam to remain on internal/cli")
}

// goListDeps runs `go list -deps <args>` in the test package's directory and
// returns the import paths (one per line), skipping blank lines.
func goListDeps(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list", "-deps"}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go list -deps failed:\n%s", out)

	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pkgs = append(pkgs, line)
		}
	}
	return pkgs
}
