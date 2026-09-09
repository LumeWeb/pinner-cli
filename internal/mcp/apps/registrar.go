package apps

// De-fork seam (Stage 5, slice 4): the instance-scoped registration machinery
// (AppView / AppViewInfo / AppCatalog, the tool→view association map, the
// deployment view-domain resolver, and RegisterAppView's atomic wiring) lives
// in go.lumeweb.com/mcpplane/apps. This file is the thin CLI adapter that
// preserves the package-global registration API the CLI was built against:
// a single process-wide AppRegistry serves the CLI's one-server-per-process
// assembly (and its tests, which save/restore association state), while
// product app specs (apps.go) and the open_* launcher tools (open_launcher.go,
// which carries the CLI catalog's MCPTargets seam) remain CLI-owned.

import (
	mcpapps "go.lumeweb.com/mcpplane/apps"
	sdk "go.lumeweb.com/mcpplane/sdk"
)

// AppView / AppViewInfo / AppCatalog are the module's SDK-neutral app-view
// types, re-exported so CLI call sites and the CLI-owned apps.go keep
// compiling unchanged.
type (
	AppView     = mcpapps.AppView
	AppViewInfo = mcpapps.AppViewInfo
	AppCatalog  = mcpapps.AppCatalog
)

// registry is the process-wide app registry. The CLI's deployment origin is
// installed once at server assembly and app registration may happen on
// another goroutine / server instance (tests), so the resolver lives on the
// registry exactly as in the module.
var registry = mcpapps.NewAppRegistry()

// SetViewDomainResolver installs the origin a deployment attributes its ui://
// views to (e.g. the hosted BaseURL origin or the tunnel origin). The resolver
// is invoked at app-registration time so the value reflects the live
// deployment. Unset (zero) — the normal case for a fully self-hosted CLI
// server with no public origin — means views carry NO domain at all, so a
// self-hosted server never advertises a domain that is not its own.
func SetViewDomainResolver(f func() string) { registry.SetViewDomainResolver(f) }

// ViewDomainResolver returns the currently installed view-domain resolver (or
// nil). Exposed so tests can save and restore the deployment origin.
func ViewDomainResolver() func() string { return registry.ViewDomainResolver() }

// AppInfoForTool looks up the app view attached to toolName, if any: the
// server's record of "this tool renders an app", used to annotate needs_human
// results with the companion-app context.
func AppInfoForTool(toolName string) (AppViewInfo, bool) { return registry.AppInfoForTool(toolName) }

// SetAppViewInfo is the explicit registry write paired with AppInfoForTool,
// used when an association must be recorded independently of a full
// RegisterAppView call (and by tests to seed the registry).
func SetAppViewInfo(toolName string, info AppViewInfo) { registry.SetAppViewInfo(toolName, info) }

// DeleteAppViewInfo removes a tool→view association (used by tests and
// teardown paths).
func DeleteAppViewInfo(toolName string) { registry.DeleteAppViewInfo(toolName) }

// RegisterAppView wires a complete app view (ui:// resource, helpers, and
// tool→view associations) onto the official server through the module's
// AppRegistry. Wiring is validated up front and atomic: a failed registration
// never leaves partial app wiring behind (see
// mcpplane/apps.AppRegistry.RegisterAppView).
func RegisterAppView(srv *sdk.Server, catalog AppCatalog, v AppView) error {
	return registry.RegisterAppView(srv, catalog, v)
}

// AttachAppMeta attaches the _meta.ui resource reference onto a catalog tool
// (module-owned implementation, re-exported for the CLI's direct attach path).
var AttachAppMeta = mcpapps.AttachAppMeta

// MCP Apps protocol constants (mirroring @modelcontextprotocol/ext-apps),
// owned by the module and re-exported here.
const (
	// RESOURCE_MIME_TYPE is the MIME type of MCP Apps (mcp-app) resources.
	RESOURCE_MIME_TYPE = mcpapps.RESOURCE_MIME_TYPE
	// RESOURCE_URI_META_KEY is the legacy flat _meta key pointing a tool at its
	// UI resource. Kept so older hosts that do not read the nested _meta.ui
	// shape still find the UI.
	RESOURCE_URI_META_KEY = mcpapps.RESOURCE_URI_META_KEY
	// EXTENSION_ID is the capability extension identifier under which clients
	// advertise MCP Apps support (in client capabilities `extensions`) and
	// servers advertise it back (in server capabilities `extensions`).
	EXTENSION_ID = mcpapps.EXTENSION_ID
)

// GetClientUICapability reads the typed MCP Apps capability from a client's
// advertised `extensions` map; it returns nil when the client did not
// advertise MCP Apps. Module-owned, re-exported.
var GetClientUICapability = mcpapps.GetClientUICapability
