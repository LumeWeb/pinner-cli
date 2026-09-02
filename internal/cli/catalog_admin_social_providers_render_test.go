package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/portal-sdk/admin"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// TestRenderAdminSocialProviderResult verifies renderAdminResult handles the
// *admin.SocialProvider type returned by the admin_social_providers create,
// get, update, enable and disable operations. Without a case it fell through
// to the default "unroutable result type" error, breaking every human-readable
// invocation of those commands.
func TestRenderAdminSocialProviderResult(t *testing.T) {
	provider := &admin.SocialProvider{}
	provider.Id = 3
	provider.ProviderId = "google"
	provider.DisplayName = "Google"
	provider.Enabled = true
	provider.OrderIndex = 2
	provider.ClientId = "client-abc"
	provider.Scopes = []string{"openid", "email", "profile"}

	op := catalog.NewOperation(catalog.OperationSpec{Name: catalogops.OpAdminSocialProvidersCreate})

	t.Run("renders provider as a field group", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := &cli.Command{
			Name:   "create",
			Writer: &buf,
			Action: func(ctx context.Context, c *cli.Command) error {
				return renderAdminResult(ctx, c, op, provider)
			},
		}

		require.NoError(t, cmd.Run(t.Context(), []string{"create"}))
		got := buf.String()
		for _, want := range []string{
			"Social provider",
			"google",
			"client-abc",
			"openid",
		} {
			assert.Contains(t, got, want)
		}
	})

	t.Run("renders JSON when --json is set", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := &cli.Command{
			Name:   "create",
			Writer: &buf,
			Flags:  []cli.Flag{&cli.BoolFlag{Name: FlagJSON}},
			Action: func(ctx context.Context, c *cli.Command) error {
				return renderAdminResult(ctx, c, op, provider)
			},
		}

		require.NoError(t, cmd.Run(t.Context(), []string{"create", "--json"}))
		got := buf.String()
		assert.Contains(t, got, `"provider_id": "google"`)
		assert.Contains(t, got, `"client_id": "client-abc"`)
		assert.Contains(t, got, `"display_name": "Google"`)
	})
}

// TestRenderAdminSocialProvidersDeleteResult verifies renderAdminResult handles
// the *catalogops.SocialProvidersDeleteResult type returned by
// admin_social_providers_delete.
func TestRenderAdminSocialProvidersDeleteResult(t *testing.T) {
	var buf bytes.Buffer
	op := catalog.NewOperation(catalog.OperationSpec{Name: catalogops.OpAdminSocialProvidersDelete})

	cmd := &cli.Command{
		Name:   "delete",
		Writer: &buf,
		Action: func(ctx context.Context, c *cli.Command) error {
			return renderAdminResult(ctx, c, op, &catalogops.SocialProvidersDeleteResult{Deleted: true, ID: "7"})
		},
	}

	require.NoError(t, cmd.Run(t.Context(), []string{"delete"}))
	assert.Contains(t, buf.String(), "Social provider 7 deleted")
}

// TestResolveSocialProviderID verifies numeric IDs pass through and provider
// keys resolve against the configured provider list.
func TestResolveSocialProviderID(t *testing.T) {
	t.Run("numeric id passes through", func(t *testing.T) {
		got, err := resolveSocialProviderID(t.Context(), catalogops.AdminDeps{}, "12")
		require.NoError(t, err)
		assert.Equal(t, "12", got)
	})

	t.Run("unwired deps error clearly on key", func(t *testing.T) {
		_, err := resolveSocialProviderID(t.Context(), catalogops.AdminDeps{}, "google")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "social provider service unavailable")
	})
}
