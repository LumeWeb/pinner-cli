package mcp

// This file pins the wire-level contract required by the MCP/Claude directory
// compatibility audits:
//
//   - Every tool carries annotations with all three behavior hints present as
//     booleans (readOnlyHint, destructiveHint, openWorldHint); a nil pointer
//     hint is emitted as null on the wire and fails validators.
//   - The progressive-disclosure meta-tools (search_tools/describe_tool/
//     invoke_tool) additionally carry top-level titles (the Claude directory
//     submission requires them; annotations.title is only a legacy fallback).
//   - auth_status's hints declare the platform-required values (an out-of-band
//     sign-in email cannot be unsent, so readOnly=false / destructive=true).
//   - Every ui:// app view declares one exact HTTPS origin (_meta.ui.domain
//     plus the openai/widgetDomain alias) and a widget description, shared by
//     all views of the app.
//   - The app-only upload helpers (ipfs_upload_submit / ipfs_upload_status)
//     carry openai/toolInvocation labels and fully described parameters.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// catalogHiddenHints are the JSON keys the directory validators require to be
// plain booleans on every tool's annotations.
var catalogHiddenHints = []string{"readOnlyHint", "destructiveHint", "openWorldHint"}

// requireBoolHints asserts the three behavior hints are booleans (the SDK
// emits readOnlyHint always; nil pointer hints become null and fail checks).
func requireBoolHints(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	require.NotNil(t, tool.Annotations, "%s: annotations present", tool.Name)
	raw, err := json.Marshal(tool.Annotations)
	require.NoError(t, err)
	var hints map[string]any
	require.NoError(t, json.Unmarshal(raw, &hints))
	for _, key := range catalogHiddenHints {
		_, ok := hints[key].(bool)
		require.True(t, ok, "%s: %s must be a boolean, got %v (%s)", tool.Name, key, hints[key], string(raw))
	}
}

// TestWireAnnotationsOnMetaTools pins titles + full boolean hint set on the
// progressive-disclosure meta-tools.
func TestWireAnnotationsOnMetaTools(t *testing.T) {
	catalog := NewToolCatalog()
	srv, err := OfficialServerFromCatalog(catalog, "instructions", false, nil, nil, nil)
	require.NoError(t, err)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	titles := map[string]string{
		"search_tools":  "Search tool catalog",
		"describe_tool": "Describe a catalog tool",
		"invoke_tool":   "Invoke a catalog tool",
	}
	seen := map[string]bool{}
	for _, tool := range res.Tools {
		requireBoolHints(t, tool)
		seen[tool.Name] = true
		want, ok := titles[tool.Name]
		if !ok {
			continue
		}
		require.Equal(t, want, tool.Title, "%s: top-level title required for directory submission", tool.Name)
		require.Equal(t, want, tool.Annotations.Title, "%s: annotations.title mirrors the title", tool.Name)
	}
	for name := range titles {
		require.True(t, seen[name], "%s must be listed", name)
	}
}

// TestAuthStatusAnnotationOverride pins the platform-required annotation
// corrections applied in catalogDescriptorToEntry: auth_status can trigger
// out-of-band sign-in communication (an email that cannot be unsent), so its
// hints must declare non-read and destructive rather than the SafetyRead
// defaults.
func TestAuthStatusAnnotationOverride(t *testing.T) {
	entry := catalogDescriptorToEntry(catalog.ToolDescriptor{
		Name:   "auth_status",
		Safety: catalog.SafetyRead,
	}, nil, nil)
	require.False(t, entry.ReadOnly, "auth_status must declare readOnlyHint=false (external out-of-band communication)")
	require.True(t, entry.Destructive, "auth_status must declare destructiveHint=true (a sent email cannot be unsent)")
	require.True(t, entry.OpenWorldHint, "auth_status classifies as open-world")

	// A sibling read op without an override keeps the Safety mapping. Reads
	// change no external state, so openWorldHint must stay false alongside
	// readOnlyHint=true (directory validators reject readOnly+openWorld).
	entry = catalogDescriptorToEntry(catalog.ToolDescriptor{
		Name:   "pins_list",
		Safety: catalog.SafetyRead,
	}, nil, nil)
	require.True(t, entry.ReadOnly, "pins_list keeps SafetyRead -> readOnlyHint=true")
	require.False(t, entry.OpenWorldHint, "pins_list is a read; openWorldHint must be false for read-only tools")
}

