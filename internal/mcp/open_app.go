package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
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
// openAppDescriptionFor returns the profile-aware description for the open_app
// tool. On GUI-capable hosts (FeatMCPApps) the copy steers the agent toward
// using open_app for human-facing interactions; on agent-only hosts it explains
// the tool returns a ui:// URI as data (no auto-render) so the agent includes
// it in a message for a human to open.
func openAppDescriptionFor(p hostenv.PlatformProfile) string {
	if p.Features.Has(hostenv.FeatMCPApps) {
		return "Open one of Pinner's interactive app views by name (vault_browser, sso_signin, pin_creator, upload_manager, pin_list, account, vault_create, vault_restore, account_password, account_email). The host renders the returned ui:// view as an iframe. Use this for human-facing interactions; prefer headless primitives (vault_status, vault_put_file, pins_list, auth_sso, ...) for autonomous workflows."
	}
	return "Resolve an app name to its ui:// view URI. This host does not render MCP Apps, so the URI is returned as data — include it in a message for a human to open. Available apps: vault_browser, sso_signin, pin_creator, upload_manager, pin_list, account, vault_create, vault_restore, account_password, account_email."
}

func openAppTargets() []model.ToolTarget {
	return toolforge.MCPTargets(model.ToolTarget{
		Visible:  true,
		DescFunc: openAppDescriptionFor,
	})
}

func newOpenAppDescriptor(catalog *ToolCatalog) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "open_app",
		Title:       "Open an app",
		Description: openAppDescriptionFor(hostenv.ProfileStdioMCPApps),
		Category:    model.CategoryCore,
		MCPTargets:  openAppTargets(),
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
