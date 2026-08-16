package cloudflare

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildTokenTemplateURL verifies the deep-link contains the minimal tunnel
// scope (argotunnel:edit, dns:edit, zone:read), the account/zone scope params,
// and the URL-encoded token name.
func TestBuildTokenTemplateURL(t *testing.T) {
	u := BuildTokenTemplateURL("Pinner Tunnel")
	require.True(t, strings.HasPrefix(u, "https://dash.cloudflare.com/profile/api-tokens?"))

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	q := parsed.Query()

	permJSON := q.Get("permissionGroupKeys")
	require.Contains(t, permJSON, `"key":"argotunnel"`)
	require.Contains(t, permJSON, `"type":"edit"`)
	require.Contains(t, permJSON, `"key":"dns"`)
	require.Contains(t, permJSON, `"key":"zone"`)
	require.Contains(t, permJSON, `"type":"read"`)

	require.Equal(t, "*", q.Get("accountId"))
	require.Equal(t, "all", q.Get("zoneId"))
	require.Equal(t, "Pinner Tunnel", q.Get("name"))
}

// TestBuildTokenTemplateURLNoName verifies the name param is omitted when empty.
func TestBuildTokenTemplateURLNoName(t *testing.T) {
	u := BuildTokenTemplateURL("")
	require.False(t, strings.Contains(u, "name="))
}
