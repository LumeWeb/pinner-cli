package clicatalog

// shapes_workspaces.go declares the declarative command shape for the
// workspaces catalog domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, emitted by
// catalogops.WorkspacesOperations in a stable declaration order):
//
//	workspaces_list/create/get/attach/suspend/resume/access/delete
//	                          -> {"workspaces",<leaf>}   (flat leaves)
//
// Workspaces are a SEPARATE frontend/backend concept from websites (an
// isolated runtime, not a domain-to-CID mapping), so they mount as their own
// top-level `workspaces` command tree rather than nesting under `websites`.
//
// Name mapping:
//
//	- The top-level workspaces_list renders as the canonical "list" leaf with
//	  the "ls" alias (ideal naming rule: list canonical, ls a muscle-memory
//	  alias), matching the websites/pins/domains leaves below.
//	- workspaces_delete keeps the "delete" leaf (matching websites_delete; the
//	  operation ID is workspaces_delete and "delete" is a full verb).
//	- The `workspaces access` leaf is gated HumanOnly at the op level; it is a
//	  deliberate, discoverable user operation (access credentials are
//	  sensitive and not surfaced to model actors), so it stays in this tree.
//	- Runtime workspace resolve (workspaces_resolve) is deliberately NOT
//	  shipped: it is a runtime-internal concern (Coolify-injected resource
//	  UUID) and is not a normal user CLI/MCP operation.
//
// Order is left at 0 everywhere so tied siblings keep the registry-declaration
// (== WorkspacesOperations emission) order, per the model's deterministic
// (Order, declaration-order) sort.

// WorkspacesShapes is the ShapeRegistry for the workspaces domain, keyed by
// the all-underscore canonical op Name.
var WorkspacesShapes = ShapeRegistry{
	"workspaces_list": {
		Path:    []string{"workspaces", "list"},
		Aliases: []string{"ls"},
	},
	"workspaces_create":  {Path: []string{"workspaces", "create"}},
	"workspaces_get":     {Path: []string{"workspaces", "get"}},
	"workspaces_attach":  {Path: []string{"workspaces", "attach"}},
	"workspaces_suspend": {Path: []string{"workspaces", "suspend"}},
	"workspaces_resume":  {Path: []string{"workspaces", "resume"}},
	"workspaces_access":  {Path: []string{"workspaces", "access"}},
	"workspaces_delete":  {Path: []string{"workspaces", "delete"}},
}

// WorkspacesDomainRoot is the DomainRoot declaration for the workspaces
// domain. The root command itself is constructed by the consuming CLI mount;
// the transform returns its ordered children (flat leaves).
var WorkspacesDomainRoot = DomainRoot{
	Name:     "workspaces",
	Category: "Management",
	Usage:    "Manage workspaces",
	Desc:     `Manage isolated workspaces: create/list/get, attach a workspace to a website you own (the publish link), suspend/resume its runtime, fetch proxy access credentials, and delete a workspace. Workspaces are a separate concept from websites (an isolated runtime with its own portal API key and proxy endpoint) — to map domains to CIDs use the 'websites' command tree instead. These subcommands are compiled from the canonical operation catalog (internal/catalogops).`,
}
