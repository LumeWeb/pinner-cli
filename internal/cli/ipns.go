package cli

import (
	"github.com/urfave/cli/v3"
)

func newIPNSCommand() *cli.Command {
	// The ipns parent is catalog-driven: the keys + publish/republish/resolve
	// subcommands are compiled from the canonical operation catalog
	// (internal/catalogops) — see ipns_wiring.go.
	return newIPNSCommandCatalog()
}
