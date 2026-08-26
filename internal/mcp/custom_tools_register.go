package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// customToolSpec is a declarative spec describing how one custom/direct tool
// participates in the registration pipeline. Each spec declares which fixed
// phases it joins; the pipeline guarantees the global phase order
// (index → app → curated → surface) regardless of the order specs were
// declared, so wiring a tool can never race its own dependencies (e.g. an app
// view that must attach _meta.ui to a launcher that is not catalog-indexed
// yet).
type customToolSpec struct {
	// desc is the authoritative descriptor for the tool.
	desc model.ToolDescriptor

	// index adds the tool to the ToolCatalog so it is discoverable through
	// search_tools/describe_tool/invoke_tool. App views resolve their
	// launchers against the catalog, so index always precedes app install.
	index bool

	// direct registers the tool on tools/list via RegisterOfficialDescriptor.
	// Tools surfaced only through a DirectVisible catalog entry (SSO, resume,
	// curated ops, dev tools) leave this false and let the curated phase
	// project them.
	direct bool

	// app, when non-nil, installs the tool's MCP App view after every catalog
	// entry is indexed. It attaches _meta.ui to the launcher entry in the
	// catalog and registers the ui:// resource + app-only helpers. The catalog
	// satisfies apps.AppCatalog (the view layer's narrower interface).
	app func(srv *sdk.Server, catalog apps.AppCatalog) error
}

// customToolRegistry runs the fixed multi-phase registration pipeline for the
// custom/direct tool surface. It separates gathering (declare specs) from the
// ordered phases that actually register, so ordering dependencies are
// properties of the pipeline rather than of call order.
type customToolRegistry struct {
	srv     *sdk.Server
	catalog *ToolCatalog
	specs   []*customToolSpec

	// postSurface runs after the direct tool surface is projected (resources,
	// prompts).
	postSurface []func() error
}

// newCustomToolRegistry creates a registry bound to the server and catalog.
func newCustomToolRegistry(srv *sdk.Server, catalog *ToolCatalog) *customToolRegistry {
	return &customToolRegistry{srv: srv, catalog: catalog}
}

// add declares a spec for a custom tool. Callers that need a spec to be
// conditional (e.g. only register upload_file when a byte source is wired)
// gate on the condition before calling add.
func (r *customToolRegistry) add(s customToolSpec) *customToolRegistry {
	spec := s
	r.specs = append(r.specs, &spec)
	return r
}

// afterSurface queues a registration that must run once the direct tool
// surface is projected (resources, prompts).
func (r *customToolRegistry) afterSurface(fn func() error) *customToolRegistry {
	r.postSurface = append(r.postSurface, fn)
	return r
}

// run executes the fixed pipeline phases in order:
//
//  1. index   — add every catalog-role spec so app wiring can resolve launchers
//  2. app     — install app views (requires all catalog entries present)
//  3. curated — stamp the compiled curated surface and project it on tools/list
//  4. surface — project directly-registered tools on tools/list
//  5. post    — resources, prompts
func (r *customToolRegistry) run() error {
	for _, s := range r.specs {
		if s.index {
			r.catalog.Add(model.ToolEntryFromDescriptor(s.desc))
		}
	}

	for _, s := range r.specs {
		if s.app != nil {
			if err := s.app(r.srv, r.catalog); err != nil {
				return err
			}
		}
	}

	markCurated(r.catalog)
	if err := RegisterOfficialCuratedTools(r.srv, r.catalog); err != nil {
		return err
	}

	for _, s := range r.specs {
		if s.direct {
			if err := RegisterOfficialDescriptor(r.srv, s.desc); err != nil {
				return err
			}
		}
	}

	for _, fn := range r.postSurface {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// launcherSpec builds the spec for an open_* UI launcher: it is catalog
// indexed (so the app view's AttachTo can resolve it), directly registered on
// tools/list, and carries the app install that attaches _meta.ui and registers
// the ui:// resource + helpers. The descriptor must carry _meta.ui (see
// registerOpenLauncher's guard).
func launcherSpec(desc model.ToolDescriptor, app func(srv *sdk.Server, catalog apps.AppCatalog) error) customToolSpec {
	return customToolSpec{desc: desc, index: true, direct: true, app: app}
}
