package cli

import "github.com/urfave/cli/v3"

// newWorkspacesCommand mounts the workspaces domain as a top-level command
// tree. The parent is catalog-driven: the core workspace lifecycle
// subcommands (list, create, get, attach, suspend, resume, access, delete) are
// compiled from the canonical operation catalog (internal/catalogops) — see
// catalog_workspaces_wiring.go.
//
// Workspaces are deliberately a SEPARATE concept from websites (an isolated
// runtime with its own portal API key and proxy endpoint, not a
// domain-to-CID mapping), so they mount as their own `workspaces` tree rather
// than nesting under `websites`. To map domains to CIDs use the 'websites'
// command tree instead.
func newWorkspacesCommand() *cli.Command {
	cmds := newWorkspacesCatalogCommands()

	return &cli.Command{
		Name:     "workspaces",
		Category: "Management",
		Usage:    "Manage workspaces",
		Description: `Manage isolated workspaces: an isolated runtime with its own portal API key and proxy endpoint. Covers create/list/get, attach to a website you own (the publish link), suspend/resume its runtime, fetch proxy access credentials (access), and delete.

Workspaces are a separate concept from websites. To associate domain names with CIDs so your IPFS/IPNS content is served over custom domains, use the 'websites' command tree instead. A website association is only an optional link on a workspace (attach), not what a workspace is for.`,
		Commands: cmds,
	}
}