// requireSharedViewOrigin asserts every registered ui:// resource carries one
// exact HTTPS origin domain (no path) plus a widget description, on both the
// nested _meta.ui block and the openai/widget* alias keys.
func requireSharedViewOrigin(t *testing.T, r *mcp.Resource) {
	t.Helper()
	require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType, "%s: ui:// resource must advertise text/html;profile=mcp-app", r.URI)
	require.NotNil(t, r.Meta, "%s: resource must carry _meta", r.URI)
	ui, ok := r.Meta["ui"].(map[string]any)
	require.True(t, ok, "%s: _meta.ui block present", r.URI)
	domain, ok := ui["domain"].(string)
	require.True(t, ok, "%s: _meta.ui.domain must be a string, got %v", r.URI, ui["domain"])
	require.Regexp(t, `^https://[^/]+$`, domain, "%s: domain must be an exact HTTPS origin without a path", r.URI)
	_, ok = ui["widgetDescription"].(string)
	require.True(t, ok, "%s: _meta.ui.widgetDescription present", r.URI)
	require.Equal(t, domain, r.Meta["openai/widgetDomain"], "%s: openai/widgetDomain alias matches", r.URI)
}

// registerAccountAppServer builds a minimal server with the Account app
// registered, mirroring the production registration path.
func registerAccountAppServer(t *testing.T) *sdk.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:        auth.OpenAccountToolName,
		Title:       "Open Account",
		Description: "launcher",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler:     func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) { return model.ToolResult{}, nil },
	})
	srv := sdk.NewServer(nil)
	require.NoError(t, auth.RegisterAuthStatusApp(srv, catalog))
	return srv
}

// listAppResources lists the ui:// resources of srv via an in-memory client.
func listAppResources(t *testing.T, srv *sdk.Server) []*mcp.Resource {
	t.Helper()
	cs := connectOfficialClient(t, srv)
	res, err := cs.ListResources(context.Background(), nil)
	require.NoError(t, err)
	return res.Resources
}

// TestAppViewResourceMetadata pins the ChatGPT app-directory requirements on
// the ui://auth/status.html resource when the deployment resolves a public
// origin (hosted mode).
func TestAppViewResourceMetadata(t *testing.T) {
	previous := apps.ViewDomainResolver()
	apps.SetViewDomainResolver(func() string { return "https://mcp.example.com" })
	t.Cleanup(func() { apps.SetViewDomainResolver(previous) })

	found := false
	for _, r := range listAppResources(t, registerAccountAppServer(t)) {
		if r.URI == auth.AuthStatusAppURI {
			found = true
			requireSharedViewOrigin(t, r)
		}
	}
	require.True(t, found, "ui://auth/status.html listed in resources")
}

// TestAppViewNoDomainWithoutDeploymentOrigin pins that a self-hosted server
// (no view-domain resolver installed) emits NO domain at all — it must never
// advertise a foreign origin such as the hosted deployment's domain.
func TestAppViewNoDomainWithoutDeploymentOrigin(t *testing.T) {
	previous := apps.ViewDomainResolver()
	apps.SetViewDomainResolver(nil)
	t.Cleanup(func() { apps.SetViewDomainResolver(previous) })

	found := false
	for _, r := range listAppResources(t, registerAccountAppServer(t)) {
		if r.URI != auth.AuthStatusAppURI {
			continue
		}
		found = true
		require.NotNil(t, r.Meta, "%s: resource carries _meta", r.URI)
		ui, ok := r.Meta["ui"].(map[string]any)
		require.True(t, ok, "%s: _meta.ui block present", r.URI)
		require.NotContains(t, ui, "domain", "self-hosted view must carry no domain key")
		require.NotContains(t, r.Meta, "openai/widgetDomain", "self-hosted view must carry no domain alias")
	}
	require.True(t, found, "ui://auth/status.html listed in resources")
}

