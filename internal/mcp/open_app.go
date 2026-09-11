package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
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

// installedAppRecords enumerates the app views of THIS server: the finalized
// MaterializedTooling's captured app records when the assembly plan recorded
// one — the authoritative server-local source of "which apps are installed
// and at which ui:// URIs" — falling back to the catalog-scan derivation for
// catalogs assembled without the plan (documented compatibility fallback; the
// scan ALSO requires catalog membership, so a stale process-global app
// registration for a launcher this server never declared can never resolve).
func installedAppRecords(catalog *ToolCatalog) []InstalledAppView {
	if finalized := catalog.FinalizedTooling(); finalized != nil {
		return finalized.AppRecords()
	}
	var records []InstalledAppView
	for _, entry := range catalog.Entries() {
		if !strings.HasPrefix(entry.Name, "open_") {
			continue
		}
		info, ok := apps.AppInfoForTool(entry.Name)
		if !ok {
			continue
		}
		records = append(records, InstalledAppView{
			Launcher: entry.Name,
			Screen:   strings.TrimPrefix(entry.Name, "open_"),
			URI:      info.URI,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Launcher < records[j].Launcher })
	return records
}

// openAppAppNames returns the bare app screen names actually installed on this
// server (sorted) — the derived open_app inventory.
func openAppAppNames(catalog *ToolCatalog) []string {
	var names []string
	for _, record := range installedAppRecords(catalog) {
		names = append(names, record.Screen)
	}
	return names
}

// openAppAppList renders the installed app names for prose; unknown/none
// installs get an explicit truthful answer rather than a hard-coded inventory.
func openAppAppList(catalog *ToolCatalog) string {
	names := openAppAppNames(catalog)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// availableOpenApps returns the sorted list of open_* launcher names whose app
// views are installed on this server.
func availableOpenApps(catalog *ToolCatalog) []string {
	var names []string
	for _, record := range installedAppRecords(catalog) {
		names = append(names, record.Launcher)
	}
	return names
}

// resolveOpenApp resolves an app request (an open_* launcher name or its bare
// screen name) to the launcher name and ui:// resource URI of an app view
// installed on THIS server, or returns false. It consults ONLY the server's
// own installed app records — never the process-global app registry directly —
// so a full launcher name that this server never installed (or a stale
// launcher left over from a previous assembly in the same process) is rejected.
func resolveOpenApp(catalog *ToolCatalog, requested string) (string, string, bool) {
	req := strings.TrimSpace(strings.ToLower(requested))
	if req == "" {
		return "", "", false
	}
	for _, record := range installedAppRecords(catalog) {
		if record.Launcher == req || record.Screen == req {
			return record.Launcher, record.URI, true
		}
	}
	return "", "", false
}

// openAppDescriptionFor returns the profile-aware description for the open_app
// tool, derived from the server's own installed app views. On GUI-capable hosts
// (FeatMCPApps) the copy steers the agent toward using open_app for
// human-facing interactions and names headless primitive examples from the ONE
// shared availability-gated source (headlessPrimitiveExamplesFor — the same
// list the agent guide's MCP-Apps rule uses), so a hosted/minimal assembly
// never suggests a headless primitive its surface never registered; on
// agent-only hosts it explains the tool returns a ui:// URI as data (no
// auto-render) so the agent includes it in a message for a human to open. The
// tool's INPUT SCHEMA is intentionally NOT app-derived (the tools/list schema
// contract stays stable), only the description copy.
func openAppDescriptionFor(p hostenv.PlatformProfile, catalog *ToolCatalog) string {
	if p.Features.Has(hostenv.FeatMCPApps) {
		return "Open one of Pinner's interactive app views by name (" + openAppAppList(catalog) + "). The host renders the returned ui:// view as an iframe. Use this for human-facing interactions; prefer " + headlessPrimitiveExamplesFor(catalogToolAvailable(catalog)) + " for autonomous workflows."
	}
	return "Resolve an app name to its ui:// view URI. This host does not render MCP Apps, so the URI is returned as data — include it in a message for a human to open. Available apps: " + openAppAppList(catalog) + "."
}

func openAppTargets(catalog *ToolCatalog) []model.ToolTarget {
	return toolforge.MCPTargets(model.ToolTarget{
		Visible: true,
		DescFunc: toolforge.DescResolver(func(p hostenv.PlatformProfile) string {
			return openAppDescriptionFor(p, catalog)
		}),
	})
}

// openAppCollectionDescription is the COLLECTION-phase placeholder body:
// open_app is indexed during collection (searchable, so the construction-time
// initialize-instruction count equals the final indexed catalog), but NO app
// view is installed yet at that point, so the placeholder names the tool's
// resolution contract WITHOUT enumerating an app inventory — an enumerated
// "Available apps: none" copy would violate the collection invariant and go
// stale on every assembly. The descriptor is only authoritative after
// materialization, where the post-surface hook swaps it for the enumerated
// openAppDescriptionFor copy; the placeholder is never materialized wire
// text.
const openAppCollectionDescription = "Resolve an app name to its ui:// view URI. The set of installed app views for this server is resolved at request time; each result names the app and its view, so list the apps installed on this host before opening one."

// newOpenAppDescriptor bakes the startup tools/list description resolved
// against the EFFECTIVE startup host profile, NOT a hard-coded MCP-Apps
// profile: a flat agent-only assembly marks open_app DirectVisible without
// FeatMCPApps, and baking the GUI copy would advertise iframe rendering the
// host never performs (headless copy whenever the startup host is not
// gui-capable). Per-request MCPTargets still re-resolve against the
// requesting profile.
func newOpenAppDescriptor(catalog *ToolCatalog, startup hostenv.PlatformProfile) model.ToolDescriptor {
	return openAppDescriptorWithDescription(catalog, openAppDescriptionFor(startup, catalog))
}

// newOpenAppCollectionPlaceholder builds the collection-phase open_app entry:
// identical shape (name, schema, handler, per-request profile-aware
// MCPTargets — those re-resolve post-installation at request time) but with a
// static, NON-enumerating description, because no app view exists yet to
// enumerate and the entry is only authoritative after the post-surface hook
// rebuilds it. Only the materialized descriptor may carry "Available apps:
// ..." text.
func newOpenAppCollectionPlaceholder(catalog *ToolCatalog) model.ToolDescriptor {
	return openAppDescriptorWithDescription(catalog, openAppCollectionDescription)
}

// openAppDescriptorWithDescription builds the open_app descriptor body shared
// by the materialized copy (enumerated description baked from the resolved
// profiles) and the collection-phase placeholder (non-enumerating).
func openAppDescriptorWithDescription(catalog *ToolCatalog, description string) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "open_app",
		Title:       "Open an app",
		Description: description,
		Category:    model.CategoryCore,
		MCPTargets:  openAppTargets(catalog),
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
