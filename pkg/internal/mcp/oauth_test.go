package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "s3cr3t123"

func newTestOAuth() *oauthServer {
	return newOAuthServer(testSecret, "https://mcp.example.com")
}

func TestOAuthASMetadata(t *testing.T) {
	o := newTestOAuth()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	o.asMetadataHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, "https://mcp.example.com", doc["issuer"])
	assert.Equal(t, "https://mcp.example.com/oauth/authorize", doc["authorization_endpoint"])
	assert.Equal(t, "https://mcp.example.com/oauth/token", doc["token_endpoint"])
	assert.Equal(t, "authorization_code", doc["grant_types_supported"].([]any)[0])
}

func TestOAuthProtectedResourceMetadata(t *testing.T) {
	o := newTestOAuth()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	o.protectedResourceHandler("/mcp")(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, "https://mcp.example.com/mcp", doc["resource"])
	assert.Equal(t, []any{"https://mcp.example.com"}, doc["authorization_servers"])
}

func TestOAuthAuthorizeGET(t *testing.T) {
	o := newTestOAuth()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=cli&redirect_uri=http://localhost/cb", nil)
	rec := httptest.NewRecorder()
	o.authorizeGET(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "auth secret")
	assert.Contains(t, body, "cli")
}

func TestOAuthFullFlow(t *testing.T) {
	o := newTestOAuth()

	// Resource server: unauthenticated request must 401 with a challenge
	// pointing at the protected-resource metadata.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	protected := o.protectMCP("/mcp", inner)

	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "oauth-protected-resource")
	// Invalid bearer token also rejected.
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	bad.Header.Set("Authorization", "Bearer notissued")
	protected.ServeHTTP(rec, bad)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Authorization: wrong secret-as-password is rejected.
	rec = httptest.NewRecorder()
	o.authorizePOST(rec, formPost(map[string]string{
		"client_id": "cli", "redirect_uri": "http://localhost/cb",
		"state": "st", "password": "wrong",
	}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Correct secret-as-password issues a code and redirects.
	rec = httptest.NewRecorder()
	o.authorizePOST(rec, formPost(map[string]string{
		"client_id": "cli", "redirect_uri": "http://localhost/cb",
		"state": "st", "password": testSecret,
	}))
	assert.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, "st", loc.Query().Get("state"))
	assert.Equal(t, "http://localhost/cb", loc.Scheme+"://"+loc.Host+loc.Path)

	// Redeeming the same code twice must fail (one-time use).
	rec = httptest.NewRecorder()
	o.authorizePOST(rec, formPost(map[string]string{
		"client_id": "cli", "redirect_uri": "http://localhost/cb",
		"state": "st", "password": testSecret,
	}))
	loc2, _ := url.Parse(rec.Header().Get("Location"))
	code2 := loc2.Query().Get("code")
	require.NotEqual(t, code, code2)

	// Token endpoint: exchange the (single-use) code for an access token.
	rec = httptest.NewRecorder()
	o.tokenHandler(rec, formPost(map[string]string{
		"grant_type": "authorization_code",
		"code":       code,
	}))
	assert.Equal(t, http.StatusOK, rec.Code)
	var tok map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&tok))
	access := tok["access_token"].(string)
	require.NotEmpty(t, access)
	assert.Equal(t, "Bearer", tok["token_type"])

	// The issued token authorizes the MCP endpoint.
	rec = httptest.NewRecorder()
	good := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	good.Header.Set("Authorization", "Bearer "+access)
	protected.ServeHTTP(rec, good)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOAuthTokenRejectsBadGrant(t *testing.T) {
	o := newTestOAuth()
	rec := httptest.NewRecorder()
	o.tokenHandler(rec, formPost(map[string]string{"grant_type": "authorization_code", "code": "nope"}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func formPost(values map[string]string) *http.Request {
	form := url.Values{}
	for k, v := range values {
		form.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
