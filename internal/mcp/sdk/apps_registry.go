package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// RegisterToolFunc adapts a Pinner-owned tool descriptor + handler into the
// SDK tool handler used by server.AddTool. The hub provides the concrete
// adapter (which wraps the handler with per-request capability, app, and
// elicitation state logic); sdk is registered after that so the app-registration
// bridge can reuse the same single registration seam.
type RegisterToolFunc func(srv *Server, desc model.ToolDescriptor, handler model.PinnerToolHandler) error

// registerToolFn is the tool-registration adapter used by app tools. The hub
// installs its adapter (registerTool, which routes through AdaptToolHandler
// with the hub's deps) via SetToolRegistrar during assembly. It is read on
// every app-tool registration, so it is guarded by a mutex. Failing fast when
// unset (rather than a degraded default) ensures a missing SetToolRegistrar is
// caught loudly at registration, never as silent loss of the
// elicitation/capability/state adornment app tools depend on.
var (
	registerToolMu sync.RWMutex
	registerToolFn RegisterToolFunc
)

// SetToolRegistrar installs the tool-adapter used by app-tool registration.
// It must be called once during hub assembly (before any server serves) so
// RegisterAppTool can reuse the hub's handler-adaptation seam.
func SetToolRegistrar(f RegisterToolFunc) {
	registerToolMu.Lock()
	registerToolFn = f
	registerToolMu.Unlock()
}

// getToolRegistrar returns the installed tool-registration adapter, or a
// fail-fast stub when none was installed.
func getToolRegistrar() RegisterToolFunc {
	registerToolMu.RLock()
	defer registerToolMu.RUnlock()
	if registerToolFn == nil {
		return func(*Server, model.ToolDescriptor, model.PinnerToolHandler) error {
			return fmt.Errorf("sdk: tool registrar not installed; call sdk.SetToolRegistrar before RegisterAppTool")
		}
	}
	return registerToolFn
}

// AppResource is a typed, SDK-neutral app HTML resource registration.
type AppResource struct {
	URI         string
	Name        string
	Title       string
	Description string
	Meta        model.AppResourceMeta
	// ConnectDomainsFunc, when set, is called at read time to resolve the CSP
	// connectDomains the sandboxed app may reach over the network (e.g. the
	// origin the app's Uppy XHR uploader PUTs to). It is needed because that
	// origin is only known once the server/tunnel base URL is resolved — which
	// happens AFTER app registration — so the value cannot be baked into Meta
	// at registration time. The resolved list overrides Meta.CSP.ConnectDomains
	// in the read result (the value a host uses to render the app), while the
	// resources/list entry keeps the static Meta fallback.
	ConnectDomainsFunc func() []string
	// HTML is the rendered mcp-app document served by resources/read.
	HTML string
}

// RegisterAppTool registers a Pinner-owned tool and attaches its MCP Apps UI
// metadata (`_meta.ui.resourceUri` plus the legacy flat key) so a UI-capable
// host renders the referenced ui:// resource for this tool. Plain (non-UI)
// hosts still call the tool normally and receive the text fallback from the
// handler. desc.Meta is extended, never replaced, so existing metadata
// survives.
func RegisterAppTool(srv *Server, desc model.ToolDescriptor, meta model.AppToolMeta) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	attached, err := MarshalToolMeta(meta)
	if err != nil {
		return err
	}
	if desc.Meta == nil {
		desc.Meta = mcp.Meta{}
	}
	for k, v := range attached {
		desc.Meta[k] = v
	}
	return getToolRegistrar()(srv, desc, desc.Handler)
}

// appResourceReg is the retained state of a registered ui:// app resource so
// the SDK can re-register it later (e.g. to bake a live connectDomains into the
// resources/list entry once the transport has resolved its origin).
type appResourceReg struct {
	// meta is the original AppResourceMeta captured at registration (before any
	// runtime connectDomains overwrite). Re-registration rebuilds from it so the
	// static fields (domain/prefersBorder) are never lost.
	meta model.AppResourceMeta
	// handler serves resources/read for this resource and is kept verbatim across
	// re-registration so the read-level semantics stay identical.
	handler mcp.ResourceHandler
	// resource preserves the registration-time fields (URI/name/title/etc.) so a
	// re-registration can rebuild the resource without carrying stale meta.
	resource *mcp.Resource
}

