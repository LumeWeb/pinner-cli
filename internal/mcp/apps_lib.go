package mcp

import (
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// This file is the light lib layer for authoring ui:// MCP Apps. It collapses
// the three-step manual wiring every app used to repeat — attach _meta.ui to
// the model tool(s), register the HTML resource, register app-only helper
// tools — into one declarative call, so an app is a single AppView value
// instead of a hand-written RegisterXxxApp function that spawns raw
// RegisterAppTool / RegisterAppResource plumbing per app.
//
// It sits on top of the SDK-neutral seam in mcpapps.go (the typed model.AppToolMeta /
// AppResource / RegisterAppTool / RegisterAppResource primitives). Add a new
// app by filling in an AppView and calling RegisterAppView — no direct
// _meta.ui, resources/list, or CSP manipulation.

// AppView declares one ui:// MCP App. A view is an HTML document served at a
// ui:// URI, attached to the existing model-visible tool(s) it renders for,
// plus any app-only helper tools the view calls. RegisterAppView wires all of
// it at once.
type AppView struct {
	// URI is the ui:// resource URI, e.g. "ui://auth/sso.html".
	URI string
	// Name is a stable, unique resource slug (shown to hosts in resources/list).
	Name string
	// Title is the human-facing view title.
	Title string
	// Description is the resource description surfaced in resources/list.
	Description string
	// HTML is the complete, self-contained mcp-app document served at URI.
	// Build it with mcpapp.RenderMcpAppDoc(title, body, module) so it shares the app
	// shell/theme/bootstrap. Served verbatim; the sandboxed iframe needs no
	// network request.
	HTML string
	// PrefersBorder hints hosts to render the iframe with a border.
	PrefersBorder bool

	// AttachTo lists existing catalog tool names whose _meta.ui should point at
	// URI. These are the model-visible tools the view renders for. Each must
	// already exist in the catalog; missing names are an error.
	AttachTo []string

	// Helpers are app-only tools the view calls via callServerTool. They are
	// registered with model.ToolVisibilityApp so a UI-capable host exposes them to the
	// iframe while the model never sees them in text-form hosts or the model
	// surface.
	Helpers []model.ToolDescriptor
}

// AppViewInfo is the minimal companion-app description the server emits into a
// model-visible needs_human result so a text-only host can still tell the user
// an interactive MCP App exists alongside the raw URL/handle flow.
type AppViewInfo struct {
	// URI is the ui:// resource URI, e.g. "ui://auth/sso.html".
	URI string
	// Name is the stable resource slug, e.g. "auth-sso".
	Name string
	// Title is the human-facing view title, e.g. "Sign In".
	Title string
}

// appViewsByTool maps a model-visible tool name to its attached MCP App view.
// Populated by RegisterAppView from AppView.AttachTo. It is the server's own
// record of "this tool renders an app", used to annotate needs_human results
// with the companion-app context. Guarded by appViewsMu because app
// registration is additive but may run alongside handlers in tests.
var (
	appViewsMu     sync.RWMutex
	appViewsByTool = map[string]AppViewInfo{}
)

// appInfoForTool returns the companion-app info registered for toolName, or the
// zero value if the tool has no attached app.
func appInfoForTool(toolName string) (AppViewInfo, bool) {
	appViewsMu.RLock()
	defer appViewsMu.RUnlock()
	info, ok := appViewsByTool[toolName]
	return info, ok
}

// RegisterAppView wires a complete ui:// MCP App in one call:
//
//   - attaches _meta.ui (resource URI) to every model tool named in AttachTo,
//   - registers the HTML document as a ui:// resource at URI,
//   - registers each Helper as an app-only tool bound to URI.
//
// Returns an error if srv/catalog are nil, URI/Name/Title are empty, any
// AttachTo tool is missing from the catalog, or a helper registers with an
// empty URI. App wiring is additive: existing tools and their plain-host text
// results are preserved, and a tool's existing _meta is extended, never
// replaced.
func RegisterAppView(srv *sdk.Server, catalog *ToolCatalog, v AppView) error {
	if srv == nil {
		return fmt.Errorf("mcp: nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("mcp: nil tool catalog")
	}
	if v.URI == "" {
		return fmt.Errorf("mcp: app view requires a uri")
	}
	if v.Name == "" {
		return fmt.Errorf("mcp: app view %q requires a name", v.URI)
	}
	if v.HTML == "" {
		return fmt.Errorf("mcp: app view %q requires html", v.URI)
	}

	info := AppViewInfo{URI: v.URI, Name: v.Name, Title: v.Title}
	for _, toolName := range v.AttachTo {
		if err := attachAppMeta(catalog, toolName, v.URI); err != nil {
			return err
		}
		appViewsMu.Lock()
		appViewsByTool[toolName] = info
		appViewsMu.Unlock()
	}

	if err := sdk.RegisterAppResource(srv, sdk.AppResource{
		URI:         v.URI,
		Name:        v.Name,
		Title:       v.Title,
		Description: v.Description,
		Meta: model.AppResourceMeta{
			PrefersBorder: boolPtr(v.PrefersBorder),
		},
		HTML: v.HTML,
	}); err != nil {
		return err
	}

	for _, h := range v.Helpers {
		if err := sdk.RegisterAppTool(srv, h, model.AppToolMeta{
			ResourceURI: v.URI,
			Visibility:  []model.ToolVisibility{model.ToolVisibilityApp},
		}); err != nil {
			return err
		}
	}

	return nil
}

// attachAppMeta attaches the ui:// resource URI to an existing catalog tool's
// _meta.ui (plus the legacy flat key), extending rather than replacing any
// existing metadata. The named tool must already be in the catalog.
func attachAppMeta(catalog *ToolCatalog, toolName, resourceURI string) error {
	entry, ok := catalog.Get(toolName)
	if !ok {
		return fmt.Errorf("mcp: app view tool %q not in catalog", toolName)
	}
	meta, err := sdk.MarshalToolMeta(model.AppToolMeta{ResourceURI: resourceURI})
	if err != nil {
		return err
	}
	if entry.Meta == nil {
		entry.Meta = map[string]any{}
	}
	for k, v := range meta {
		entry.Meta[k] = v
	}
	return nil
}
