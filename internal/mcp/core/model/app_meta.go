package model

// MCP Apps (ext-apps) capability types. These are SDK-free: they describe the
// ui://-resource attachment for app tools/resources and the MCP Apps client
// capability, without importing the official MCP SDK (see internal/mcp/sdk).

// ToolVisibility is who may access an app tool. Mirrors ext-apps'
// McpUiToolVisibility: "model" = agent-callable, "app" = UI-callable only.
type ToolVisibility string

const (
	ToolVisibilityModel ToolVisibility = "model"
	ToolVisibilityApp   ToolVisibility = "app"
)

// AppToolMeta is the typed, SDK-neutral form of the `_meta.ui` block attached
// to an app tool. It is marshaled under `_meta.ui`; the resource URI is also
// written under the legacy flat key `_meta["ui/resourceUri"]` for older hosts.
type AppToolMeta struct {
	// ResourceURI is the ui:// URI of the HTML resource to render for this
	// tool. Required for an app tool.
	ResourceURI string
	// Visibility restricts who can call the tool. Defaults to both "model"
	// and "app" (everyone) when unset.
	Visibility []ToolVisibility
}

// AppResourceCSP is the per-resource Content-Security-Policy domain allowlist.
type AppResourceCSP struct {
	// ConnectDomains are origins allowed for network requests (fetch/XHR).
	ConnectDomains []string `json:"connectDomains,omitempty"`
	// ResourceDomains are origins allowed to load scripts, styles and images.
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	// FrameDomains are origins allowed to nest frames.
	FrameDomains []string `json:"frameDomains,omitempty"`
}

// AppResourceMeta is the typed, SDK-neutral form of the `_meta.ui` block on a
// ui:// resource (served on the resource list entry and the read result).
type AppResourceMeta struct {
	CSP *AppResourceCSP `json:"csp,omitempty"`
	// Domain is the exact HTTPS origin the widget layer attributes this view
	// to (ChatGPT app-directory check; one origin shared by all views of an
	// app, no path).
	Domain string `json:"domain,omitempty"`
	// WidgetDescription is the human-readable description directory surfaces
	// render for the widget. It is read from _meta.ui.widgetDescription by
	// directory submissions; the SDK also mirrors it under the OpenAI
	// compatibility alias _meta["openai/widgetDescription"].
	WidgetDescription string `json:"widgetDescription,omitempty"`
	PrefersBorder     *bool  `json:"prefersBorder,omitempty"`
}
