// MCP Apps (ext-apps) server-side helpers.
//
// This file provides the high-level MCP Apps API so Pinner tool authors don't
// hand-write the ui://-resource / iframe / _meta.ui plumbing themselves. It is
// a port of the server side of `@modelcontextprotocol/ext-apps` onto the
// official Go SDK's wire seam (_meta + resources/list + resources/read +
// capabilities extensions).
//
// Two mechanisms coexist in MCP and must not be confused:
//
//   - Form elicitation (pkg/internal/mcp/elicitation.go): a core-spec
//     `input_required` multi-round-trip for gathering input from the user as
//     part of a tool call. Renders a schema-desribed form the client owns.
//   - MCP Apps (this file): an extension that pairs an existing TOOL with an
//     HTML resource named by `_meta.ui.resourceUri` (scheme `ui://`). The host
//     fetches that resource and renders it in a sandboxed iframe; the tool
//     continues to work (with a text fallback) for non-UI hosts.
//
// Like [sdk_official.go], this file imports the official MCP SDK; it is part
// of the wire seam. Tool/resource business logic must NOT import either MCP
// SDK and instead use these typed helpers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// MCP Apps protocol constants (mirroring @modelcontextprotocol/ext-apps).
const (
	// RESOURCE_MIME_TYPE is the MIME type of MCP Apps (mcp-app) resources.
	RESOURCE_MIME_TYPE = "text/html;profile=mcp-app"
	// Legacy flat _meta key pointing a tool at its UI resource. Kept so older
	// hosts that do not read the nested _meta.ui shape still find the UI.
	RESOURCE_URI_META_KEY = "ui/resourceUri"
	// EXTENSION_ID is the capability extension identifier under which clients
	// advertise MCP Apps support (in client capabilities `extensions`) and
	// servers advertise it back (in server capabilities `extensions`).
	EXTENSION_ID = "io.modelcontextprotocol/ui"
)

// appToolMetaJSON is the nested `_meta.ui` wire shape for a tool. Encoded with
// the resource URI as the recognized key.
type appToolMetaJSON struct {
	ResourceURI string   `json:"resourceUri"`
	Visibility  []string `json:"visibility,omitempty"`
}

// marshalToolMeta produces the full `_meta` map (both the nested ui shape and
// the legacy flat key) from a typed model.AppToolMeta.
func marshalToolMeta(meta model.AppToolMeta) (mcp.Meta, error) {
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
	out := mcp.Meta{
		"ui":                  uiAny,
		RESOURCE_URI_META_KEY: meta.ResourceURI,
	}
	return out, nil
}

// RegisterAppTool registers a Pinner-owned tool and attaches its MCP Apps UI
// metadata (`_meta.ui.resourceUri` plus the legacy flat key) so a UI-capable
// host renders the referenced ui:// resource for this tool. Plain (non-UI)
// hosts still call the tool normally and receive the text fallback from the
// handler. desc.Meta is extended, never replaced, so existing metadata
// survives.
func RegisterAppTool(srv *mcp.Server, desc model.ToolDescriptor, app model.AppToolMeta) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	meta, err := marshalToolMeta(app)
	if err != nil {
		return err
	}
	if desc.Meta == nil {
		desc.Meta = mcp.Meta{}
	}
	for k, v := range meta {
		desc.Meta[k] = v
	}
	return registerTool(srv, sdk.Tool(desc), desc.Handler)
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

// RegisterAppResource registers a ui:// app resource that serves the given
// HTML document. The MIME type defaults to RESOURCE_MIME_TYPE. The resource's
// AppResourceMeta (CSP/domain/prefersBorder) is attached to the resource list
// entry AND to the read result, matching ext-apps' listing-level fallback and
// content-item-override semantics (the read-level value takes precedence).
func RegisterAppResource(srv *mcp.Server, res AppResource) error {
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
		MIMEType:    RESOURCE_MIME_TYPE,
		Meta:        listMeta,
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      res.URI,
				MIMEType: RESOURCE_MIME_TYPE,
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

// GetClientUICapability reads the typed MCP Apps capability from a client's
// advertised `extensions` (map of extension id -> settings). It returns nil if
// the client did not advertise MCP Apps.
func GetClientUICapability(extensions map[string]any) *model.ClientUICapabilities {
	raw, ok := extensions[EXTENSION_ID]
	if !ok || raw == nil {
		return nil
	}
	// The extension setting is a plain object (not an array/scalar). Decode it
	// typed rather than casting fields by hand.
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var parsed struct {
		MIMETypes []string `json:"mimeTypes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	if parsed.MIMETypes == nil {
		return &model.ClientUICapabilities{}
	}
	return &model.ClientUICapabilities{MIMETypes: parsed.MIMETypes}
}
