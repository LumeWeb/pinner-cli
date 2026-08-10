package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP Apps (ext-apps) product views. Each App pairs one or more existing tools
// with a ui:// HTML resource (registered here) and optionally an app-only
// helper tool that the UI calls but the model never sees.

// PinCreateAppURI is the ui:// resource serving the "Create a Pin" app.
const PinCreateAppURI = "ui://pins/create.html"

// PinningProvider is the narrow, SDK-neutral view the pin app needs from a
// pinning backend: it reads a pin's status so the app can poll until terminal.
// It is a subset of the CLI's PinningService, kept small for testability.
type PinningProvider interface {
	// PinStatus returns the current status of the pinned CID.
	PinStatus(ctx context.Context, cid string) (PinStatusView, error)
}

// PinningProviderFactory builds a live PinningProvider (e.g. wrapping the CLI's
// PinningService) at server-build time. Returning an error aborts server setup.
type PinningProviderFactory func() (PinningProvider, error)

// PinStatusView is a typed, SDK-neutral pin status read by the pin app.
type PinStatusView struct {
	CID    string `json:"cid"`
	Status string `json:"status"`
}

// renderPinCreateAppHTML renders the full "Create a Pin" app document
// (ui://pins/create.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + pin logic) come from
// renderMcpAppDoc; only the visible body form is authored in templ. Served
// verbatim so the sandboxed iframe needs no network request.
func renderPinCreateAppHTML() string {
	return renderMcpAppDoc("Create a Pin", pinCreateAppForm(), pinAppModule(extAppsClientBase64()))
}


// attachPinAppMeta wires pinner_pin (a curated catalog tool) to its ui:// app
// resource so a UI-capable host renders the create-pin view for it. Plain hosts
// keep the tool's text result. The entry's existing metadata is extended.
func attachPinAppMeta(catalog *ToolCatalog) error {
	entry, ok := catalog.Get("pinner_pin")
	if !ok {
		return fmt.Errorf("pinner_pin not in catalog")
	}
	meta, err := marshalToolMeta(AppToolMeta{ResourceURI: PinCreateAppURI})
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

// pinStatusDescriptor builds the app-only pin status helper. It is visible to
// the app only (never the model) and shares the pin create view; the pin app
// calls it via callServerTool to poll until a terminal state.
func pinStatusDescriptor(pins PinningProvider) ToolDescriptor {
	return ToolDescriptor{
		Name:        "pinner_pin_status",
		Description: "Poll the current status of a pinned CID. App-only helper for the Create a Pin view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"cid":{"type":"string"}},"required":["cid"]}`),
		Handler: func(ctx context.Context, req ToolRequest) (ToolResult, error) {
			cid, _ := req.Arguments["cid"].(string)
			if cid == "" {
				return ToolResult{IsError: true, Text: "cid is required"}, nil
			}
			view, err := pins.PinStatus(ctx, cid)
			if err != nil {
				return ToolResult{IsError: true, Text: err.Error()}, nil
			}
			return ToolResult{
				Text:              fmt.Sprintf("%s: %s", view.CID, view.Status),
				StructuredContent: map[string]any{"status": view.Status, "cid": view.CID},
			}, nil
		},
	}
}

// RegisterPinApp wires the complete "Create a Pin" MCP App:
//   - attaches the ui:// view to the curated pinner_pin tool,
//   - registers the ui://pins/create.html HTML resource,
//   - registers the app-only pinner_pin_status polling helper.
//
// Returns an error if the pin tool is missing from the catalog. App wiring is
// additive: existing curated tools and plain-host text results are preserved.
func RegisterPinApp(srv *mcp.Server, catalog *ToolCatalog, pins PinningProvider) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if pins == nil {
		return fmt.Errorf("nil pinning provider")
	}

	if err := attachPinAppMeta(catalog); err != nil {
		return err
	}

	if err := RegisterAppResource(srv, AppResource{
		URI:         PinCreateAppURI,
		Name:        "create-pin",
		Title:       "Create a Pin",
		Description: "Create a pin for an existing CID via the Pinner.xyz API.",
		Meta: AppResourceMeta{
			PrefersBorder: boolPtr(true),
		},
		HTML: renderPinCreateAppHTML(),
	}); err != nil {
		return err
	}

	if err := RegisterAppTool(srv, pinStatusDescriptor(pins), AppToolMeta{
		ResourceURI: PinCreateAppURI,
		Visibility:  []ToolVisibility{ToolVisibilityApp},
	}); err != nil {
		return err
	}

	return nil
}

// boolPtr returns a pointer to b (nil-safe convenience for optional flags).
func boolPtr(b bool) *bool { return &b }
