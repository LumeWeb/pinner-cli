package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/pinner/core/workspaces"
)

// catalog_workspaces_wiring.go adapts the workspaces domain operations in
// internal/catalogops to the urfave CLI: it compiles the operations' command
// tree through the shape model in internal/clicatalog/shapes_workspaces.go and
// CompileCommandTree, renders each handler's result through the Output
// formatter, and maps the destructive --force gate onto operation inputs. IO
// and CLI concerns (the destructive force gate and sensitive credential
// rendering) live here, not in catalogops.
//
// Workspaces are deliberately a SEPARATE command tree from websites (an
// isolated runtime, not a domain-to-CID mapping): they mount as the top-level
// `workspaces` group (list/create/get/attach/suspend/resume/access/delete).
// Runtime workspace resolve is NOT exposed as a user operation.

// catalogWorkspacesDeps builds the catalogops.WorkspacesDeps from the live CLI
// wiring. Service construction uses the core factories; all config is read
// lazily per invocation.
func catalogWorkspacesDeps(factory ...ConfigManagerFactory) catalogops.WorkspacesDeps {
	cfgFactory := resolveConfigFactory(factory...)
	return catalogops.WorkspacesDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		Secure: func() bool {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return false
			}
			return GetSecureSetting(nil, cfgMgr)
		},
		ServiceFactory: workspaces.DefaultFactory,
		NewAuthenticated: func(cfgMgr config.Manager, secure bool, token string) (workspaces.Service, error) {
			return workspaces.NewAuthenticated(cfgMgr, token, secure)
		},
		GetAuthToken: func() string {
			cfgMgr, err := cfgFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().AuthToken
		},
	}
}

// workspacesCatalogDepsVar is an indirection so the wiring and the renderer
// can both reach the canonical operation list without rebuilding it repeatedly.
var workspacesCatalogDepsVar = catalogops.WorkspacesDeps(catalogWorkspacesDeps())

// newWorkspacesCatalogCommands compiles the workspaces catalog operations and
// returns the top-level "workspaces" subcommands they produce (list, create,
// get, attach, suspend, resume, access, delete). It declares the operations'
// command shape in internal/clicatalog/shapes_workspaces.go and materializes
// the tree through mount-owned leaf and parent builders.
func newWorkspacesCatalogCommands() []*cli.Command {
	root := clicatalog.WorkspacesDomainRoot
	ops := catalogops.WorkspacesOperations(workspacesCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.WorkspacesShapes,
		root,
		workspacesCatalogConfig(),
		buildWorkspacesLeaf,
		buildCLIParent,
	)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the workspaces group.
		panic(fmt.Sprintf("catalog compile workspaces: %v", err))
	}
	return cmds
}

// buildWorkspacesLeaf is the mount-owned leaf builder materializing one
// workspaces leaf into an urfave *cli.Command. Shape
// (name/category/aliases/flags/usage) comes from the clicatalog model via
// NewCLILeaf; behavior (the catalog action adapter) stays mount-owned here.
func buildWorkspacesLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// workspacesCatalogConfig returns the CatalogAdapterConfig that expresses the
// workspaces domain's exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline: the result renderer, the per-invocation
// --auth-token override, and the destructive --force gate (workspaces delete
// only). All remaining fields stay nil so the shared pipeline's safe defaults
// apply.
func workspacesCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer:               renderWorkspacesResult,
		HonorAuthTokenOverride: true,

		// Destructive gate (workspaces delete). The shared pipeline enforces
		// --force/--confirm; with a target workspace and no --force, refuse
		// loudly (non-zero exit) with the "workspaces delete:" message. With
		// no target (no "id" arg), fall through so the handler's required-arg
		// validation produces a non-zero exit.
		DestructiveGate: GateForceReject(
			func(ic *CatalogInvokeContext) bool {
				return opmesh.StrArg(ic.Input, "id", "") != ""
			},
			func(ic *CatalogInvokeContext) string {
				return "workspaces delete: pass --force to confirm this destructive operation"
			},
		),
	}
}

// renderWorkspacesResult is the catalog.RenderFunc that renders a workspaces
// handler's typed result through the CLI Output formatter. It is the single
// rendering home for catalog-driven workspaces commands.
func renderWorkspacesResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
	output := setupOutput(c)

	// Guard against a typed-nil single-object result (interface non-nil,
	// underlying POINTER nil): a handler returning (nil, nil) yields a typed
	// nil here, and the pointer branches below would dereference it and panic.
	if result != nil && isNilPointerResult(result) {
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

	case *ipfs.WorkspaceResponse:
		// workspaces get/create/attach/suspend/resume all return a workspace.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		renderWorkspaceHuman(output, r)
		return nil

	case *ipfs.WorkspaceAccessResponse:
		// workspaces access. This carries SENSITIVE proxy Basic Auth
		// credentials. Human output is rendered deliberately as labeled
		// fields (never buried in a shared table/list). JSON output is the
		// caller's explicit choice and prints the raw document.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		renderWorkspaceAccessHuman(output, r)
		return nil

	case *catalogops.WorkspaceDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"success": true, "id": r.ID, "status": r.Status})
		}
		output.Printfln("Workspace %s deleted successfully", r.ID)
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// renderWorkspaceHuman renders the fields of a single workspace (used by get,
// create, attach, suspend, resume).
func renderWorkspaceHuman(output Output, w *ipfs.WorkspaceResponse) {
	output.Printfln("Workspace Details")

	fields := []Field{
		{"ID", fmt.Sprintf("%d", w.Id)},
		{"Domain", w.Domain},
		{"Label", w.Label},
		{"Status", w.Status},
	}
	if w.WebsiteId != nil {
		fields = append(fields, Field{"Website ID", fmt.Sprintf("%d", *w.WebsiteId)})
	} else {
		fields = append(fields, Field{"Website ID", "-"})
	}
	if w.Error != nil && *w.Error != "" {
		fields = append(fields, Field{"Error", *w.Error})
	}
	fields = append(fields,
		Field{"Created", w.Created.Format("2006-01-02 15:04:05")},
		Field{"Updated", w.Updated.Format("2006-01-02 15:04:05")},
	)

	output.PrintFields(FieldGroup{Fields: fields})
}

// renderWorkspaceAccessHuman renders the workspace proxy access credentials.
// These are SENSITIVE; the username/password are deliberately rendered as
// labeled fields on their own (the op is HumanOnly at the catalog layer, and
// a human explicitly invoking `workspaces access` is shown the credential
// deliberately rather than folded into a shared table/list).
func renderWorkspaceAccessHuman(output Output, r *ipfs.WorkspaceAccessResponse) {
	output.Printfln("Workspace Proxy Access Credentials")
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Username", r.Username},
			{"Password", r.Password},
		},
	})
}
