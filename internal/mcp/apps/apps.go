package apps

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
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
// mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ. Served
// verbatim so the sandboxed iframe needs no network request.
func RenderPinCreateAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Create a Pin", mcpapp.PinCreateAppForm(), mcpapp.AppModule("pin"))
}

// pinStatusDescriptor builds the app-only pin status helper. It is visible to
// the app only (never the model) and shares the pin create view; the pin app
// calls it via callServerTool to poll until a terminal state.
func pinStatusDescriptor(pins PinningProvider) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "pin_status",
		Description: "Poll the current status of a pinned CID. App-only helper for the Create a Pin view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"cid":{"type":"string"}},"required":["cid"]}`),
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			cid, _ := req.Arguments["cid"].(string)
			if cid == "" {
				return model.ToolResult{IsError: true, Text: "cid is required"}, nil
			}
			view, err := pins.PinStatus(ctx, cid)
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}
			return model.ToolResult{
				Text:              fmt.Sprintf("%s: %s", view.CID, view.Status),
				StructuredContent: map[string]any{"status": view.Status, "cid": view.CID},
			}, nil
		},
	}
}

// RegisterPinApp wires the complete "Create a Pin" MCP App: attaches the
// ui:// view to the curated pins_add tool, registers the ui://pins/create.html
// HTML resource, and registers the app-only pin_status polling helper. It is
// expressed through the shared RegisterAppView lib layer so the pin app stays
// a single declarative spec rather than hand-written registration plumbing.
//
// Returns an error if the pin tool is missing from the catalog. App wiring is
// additive: existing curated tools and plain-host text results are preserved.
func RegisterPinApp(srv *sdk.Server, catalog AppCatalog, pins PinningProvider) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if pins == nil {
		return fmt.Errorf("nil pinning provider")
	}

	return RegisterAppView(srv, catalog, AppView{
		URI:           PinCreateAppURI,
		Name:          "create-pin",
		Title:         "Create a Pin",
		Description:   "Create a pin for an existing CID via the Pinner.xyz API.",
		HTML:          RenderPinCreateAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"pins_add"},
		Helpers:       []model.ToolDescriptor{pinStatusDescriptor(pins)},
	})
}

// boolPtr returns a pointer to b (nil-safe convenience for optional flags).
func boolPtr(b bool) *bool { return &b }