// appResourceRegs tracks registered app resources keyed by server and then by
// resource URI so SetAppResourceConnectDomains can re-register one with an
// updated listing-level CSP without colliding across distinct server instances:
// RegisterAppResource/SetAppResourceConnectDomains are per-server APIs, so two
// servers in one process (e.g. tests, or multiple streamable listeners) may
// register the same app URI without the second overwriting the first's retained
// meta/handler. Guarded by appResourceRegsMu.
var (
	appResourceRegsMu sync.Mutex
	appResourceRegs   = map[*mcp.Server]map[string]appResourceReg{}
)

// serverAppResources returns the per-server registry map for srv, creating it if
// absent. The caller must hold appResourceRegsMu.
func serverAppResources(srv *mcp.Server) map[string]appResourceReg {
	m := appResourceRegs[srv]
	if m == nil {
		m = map[string]appResourceReg{}
		appResourceRegs[srv] = m
	}
	return m
}

// RegisterAppResource registers a ui:// app resource that serves the given
// HTML document. The MIME type defaults to MCPAppsMIMEType. The resource's
// AppResourceMeta (CSP/domain/prefersBorder) is attached to the resource list
// entry AND to the read result, matching ext-apps' listing-level fallback and
// content-item-override semantics (the read-level value takes precedence).
func RegisterAppResource(srv *Server, res AppResource) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if res.URI == "" {
		return fmt.Errorf("app resource requires a uri")
	}
	// The resources/list entry carries the static Meta fallback (baked because
	// go-sdk's list serializes the retained Resource.Meta). The read result —
	// what a host uses to render the app — resolves the dynamic
	// ConnectDomainsFunc so the advertised connect-src reflects the current
	// base/tunnel or loopback origin even though it was resolved after
	// registration. A dynamic connectDomains is baked into the LIST entry later,
	// once the origin is known, via SetAppResourceConnectDomains — hosts read
	// the list at connection time (see ext-apps: the list entry is the static
	// default hosts review at connection time), so an empty list CSP means the
	// host derives connect-src 'self' and blocks the upload.
	handler := func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		// Resolve the read-level meta on every read so a dynamic
		// ConnectDomainsFunc reflects the LIVE origin (the tunnel/base URL or
		// loopback address can change after registration as the transport
		// resolves its public origin). The read result is the value a host uses
		// to render the app.
		readMeta := res.Meta
		if res.ConnectDomainsFunc != nil {
			readMeta.CSP = cloneAppResourceCSP(readMeta.CSP)
			readMeta.CSP.ConnectDomains = res.ConnectDomainsFunc()
		}
		// _meta.ui MUST go on the content item (ResourceContents.Meta), NOT on
		// the top-level ReadResourceResult.Meta. The ext-apps spec says hosts
		// read CSP from "the resources/read content item (with resources/list
		// entry as fallback)". An MCP host reads ResourceContents._meta.ui.csp to
		// derive the sandbox connect-src; a result-level _meta.ui is invisible
		// to the CSP enforcement path.
		uiMeta := cloneMeta(appResourceUIMeta(readMeta))
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      res.URI,
				MIMEType: MCPAppsMIMEType,
				Text:     res.HTML,
				Meta:     uiMeta,
			}},
		}, nil
	}
	resource := &mcp.Resource{
		URI:         res.URI,
		Name:        res.Name,
		Title:       res.Title,
		Description: res.Description,
		MIMEType:    MCPAppsMIMEType,
		Meta:        appResourceUIMeta(res.Meta),
	}
	appResourceRegsMu.Lock()
	serverAppResources(srv)[res.URI] = appResourceReg{meta: res.Meta, handler: handler, resource: resource}
	appResourceRegsMu.Unlock()
	srv.AddResource(resource, handler)
	return nil
}

// SetAppResourceConnectDomains bakes a live CSP connectDomains into the
// resources/list entry of a registered app resource. It must be called once the
// transport has resolved its base/tunnel (or loopback) origin — always after
// registration — so a host that reads the list at connection time (e.g. an MCP
// host deriving its sandbox connect-src) sees the origin the app's Uppy XHR
// uploader PUTs to. The resource is re-registered under its URI (go-sdk replaces by
// URI) with the same read handler, so read-level behavior is unchanged. It is
// safe to call more than once (e.g. first with the loopback/base origin, then
// again with the provider-approved tunnel origin); the last write wins.
func SetAppResourceConnectDomains(srv *Server, uri string, origins []string) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	appResourceRegsMu.Lock()
	reg, ok := serverAppResources(srv)[uri]
	if !ok {
		appResourceRegsMu.Unlock()
		return fmt.Errorf("sdk: no registered app resource %q", uri)
	}
	// Rebuild the listing-level meta with the resolved connectDomains while
	// preserving the registration-time CSP siblings (resourceDomains etc.) and
	// the non-CSP meta (domain, prefersBorder).
	meta := reg.meta
	csp := cloneAppResourceCSP(meta.CSP)
	csp.ConnectDomains = append([]string(nil), origins...)
	meta.CSP = csp
	resource := &mcp.Resource{
		URI:         reg.resource.URI,
		Name:        reg.resource.Name,
		Title:       reg.resource.Title,
		Description: reg.resource.Description,
		MIMEType:    reg.resource.MIMEType,
		Meta:        appResourceUIMeta(meta),
	}
	serverAppResources(srv)[uri] = appResourceReg{meta: meta, handler: reg.handler, resource: resource}
	appResourceRegsMu.Unlock()
	srv.AddResource(resource, reg.handler)
	return nil
}

