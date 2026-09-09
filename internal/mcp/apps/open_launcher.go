package apps

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// OpenLauncherSpec declares a model-facing UI launcher tool. Launching an app
// is an explicit, intentional action: the launcher carries _meta.ui.resourceUri
// so a supporting host renders the app's iframe, and it participates in the
// model's tool list as a normal (visible) tool. It is the ONLY tool that
// advertises the app's resourceUri — the operational primitives that app is
// attached to (upload_file, vault_status, pins_list, ...) remain headless (no
// resourceUri), so mid-workflow agent calls to them never render a card.
//
// The actual operation the app represents is driven by the rendered iframe
// itself over callServerTool against the underlying primitives and app-only
// helpers; the launcher's own handler is a thin "the view is open" result.
type OpenLauncherSpec struct {
	// Name is the launcher tool name, e.g. "open_upload_manager".
	Name string
	// Title is the tool title.
	Title string
	// Description explains that this is a UI launcher (renders an app).
	Description string
	// Category is the discovery category.
	Category model.ToolCategory
	// ResourceURI is the ui:// view this launcher renders, e.g.
	// "ui://uploads/ipfs.html".
	ResourceURI string
	// InputSchema is the tool's argument schema. Most launchers take no
	// arguments (an empty-object schema, the default).
	InputSchema json.RawMessage
}

// NewOpenLauncherDescriptor builds a model-facing launcher tool for the given
// app view. The tool's handler returns a minimal structured result ("the app
// view is open"); the operation the view represents is driven by the iframe
// over callServerTool.
//
// The error return propagates the _meta.ui marshal failure (e.g. an empty
// ResourceURI): a launcher without its marshaled app meta would register as a
// plain tool whose app view silently fails to render, so that is a hard
// assembly error, not one to swallow.
func NewOpenLauncherDescriptor(spec OpenLauncherSpec) (model.ToolDescriptor, error) {
	appMeta, err := sdk.MarshalToolMeta(model.AppToolMeta{
		ResourceURI: spec.ResourceURI,
		Visibility:  []model.ToolVisibility{model.ToolVisibilityModel, model.ToolVisibilityApp},
	})
	if err != nil {
		return model.ToolDescriptor{}, fmt.Errorf("app launcher %q: %w", spec.Name, err)
	}
	if spec.InputSchema == nil {
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	desc := spec.Description
	if desc == "" {
		desc = "Open the " + spec.Title + " app view."
	}
	desc += " open_app (the consolidated launcher) is the unified entry point on hosts where it is visible; this per-app launcher is also discoverable via search_tools."

	return model.ToolDescriptor{
		Name:        spec.Name,
		Title:       spec.Title,
		Description: desc,
		Category:    spec.Category,
		Meta:        appMeta,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(desc)),
		InputSchema: spec.InputSchema,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			// Launching the app is the action. Return a result carrying the
			// resource URI so a UI-capable host renders the iframe; surface
			// any arguments the model passed so the app/agent can act on them.
			sc := map[string]any{"view": spec.ResourceURI}
			for k, v := range request.Arguments {
				sc[k] = v
			}
			return model.ToolResult{
				StructuredContent: sc,
				Text:              toolargs.ResultJSONText(sc) + " The app view is open.",
			}, nil
		},
	}, nil
}
