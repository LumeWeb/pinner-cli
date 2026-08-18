package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// RegisterToolFunc adapts a Pinner-owned tool handler into the SDK tool
// handler used by server.AddTool. The hub provides the concrete adapter
// (officialToolHandler wraps the handler with per-request capability, app, and
// elicitation state logic); sdk is registered after that so the app-registration
// bridge can reuse the same single registration seam.
type RegisterToolFunc func(srv *Server, tool *mcp.Tool, handler model.PinnerToolHandler) error

// registerToolFn is the tool-registration adapter used by app tools. The hub
// installs its adapter (officialToolHandler) via SetToolRegistrar during
// assembly. It is read on every app-tool registration, so it is guarded by a
// mutex. Failing fast when unset (rather than a degraded default) ensures a
// missing SetToolRegistrar is caught loudly at registration, never as silent
// loss of the elicitation/capability/state adornment app tools depend on.
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
		return func(*Server, *mcp.Tool, model.PinnerToolHandler) error {
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
	return getToolRegistrar()(srv, Tool(desc), desc.Handler)
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
	listMeta := mcp.Meta{}
	if res.Meta != (model.AppResourceMeta{}) {
		uiJSON, err := json.Marshal(res.Meta)
		if err != nil {
			return err
		}
		var uiAny map[string]any
		if err := json.Unmarshal(uiJSON, &uiAny); err != nil {
			return err
		}
		listMeta["ui"] = uiAny
	}
	srv.AddResource(&mcp.Resource{
		URI:         res.URI,
		Name:        res.Name,
		Title:       res.Title,
		Description: res.Description,
		MIMEType:    MCPAppsMIMEType,
		Meta:        listMeta,
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      res.URI,
				MIMEType: MCPAppsMIMEType,
				Text:     res.HTML,
			}},
			// Return a defensive copy, never the shared listMeta reference: the
			// server retains listMeta on the Resource entry for resources/list,
			// and a read-time mutation of the shared map would corrupt every
			// subsequent listing. The read-level value deliberately mirrors the
			// listing-level fallback value from res.Meta.
			Meta: cloneMeta(listMeta),
		}, nil
	})
	return nil
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