// UnregisterAppResource releases a registered app resource for srv: it removes
// the live resource from the server (go-sdk RemoveResources drops it from
// resources/list and read) and discards the SDK's retained registration state so
// the handler closure and captured app HTML can be garbage-collected. Call this
// when a server is being discarded to keep the per-server registry from growing
// without bound in long-running or multi-server processes. Removing a URI that
// was never registered is a no-op.
func UnregisterAppResource(srv *Server, uri string) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	appResourceRegsMu.Lock()
	m, ok := appResourceRegs[srv]
	if !ok {
		appResourceRegsMu.Unlock()
		return nil
	}
	if _, ok := m[uri]; ok {
		delete(m, uri)
		if len(m) == 0 {
			delete(appResourceRegs, srv)
		}
	}
	appResourceRegsMu.Unlock()
	srv.RemoveResources(uri)
	return nil
}

// appResourceUIMeta marshals an AppResourceMeta into the `_meta.ui` map, or an
// empty meta map when the meta carries no fields (mirroring the previous
// "always an empty _meta.ui" wire shape for a bare app resource).
func appResourceUIMeta(meta model.AppResourceMeta) mcp.Meta {
	out := mcp.Meta{}
	if meta == (model.AppResourceMeta{}) {
		return out
	}
	uiJSON, err := json.Marshal(meta)
	if err != nil {
		return out
	}
	var uiAny map[string]any
	if err := json.Unmarshal(uiJSON, &uiAny); err != nil {
		return out
	}
	out["ui"] = uiAny
	return out
}

// cloneAppResourceCSP returns a deep copy of csp, or a fresh empty CSP when csp
// is nil, so a dynamic ConnectDomains overwrite never mutates the caller's
// (or a sibling resource's) shared CSP pointer.
func cloneAppResourceCSP(csp *model.AppResourceCSP) *model.AppResourceCSP {
	cp := &model.AppResourceCSP{}
	if csp == nil {
		return cp
	}
	*cp = *csp
	cp.ConnectDomains = append([]string(nil), csp.ConnectDomains...)
	cp.ResourceDomains = append([]string(nil), csp.ResourceDomains...)
	cp.FrameDomains = append([]string(nil), csp.FrameDomains...)
	return cp
}

// appToolMetaJSON is the nested `_meta.ui` wire shape for a tool. Encoded with
// the resource URI as the recognized key.
type appToolMetaJSON struct {
	ResourceURI string   `json:"resourceUri"`
	Visibility  []string `json:"visibility,omitempty"`
}

// MarshalToolMeta produces the full `_meta` map (both the nested ui shape and
// the legacy flat key) from a typed model.AppToolMeta. The hub uses it to
// attach app metadata to catalog entries that are not registered directly
// through RegisterAppTool.
func MarshalToolMeta(meta model.AppToolMeta) (mcp.Meta, error) {
	if meta.ResourceURI == "" {
		return nil, fmt.Errorf("app tool requires a ui:// resourceUri")
	}
	ui := appToolMetaJSON{ResourceURI: meta.ResourceURI}
	for _, v := range meta.Visibility {
		ui.Visibility = append(ui.Visibility, string(v))
	}
	uiJSON, err := json.Marshal(ui)
	if err != nil {
		return nil, err
	}
	var uiAny map[string]any
	if err := json.Unmarshal(uiJSON, &uiAny); err != nil {
		return nil, err
	}
	return mcp.Meta{
		"ui":                      uiAny,
		MCPAppsResourceURIMetaKey: meta.ResourceURI,
	}, nil
}

// cloneMeta returns a deep copy of a non-nil meta map (nested "ui" included),
// or nil for an empty input map.
func cloneMeta(meta mcp.Meta) mcp.Meta {
	if len(meta) == 0 {
		return nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		// Meta only ever holds JSON-serializable values built above; treat a
		// marshal failure as "no meta" rather than panicking at serve time.
		return nil
	}
	var out mcp.Meta
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
