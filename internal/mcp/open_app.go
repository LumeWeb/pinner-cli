package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// openAppInput is the argument shape of the consolidated open_app tool.
type openAppInput struct {
	// App is the open_* launcher name (e.g. "open_vault_browser") or its bare
	// screen name (e.g. "vault_browser") whose app view should be opened.
	App string `json:"app" jsonschema:"description=Which app view to open. Use a launcher name (open_vault_browser, open_sso_signin, open_upload_manager, ...) or the bare screen name (vault_browser, sso_signin, upload_manager, ...)."`
}

// openAppOutput is the StructuredContent returned by open_app.
type openAppOutput struct {
	// App is the resolved launcher name.
	App string `json:"app"`
	// View is the ui:// resource URI to render for the app.
	View string `json:"view"`
	// Available lists every app view this server currently exposes, for hosts
	// whose clients want to enumerate them.
	Available []string `json:"available,omitempty"`
}

// availableOpenApps returns the sorted list of open_* launcher names currently
// registered in the catalog (entries whose name starts with "open_" and that
// have an attached MCP App view). It is the single source for both the
// available list in the result and the schema enum for open_app.
func availableOpenApps(catalog *ToolCatalog) []string {
	var names []string
	for _, entry := range catalog.Entries() {
		if !strings.HasPrefix(entry.Name, "open_") {
			continue
		}
		if _, ok := apps.AppInfoForTool(entry.Name); !ok {
			continue
		}
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// resolveOpenApp resolves an app request (launcher name or bare screen name)
// to its registered launcher name and ui:// resource URI, or returns false.
func resolveOpenApp(catalog *ToolCatalog, requested string) (string, string, bool) {
	req := strings.TrimSpace(requested)
	if req == "" {
		return "", "", false
	}
	// Accept the full launcher name first.
	if info, ok := apps.AppInfoForTool(req); ok {
		return req, info.URI, true
	}
	// Accept a bare screen name by matching the launcher's suffix.
	for _, name := range availableOpenApps(catalog) {
		if strings.TrimPrefix(name, "open_") == req {
			info, _ := apps.AppInfoForTool(name)
			return name, info.URI, true
		}
	}
	return "", "", false
}

// newOpenAppDescriptor builds the consolidated open_app tool. It is the single
// directly-surfaced launcher for a GUI-capable host: instead of one
// per-screen open_* tool on tools/list, the agent calls open_app with an app
// name and receives the ui:// view to render. The handler resolves the app
// against the live catalog's app-view registry, so availability always tracks
// what is actually wired.
func newOpenAppDescriptor(catalog *ToolCatalog) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "open_app",
		Title:       "Open an app",
		Description: "Open one of Pinner's interactive app views (vault browser, create/restore vault, sign in, pin creator, upload/download managers, account). Pass the app name and it returns the ui:// view to render. This is the single launcher entry point — prefer the headless primitives (vault_status, vault_put_file, pins_list, ...) for autonomous workflows, and open_app only when a human-facing screen is desired.",
		Category:    model.CategoryCore,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback("Open one of Pinner's interactive app views by name and receive the ui:// view to render.")),
		InputSchema: toolargs.ToolSchemaFor[openAppInput](),
		Meta:        nil,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[openAppInput](request)
			if err != nil {
				return model.ToolResult{IsError: true, Text: err.Error()}, nil
			}
			name, view, ok := resolveOpenApp(catalog, in.App)
			if !ok {
				avail := availableOpenApps(catalog)
				return model.ToolResult{
					IsError: true,
					Text:    fmt.Sprintf("unknown app %q; available apps: %s", in.App, strings.Join(avail, ", ")),
				}, nil
			}
			sc := openAppOutput{App: name, View: view, Available: availableOpenApps(catalog)}
			return model.ToolResult{StructuredContent: sc, Text: toolargs.ResultJSONText(sc)}, nil
		},
	}
}