// wireSchemaBytes coerces the SDK's loosely typed Tool.InputSchema (any: it
// passes through json.RawMessage / []byte / string depending on registration
// path) into bytes for inspection.
func wireSchemaBytes(t *testing.T, v any) []byte {
	t.Helper()
	switch s := v.(type) {
	case json.RawMessage:
		return s
	case []byte:
		return s
	case string:
		return []byte(s)
	default:
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		return raw
	}
}

// TestToolDetailAlwaysEmitsBoolHints pins that describe_tool's ToolDetail JSON
// serializes readOnlyHint and destructiveHint as booleans ALWAYS — even when
// false — so a catalog consumer can distinguish a real false from a missing
// hint (rule 1: no null/missing on the catalog detail surface).
func TestToolDetailAlwaysEmitsBoolHints(t *testing.T) {
	for _, tc := range []struct {
		name        string
		readOnly    bool
		destructive bool
	}{
		{name: "mutate-tool", readOnly: false, destructive: false},
		{name: "read-tool", readOnly: true, destructive: false},
		{name: "destructive-tool", readOnly: false, destructive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := ToolDetail{
				Name:        "sample_tool",
				Title:       "Sample",
				Description: "description",
				Category:    model.CategoryCore,
				ReadOnly:    tc.readOnly,
				Destructive: tc.destructive,
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			}
			raw, err := json.Marshal(detail)
			require.NoError(t, err)
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(raw, &parsed))
			ro, hasRO := parsed["readOnlyHint"]
			require.True(t, hasRO, "readOnlyHint must be present even when false")
			roBool, ok := ro.(bool)
			require.True(t, ok, "readOnlyHint must be a boolean, got %T", ro)
			require.Equal(t, tc.readOnly, roBool)
			dh, hasDH := parsed["destructiveHint"]
			require.True(t, hasDH, "destructiveHint must be present even when false")
			dhBool, ok := dh.(bool)
			require.True(t, ok, "destructiveHint must be a boolean, got %T", dh)
			require.Equal(t, tc.destructive, dhBool)
		})
	}
}

// TestIPFSAppHelpersWireContract pins the directory requirements on the
// app-only upload helpers: boolean annotations, openai/toolInvocation labels,
// and non-empty descriptions on every params property.
func TestIPFSAppHelpersWireContract(t *testing.T) {
	srv, _ := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	byName := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		byName[x.Name] = x
	}

	for _, name := range []string{"ipfs_upload_submit", "ipfs_upload_status"} {
		tool, ok := byName[name]
		require.True(t, ok, "%s registered on the wire", name)
		requireBoolHints(t, tool)
		invocation, ok := tool.Meta["openai/toolInvocation"].(map[string]any)
		require.True(t, ok, "%s: openai/toolInvocation metadata present", name)
		require.NotEmpty(t, invocation["invoking"], "%s: present-tense invoking label", name)
		require.NotEmpty(t, invocation["invoked"], "%s: past-tense invoked label", name)
	}

	// Every property of both helpers' input schemas carries a description.
	for _, name := range []string{"ipfs_upload_submit", "ipfs_upload_status"} {
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		rawSchema := wireSchemaBytes(t, byName[name].InputSchema)
		require.NoError(t, json.Unmarshal(rawSchema, &schema), "%s schema parses", name)
		require.NotEmpty(t, schema.Properties, "%s declares properties", name)
		for pname, p := range schema.Properties {
			require.NotEmpty(t, p.Description, "%s.%s: non-empty description required", name, pname)
		}
	}
}
