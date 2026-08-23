package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	mcpauth "go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// newAuthSSOAppServer builds an official server with auth_sso/auth_resume in
// the catalog and the Sign In app registered via the AppView lib layer.
func newAuthSSOAppServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)

	authSSO := mcpauth.NewAuthSSODescriptor(newHubOOBForTest(t), handles, reg)
	authSSO.DirectVisible = true
	authResume := mcpauth.NewAuthResumeDescriptor(reg, handles)
	authResume.DirectVisible = true
	catalog.Add(model.ToolEntryFromDescriptor(authSSO))
	catalog.Add(model.ToolEntryFromDescriptor(authResume))

	// Seed the launcher; the app's AttachTo now points at open_sso_signin.
	seedLauncherForTest(t, srv, catalog, mcpauth.OpenSSOSigninToolName, mcpauth.AuthSSOAppURI, model.CategoryAccount)
	if err := mcpauth.RegisterAuthSSOApp(srv, catalog, reg, handles); err != nil {
		t.Fatalf("mcpauth.RegisterAuthSSOApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

// TestRegisterAuthSSOAppWire verifies the Sign In app registers its ui://
// resource, attaches _meta.ui to the auth_sso tool, and registers the app-only
// auth_sso_status helper.
func TestRegisterAuthSSOAppWire(t *testing.T) {
	srv := newAuthSSOAppServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Resource listed with the mcp-app MIME type.
	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	var found bool
	for _, r := range res.Resources {
		if r.URI == mcpauth.AuthSSOAppURI {
			found = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "auth sso resource not listed")

	// Read returns the rendered sign-in document.
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: mcpauth.AuthSSOAppURI})
	require.NoError(t, err)
	require.Equal(t, apps.RESOURCE_MIME_TYPE, rr.Contents[0].MIMEType)
	require.Contains(t, rr.Contents[0].Text, "Sign In")
	require.Contains(t, rr.Contents[0].Text, "sso-start")

	// auth_sso is a headless primitive (no ui.resourceUri); open_sso_signin is
	// the ONLY tool carrying resourceUri; auth_sso_status is app-only.
	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	requireHeadlessNoUI(t, toolMeta["auth_sso"])
	requireLauncherUI(t, toolMeta["open_sso_signin"], mcpauth.AuthSSOAppURI)

	status := toolMeta["auth_sso_status"]
	require.NotNil(t, status, "auth_sso_status helper not listed")
	sui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on auth_sso_status")
	require.Equal(t, mcpauth.AuthSSOAppURI, sui["resourceUri"])
	require.Contains(t, sui["visibility"], "app", "auth_sso_status should be app-only")
}
