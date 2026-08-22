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
	// registration.
	listMeta := appResourceUIMeta(res.Meta)

	srv.AddResource(&mcp.Resource{
		URI:         res.URI,
		Name:        res.Name,
		Title:       res.Title,
		Description: res.Description,
		MIMEType:    MCPAppsMIMEType,
		Meta:        listMeta,
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
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
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      res.URI,
				MIMEType: MCPAppsMIMEType,
				Text:     res.HTML,
			}},
			// Return a defensive copy, never a shared reference: the read
			// handler runs per-request, and a downstream mutation of the
			// returned map must never corrupt a concurrent read or the server's
			// resources/list entry.
			Meta: cloneMeta(appResourceUIMeta(readMeta)),
		}, nil
	})
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
