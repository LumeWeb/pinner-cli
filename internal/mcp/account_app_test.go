package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// newAccountAppsServer builds an official server with the account credential
// tools in the catalog and the Change Password / Change Email apps registered
// via the AppView lib layer.
func newAccountAppsServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)

	oob := NewOOBAccountChange(&testAccountAuthService{}, DefaultAccountChangeTTL)
	oob.SetBaseURL("http://127.0.0.1:9999")

	update := NewAccountPasswordUpdateDescriptor(oob, oob.svc, nil, nil)
	update.DirectVisible = true
	email := NewAccountEmailChangeDescriptor(oob, oob.svc)
	email.DirectVisible = true
	reset := NewAccountPasswordResetDescriptor(oob.svc, "https://web.example")
	reset.DirectVisible = true
	catalog.Add(model.ToolEntryFromDescriptor(update))
	catalog.Add(model.ToolEntryFromDescriptor(email))
	catalog.Add(model.ToolEntryFromDescriptor(reset))

	require.NoError(t, RegisterAccountPasswordApp(srv, catalog), "RegisterAccountPasswordApp")
	require.NoError(t, RegisterAccountEmailApp(srv, catalog), "RegisterAccountEmailApp")
	require.NoError(t, RegisterOfficialCuratedTools(srv, catalog), "RegisterOfficialCuratedTools")
	return srv
}

// TestRegisterAccountAppsWire verifies both account apps register their ui://
// resource, read back the rendered document, and attach _meta.ui to the
// account_password_update / account_email_change tools.
func TestRegisterAccountAppsWire(t *testing.T) {
	srv := newAccountAppsServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	found := map[string]bool{}
	for _, r := range res.Resources {
		if r.URI == AccountPasswordAppURI || r.URI == AccountEmailAppURI {
			found[r.URI] = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found[AccountPasswordAppURI], "password resource not listed")
	require.True(t, found[AccountEmailAppURI], "email resource not listed")

	// Read returns each rendered document with its view's element ids.
	pw, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: AccountPasswordAppURI})
	require.NoError(t, err)
	require.Contains(t, pw.Contents[0].Text, "Change Password")
	require.Contains(t, pw.Contents[0].Text, "pw-start")

	em, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: AccountEmailAppURI})
	require.NoError(t, err)
	require.Contains(t, em.Contents[0].Text, "Change Email")
	require.Contains(t, em.Contents[0].Text, "em-start")

	// Both start tools carry _meta.ui.resourceUri to their respective views.
	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	meta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		meta[x.Name] = x
	}
	pwt, ok := meta["account_password_update"]
	require.True(t, ok, "account_password_update not listed")
	pwUI, ok := pwt.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on account_password_update")
	require.Equal(t, AccountPasswordAppURI, pwUI["resourceUri"])

	emt, ok := meta["account_email_change"]
	require.True(t, ok, "account_email_change not listed")
	emUI, ok := emt.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on account_email_change")
	require.Equal(t, AccountEmailAppURI, emUI["resourceUri"])
}
