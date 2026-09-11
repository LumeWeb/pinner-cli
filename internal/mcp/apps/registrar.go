package apps

// This adapter exposes the process-wide registration API the CLI was built
// against over the instance-scoped registration machinery in
// go.lumeweb.com/mcpplane/apps (AppView / AppViewInfo / AppCatalog, the
// tool→view association map, the deployment view-domain resolver, and
// RegisterAppView's atomic wiring). This file is the thin CLI adapter that
// preserves the package-global registration API the CLI was built against:
// a single process-wide AppRegistry serves the CLI's one-server-per-process
// assembly (and its tests, which save/restore association state), while
// product app specs (apps.go) and the open_* launcher tools (open_launcher.go,
// which carries the CLI catalog's MCPTargets seam) remain CLI-owned.

import (
	"sync"
	"sync/atomic"

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
//
// ASSEMBLY RESTRICTION (documented decision): the app registry is
// process-global on purpose — one MCP server is assembled per CLI process,
// and handler-time lookups (open_app, the needs_human annotation) read the
// same registry. That means TWO assemblies that install DIFFERENT
// deployment-origin resolvers (e.g. two hosted embedded servers with distinct
// BaseURLs, in tests or a multi-embed host) must not interleave their
// resolver install/ui://-view-registration windows: a concurrent
// install/resolver-clear pair would attribute one assembly's views to the
// other's origin or to none. Use ForViewDomainResolver to run such a window
// serialized (hosted.go does); direct SetViewDomainResolver remains for
// tests, which are responsible for their own save/restore.
var registry = mcpapps.NewAppRegistry()

// viewDomainMu serializes the resolver-scoped app-registration windows (see
// ForViewDomainResolver). It ALSO guards the direct SetViewDomainResolver
// wrapper: teardown's generation-check/add + registry-restore sequence and
// the setter's generation-add + registry-set sequence are both mutated under
// the same mutex, so they can never interleave (a setter racing teardown
// could otherwise be clobbered by the restore, or leave a stale resolver
// installed when teardown observes the changed generation and skips it).
// Read access (ViewDomainResolver) stays mutex-free — the module registry
// is internally lock-guarded.
var viewDomainMu sync.Mutex

// viewDomainGeneration increments on every resolver mutation; a serialized
// window records the generation at install time and its GUARDED teardown
// only restores the previous resolver when nothing has re-installed one
// since (e.g. another assembly, or a test's save/restore, that must not be
// clobbered).
var viewDomainGeneration atomic.Int64

// ForViewDomainResolver runs fn (the app-registration window of a server
// assembly) with origin installed as the deployment-origin resolver for ui://
// views, serializing concurrent windows: view domains are read at app
// REGISTRATION time from the process-global registry, so two assemblies with
// distinct origins must run one at a time — without the serialization, a
// concurrent install/clear pair would attribute one assembly's views to the
// other's origin or to none. Within the serialized window the teardown is a
// GUARDED clear: a test (or other direct SetViewDomainResolver caller) that
// re-installed its own resolver while this window registered is not clobbered
// by this assembly's teardown. An empty origin ALSO runs through the window,
// installing a resolver that resolves to NO domain: the empty case must still
// be serialized against origin-bearing assemblies, or its views could inherit
// a sibling's domain mid-window instead of the no-domain self-hosted default.
func ForViewDomainResolver(origin string, fn func() error) error {
	viewDomainMu.Lock()
	defer viewDomainMu.Unlock()
	prevResolver := registry.ViewDomainResolver()
	installedAt := viewDomainGeneration.Add(1)
	registry.SetViewDomainResolver(func() string { return origin })
	defer func() {
		if viewDomainGeneration.Load() == installedAt {
			// Guarded clear: nothing re-installed a resolver over this
			// window's — restore whatever preceded the window. (Deferred
			// before the outer unlock, so the teardown runs serialized
			// against other windows.)
			viewDomainGeneration.Add(1)
			registry.SetViewDomainResolver(prevResolver)
		}
	}()
	return fn()
}

// SetViewDomainResolver installs the origin a deployment attributes its ui://
// views to (e.g. the hosted BaseURL origin or the tunnel origin). The resolver
// is invoked at app-registration time so the value reflects the live
// deployment. Unset (zero) — the normal case for a fully self-hosted CLI
// server with no public origin — means views carry NO domain at all, so a
// self-hosted server never advertises a domain that is not its own.
//
// Production assemblies must go through ForViewDomainResolver instead so the
// install/clear windows cannot interleave; this direct setter is for tests.
// It is serialized on the same viewDomainMu as ForViewDomainResolver so the
// generation-guarded teardown cannot interleave with it; it must therefore
// never be called from inside ForViewDomainResolver's fn (which holds the
// mutex) — use the registry setter directly there.
func SetViewDomainResolver(f func() string) {
	viewDomainMu.Lock()
	defer viewDomainMu.Unlock()
	viewDomainGeneration.Add(1)
	registry.SetViewDomainResolver(f)
}

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
