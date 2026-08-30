package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/oauthstore"
)

// testSecret is an arbitrary non-secret fixture value used only to exercise
// the OAuth flow in the test package. It is not a credential and never
// reaches production (the server secret always comes from the --auth-token
// flag at runtime).
const testSecret = "fixture-test-secret"

func newTestOAuth(t *testing.T) *OAuthServer {
	t.Helper()
	store, err := oauthstore.Open(filepath.Join(t.TempDir(), "oauth.db"), 30*24*time.Hour)
	require.NoError(t, err)
	o := NewOAuthServer(testSecret, "https://mcp.example.com", store)
	o.clients["cli"] = oauthClient{redirectURIs: []string{"http://localhost/cb"}}
	t.Cleanup(o.Stop) // stop the background reaper goroutine
	return o
}

func testPKCE() (verifier, challenge string) {
	verifier = "test-verifier-012345678901234567890123456789"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// allowTestServerHost adds the given test server's host to cimdAllowedHosts
// so resolveCIMDClient will fetch from it. Restored on cleanup.
func allowTestServerHost(t *testing.T, srvURL string) {
	t.Helper()
	u, err := url.Parse(srvURL)
	require.NoError(t, err)
	host := u.Hostname()
	orig := cimdAllowedHosts[host]
	cimdAllowedHosts[host] = true
	t.Cleanup(func() {
		if orig {
			cimdAllowedHosts[host] = orig
		} else {
			delete(cimdAllowedHosts, host)
		}
	})
}

func TestOAuthRegistration(t *testing.T) {
	o := newTestOAuth(t)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"client_name":"ChatGPT","redirect_uris":["http://localhost:1455/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`)
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", body)
	req.Header.Set("Content-Type", "application/json")
	o.RegisterHandler(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.NotEmpty(t, doc["client_id"])
	assert.Equal(t, "none", doc["token_endpoint_auth_method"])

	// Registered HTTPS callbacks are valid for hosted clients.
	assert.True(t, allowedClientRedirect("https://chatgpt.com/oauth/callback"))
	assert.False(t, allowedRedirect("https://chatgpt.com/oauth/callback"))
}

func TestOAuthASMetadata(t *testing.T) {
	o := newTestOAuth(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	o.AsMetadataHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, "https://mcp.example.com", doc["issuer"])
	assert.Equal(t, "https://mcp.example.com/oauth/authorize", doc["authorization_endpoint"])
	assert.Equal(t, "https://mcp.example.com/oauth/token", doc["token_endpoint"])
	assert.Equal(t, "authorization_code", doc["grant_types_supported"].([]any)[0])
}

func TestOAuthProtectedResourceMetadata(t *testing.T) {
	o := newTestOAuth(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	o.ProtectedResourceHandler("/mcp")(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, "https://mcp.example.com/mcp", doc["resource"])
	assert.Equal(t, []any{"https://mcp.example.com"}, doc["authorization_servers"])
}

func TestOAuthAuthorizeGET(t *testing.T) {
	o := newTestOAuth(t)
	_, challenge := testPKCE()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=cli&redirect_uri=http://localhost/cb&code_challenge="+challenge+"&code_challenge_method=S256&resource=https%3A%2F%2Fmcp.example.com%2Fmcp", nil)
	rec := httptest.NewRecorder()
	o.AuthorizeGET(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "auth secret")
	assert.Contains(t, body, "cli")
}

func TestOAuthAuthorizeGET_ReflectedXSSEscaped(t *testing.T) {
	o := newTestOAuth(t)
	_, challenge := testPKCE()
	// state is an opaque, unvalidated OAuth param that is echoed verbatim into
	// a hidden input. A malicious client driving a resource owner to this page
	// can set state to markup; it must render HTML-escaped so it cannot break
	// out of the hidden-input attribute. redirect_uri is left valid because it
	// is validated against the registered client.
	evil := `"><script>alert(1)</script>`
	u := "/oauth/authorize?response_type=code&client_id=cli" +
		"&redirect_uri=" + url.QueryEscape("http://localhost/cb") +
		"&code_challenge=" + challenge +
		"&code_challenge_method=S256" +
		"&state=" + url.QueryEscape(evil) +
		"&resource=https%3A%2F%2Fmcp.example.com%2Fmcp"
	rec := httptest.NewRecorder()
	o.AuthorizeGET(rec, httptest.NewRequest(http.MethodGet, u, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// The raw markup must not survive into the page: templ's attribute
	// escaping encodes both the quote and the angle brackets.
	assert.NotContains(t, body, "<script>alert(1)</script>")
	assert.NotContains(t, body, `"><script>`)
	// The hidden input must render the original value entity-escaped so that a
	// browser decodes it back to the exact state (XSS-safe AND lossless
	// round-trip: html.EscapeString escapes the quote, so the escaped text is
	// the original with each of its metacharacters entity-encoded).
	assert.Contains(t, body, `name="state" value="`+html.EscapeString(evil)+`"`)
	// Round-trip: decoding the entity-escaped attribute value yields the
	// original state the server must echo on POST.
	escaped := html.EscapeString(evil)
	assert.Equal(t, evil, html.UnescapeString(escaped))
}

func TestOAuthAuthorizeGET_InvalidCodeChallengeRejected(t *testing.T) {
	o := newTestOAuth(t)
	// A code_challenge that is not plain base64url (RFC 7636) must be rejected
	// so attacker markup cannot reach the authorize page.
	bad := `"><script>alert(1)</script>`
	u := "/oauth/authorize?response_type=code&client_id=cli" +
		"&redirect_uri=" + url.QueryEscape("http://localhost/cb") +
		"&code_challenge=" + url.QueryEscape(bad) +
		"&code_challenge_method=S256" +
		"&state=abc" +
		"&resource=https%3A%2F%2Fmcp.example.com%2Fmcp"
	rec := httptest.NewRecorder()
	o.AuthorizeGET(rec, httptest.NewRequest(http.MethodGet, u, nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOAuthFullFlow(t *testing.T) {
	o := newTestOAuth(t)

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

	// Authorization: wrong secret-as-password is rejected with a 401, and the
	// response is a branded retry form page (not a bare text/JSON body) so a
	// human who mistyped the shared secret can try again.
	rec = httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"client_id": "cli", "redirect_uri": "http://localhost/cb",
		"state": "st", "password": "wrong",
	}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "Invalid auth secret")
	assert.Contains(t, rec.Body.String(), "name=\"password\"")
	assert.Contains(t, rec.Body.String(), "action=")
	// The retry form must preserve the authorize request so resubmission works.
	assert.Contains(t, rec.Body.String(), `name="client_id"`)

	// Correct secret-as-password issues a PKCE-bound code and redirects.
	verifier, challenge := testPKCE()
	authValues := map[string]string{
		"response_type": "code", "client_id": "cli", "redirect_uri": "http://localhost/cb",
		"state": "st", "password": testSecret, "code_challenge": challenge,
		"code_challenge_method": "S256", "resource": "https://mcp.example.com/mcp",
	}
	rec = httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(authValues))
	assert.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, "st", loc.Query().Get("state"))
	assert.Equal(t, "http://localhost/cb", loc.Scheme+"://"+loc.Host+loc.Path)

	// A second authorization produces a different one-time code.
	rec = httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(authValues))
	loc2, _ := url.Parse(rec.Header().Get("Location"))
	code2 := loc2.Query().Get("code")
	require.NotEqual(t, code, code2)

	// Token endpoint: exchange the (single-use) code for an access token.
	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     "cli",
		"redirect_uri":  "http://localhost/cb",
		"code_verifier": verifier,
		"resource":      "https://mcp.example.com/mcp",
	}))
	assert.Equal(t, http.StatusOK, rec.Code)
	var tok map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&tok))
	access := tok["access_token"].(string)
	require.NotEmpty(t, access)
	assert.Equal(t, "Bearer", tok["token_type"])
	refresh := tok["refresh_token"].(string)
	require.NotEmpty(t, refresh)

	// Refresh token rotates into a new access token.
	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{
		"grant_type": "refresh_token", "refresh_token": refresh,
	}))
	assert.Equal(t, http.StatusOK, rec.Code)

	// The issued token authorizes the MCP endpoint and binds the transport session.
	var userID string
	bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := auth.TokenInfoFromContext(r.Context())
		require.NotNil(t, info)
		userID = info.UserID
		w.WriteHeader(http.StatusOK)
	}))

	// The official middleware rejects requests without a bearer token.
	rec = httptest.NewRecorder()
	bound.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")

	// Invalid or expired tokens cannot reach the MCP handler.
	rec = httptest.NewRecorder()
	bad = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	bad.Header.Set("Authorization", "Bearer notissued")
	bound.ServeHTTP(rec, bad)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")

	// A valid token authorizes the request and preserves the principal.
	rec = httptest.NewRecorder()
	good := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	good.Header.Set("Authorization", "Bearer "+access)
	bound.ServeHTTP(rec, good)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, tokenPrincipal(access), userID)
}

func TestOAuthTokenRejectsBadGrant(t *testing.T) {
	o := newTestOAuth(t)
	rec := httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{"grant_type": "authorization_code", "code": "nope"}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAllowedRedirect(t *testing.T) {
	for _, ok := range []string{
		"http://localhost/cb",
		"http://127.0.0.1:8080/cb",
		"http://localhost:3000/cb?x=1",
		"https://localhost/cb",
	} {
		assert.True(t, allowedRedirect(ok), "expected allowed: %s", ok)
	}
	for _, bad := range []string{
		"http://attacker.com/cb",
		"https://evil.example.net",
		"ftp://localhost/cb",
		"javascript:alert(1)",
		"not a url",
	} {
		assert.False(t, allowedRedirect(bad), "expected rejected: %s", bad)
	}
}

func TestOAuthRejectsCrossHostRedirect(t *testing.T) {
	o := newTestOAuth(t)
	rec := httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"client_id": "evil", "redirect_uri": "http://attacker.com/cb",
		"state": "st", "password": testSecret,
	}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NotContains(t, rec.Body.String(), "code=")
}

func TestOAuthReapExpired(t *testing.T) {
	o := newTestOAuth(t)
	o.tokenTTL = -time.Second // force expiry
	o.codeTTL = -time.Second
	o.clockSkew = 0            // disable grace for reaping test

	code := o.newCode(authorizationCode{clientID: "cli", expiry: time.Now().Add(-time.Second)})
	o.mu.Lock()
	o.tokens["expiredtok"] = time.Now().Add(-time.Second)
	o.mu.Unlock()

	o.reapLocked()

	o.mu.Lock()
	_, codeStill := o.codes[code]
	_, tokStill := o.tokens["expiredtok"]
	o.mu.Unlock()
	assert.False(t, codeStill, "expired code should have been reaped")
	assert.False(t, tokStill, "expired token should have been reaped")
	assert.False(t, o.validToken("expiredtok"))
	assert.False(t, o.validToken("nevertissued"))
}

func TestStaticBearerMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := StaticBearerMiddleware(testSecret, inner)

	// Missing token -> 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Wrong token -> 401.
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, bad)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Correct token -> 200.
	rec = httptest.NewRecorder()
	good := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	good.Header.Set("Authorization", "Bearer "+testSecret)
	h.ServeHTTP(rec, good)
	assert.Equal(t, http.StatusOK, rec.Code)
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

func TestASMetadataAdvertisesCIMD(t *testing.T) {
	o := newTestOAuth(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	o.AsMetadataHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, true, doc["client_id_metadata_document_supported"])
	assert.Contains(t, doc["token_endpoint_auth_methods_supported"].([]any), "none")
}

func TestIsCIMDClientID(t *testing.T) {
	tests := []struct {
		clientID string
		want     bool
	}{
		{"https://claude.ai/oauth/mcp-oauth-client-metadata", true},
		{"https://claude.ai/oauth/claude-code-client-metadata", true},
		{"https://vscode.dev/oauth/client-metadata.json", true},
		{"http://localhost:8080/.well-known/oauth-client-metadata", true},
		{"http://127.0.0.1:9090/metadata", true},
		{"client_abc123", false},
		{"http://localhost/oauth/metadata", true},
		{"http://attacker.com/metadata", false},
		{"https://example.com", false},
		{"https://example.com/", false},
		{"", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		t.Run(tt.clientID, func(t *testing.T) {
			assert.Equal(t, tt.want, isCIMDClientID(tt.clientID))
		})
	}
}

func TestMatchRedirectURI_LoopbackPortAgnostic(t *testing.T) {
	tests := []struct {
		name       string
		registered []string
		requested  string
		want       bool
	}{
		{
			name:       "loopback different port accepted",
			registered: []string{"http://localhost/callback"},
			requested:  "http://localhost:61264/callback",
			want:       true,
		},
		{
			name:       "loopback 127.0.0.1 different port",
			registered: []string{"http://127.0.0.1/callback"},
			requested:  "http://127.0.0.1:8080/callback",
			want:       true,
		},
		{
			name:       "loopback exact match",
			registered: []string{"http://localhost/callback"},
			requested:  "http://localhost/callback",
			want:       true,
		},
		{
			name:       "loopback different path rejected",
			registered: []string{"http://localhost/callback"},
			requested:  "http://localhost:8080/different",
			want:       false,
		},
		{
			name:       "non-loopback exact match required",
			registered: []string{"https://claude.ai/api/mcp/auth_callback"},
			requested:  "https://claude.ai/api/mcp/auth_callback",
			want:       true,
		},
		{
			name:       "non-loopback different host rejected",
			registered: []string{"https://claude.ai/api/mcp/auth_callback"},
			requested:  "https://evil.com/api/mcp/auth_callback",
			want:       false,
		},
		{
			name:       "loopback different scheme rejected",
			registered: []string{"http://localhost/callback"},
			requested:  "https://localhost:8080/callback",
			want:       false,
		},
		{
			name:       "empty registered",
			registered: []string{},
			requested:  "http://localhost/callback",
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchRedirectURI(tt.registered, tt.requested))
		})
	}
}

func TestCIMDFetchAndAuthorize(t *testing.T) {
	cimdClientID := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-client-metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "` + cimdClientID + `",
			"client_name": "Test Client",
			"redirect_uris": ["http://localhost/callback"],
			"grant_types": ["authorization_code", "refresh_token"],
			"response_types": ["code"],
			"token_endpoint_auth_method": "none"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdClientID = srv.URL + "/.well-known/oauth-client-metadata"

	o := newTestOAuth(t)

	_, challenge := testPKCE()
	authURL := "/oauth/authorize?response_type=code" +
		"&client_id=" + url.QueryEscape(cimdClientID) +
		"&redirect_uri=" + url.QueryEscape("http://localhost:9999/callback") +
		"&code_challenge=" + challenge +
		"&code_challenge_method=S256" +
		"&state=st" +
		"&resource=" + url.QueryEscape("https://mcp.example.com/mcp")

	rec := httptest.NewRecorder()
	o.AuthorizeGET(rec, httptest.NewRequest(http.MethodGet, authURL, nil))
	assert.Equal(t, http.StatusOK, rec.Code, "authorize GET should succeed for CIMD client")
	assert.Contains(t, rec.Body.String(), "auth secret")

	rec = httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"response_type":        "code",
		"client_id":            cimdClientID,
		"redirect_uri":         "http://localhost:9999/callback",
		"state":                "st",
		"password":             testSecret,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"resource":             "https://mcp.example.com/mcp",
	}))
	require.Equal(t, http.StatusFound, rec.Code, "authorize POST should redirect with code for CIMD client")
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, "st", loc.Query().Get("state"))
	assert.Equal(t, "http://localhost:9999/callback", loc.Scheme+"://"+loc.Host+loc.Path)

	o.mu.Lock()
	_, registered := o.clients[cimdClientID]
	o.mu.Unlock()
	assert.True(t, registered, "CIMD client should be registered in memory after authorize")
}

func TestCIMDCacheTTL(t *testing.T) {
	cimdClientID := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-client-metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "` + cimdClientID + `",
			"client_name": "Test Client",
			"redirect_uris": ["http://localhost/callback"],
			"token_endpoint_auth_method": "none"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdClientID = srv.URL + "/.well-known/oauth-client-metadata"

	o := newTestOAuth(t)

	_, err := o.resolveCIMDClient(cimdClientID)
	require.NoError(t, err)

	o.cimdCacheMu.Lock()
	entry, ok := o.cimdCache[cimdClientID]
	o.cimdCacheMu.Unlock()
	require.True(t, ok)
	assert.NotEmpty(t, entry.client.redirectURIs)

	entry.fetchedAt = time.Now().Add(-cimdCacheTTL - time.Second)
	o.cimdCacheMu.Lock()
	o.cimdCache[cimdClientID] = entry
	o.cimdCacheMu.Unlock()

	_, err = o.resolveCIMDClient(cimdClientID)
	require.NoError(t, err)
}

func TestCIMDRejectsClientIDMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "https://different-url.example.com/metadata",
			"client_name": "Spoofed",
			"redirect_uris": ["http://localhost/callback"],
			"token_endpoint_auth_method": "none"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdURL := srv.URL + "/metadata"

	o := newTestOAuth(t)
	_, err := o.resolveCIMDClient(cimdURL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestCIMDRejectsUnsupportedAuthMethod(t *testing.T) {
	cimdClientID := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "` + cimdClientID + `",
			"client_name": "Confidential",
			"redirect_uris": ["http://localhost/callback"],
			"token_endpoint_auth_method": "client_secret_basic"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdClientID = srv.URL + "/metadata"

	o := newTestOAuth(t)
	_, err := o.resolveCIMDClient(cimdClientID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported token_endpoint_auth_method")
}

func TestCIMDFullFlowThroughTokenExchange(t *testing.T) {
	cimdClientID := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "` + cimdClientID + `",
			"client_name": "Claude Code",
			"redirect_uris": ["http://localhost/callback", "http://127.0.0.1/callback"],
			"grant_types": ["authorization_code", "refresh_token"],
			"response_types": ["code"],
			"token_endpoint_auth_method": "none"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdClientID = srv.URL + "/metadata"

	o := newTestOAuth(t)
	verifier, challenge := testPKCE()
	const res = "https://mcp.example.com/mcp"

	rec := httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"response_type":        "code",
		"client_id":            cimdClientID,
		"redirect_uri":         "http://localhost:12345/callback",
		"state":                "st",
		"password":             testSecret,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"resource":             res,
	}))
	require.Equal(t, http.StatusFound, rec.Code, "CIMD authorize should succeed with loopback port mismatch")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     cimdClientID,
		"redirect_uri":  "http://localhost:12345/callback",
		"code_verifier": verifier,
		"resource":      res,
	}))
	require.Equal(t, http.StatusOK, rec.Code, "CIMD token exchange should succeed")
	var tok map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&tok))
	require.NotEmpty(t, tok["access_token"])
	assert.Equal(t, "Bearer", tok["token_type"])
	require.NotEmpty(t, tok["refresh_token"])
}

// TestOAuthRefreshReuseTolerated guards the Anthropic Claude failure mode: the
// connector can re-present the same refresh token while it persists the
// rotated successor. With the durable store's reuse-detection window, that
// re-presentation is accepted (not invalid_grant), so the session stays alive.
func TestOAuthRefreshReuseTolerated(t *testing.T) {
	o := newTestOAuth(t)
	verifier, challenge := testPKCE()

	// Complete an authorization-code flow to mint a refresh token.
	const res = "https://mcp.example.com/mcp"
	rec := httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"response_type": "code", "client_id": "cli", "redirect_uri": "http://localhost/cb",
		"password": testSecret, "code_challenge": challenge, "code_challenge_method": "S256", "resource": res,
	}))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")

	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{
		"grant_type": "authorization_code", "code": code, "client_id": "cli",
		"redirect_uri": "http://localhost/cb", "code_verifier": verifier, "resource": res,
	}))
	require.Equal(t, http.StatusOK, rec.Code)
	var tok map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&tok))
	refresh := tok["refresh_token"].(string)
	require.NotEmpty(t, refresh)

	// First refresh use rotates.
	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{"grant_type": "refresh_token", "refresh_token": refresh}))
	require.Equal(t, http.StatusOK, rec.Code, "first refresh must succeed")

	// Re-presenting the SAME refresh token immediately (within the reuse
	// window) must also succeed — this is what previously returned invalid_grant
	// and broke the Claude connection.
	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{"grant_type": "refresh_token", "refresh_token": refresh}))
	require.Equal(t, http.StatusOK, rec.Code, "benign refresh-token reuse within the window must not invalid_grant")
}

func TestAllowedCIMDHost(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"claude.ai allowlisted", "https://claude.ai/oauth/mcp-oauth-client-metadata", true},
		{"vscode.dev allowlisted", "https://vscode.dev/oauth/client-metadata.json", true},
		{"localhost not auto-allowed", "http://localhost:8080/metadata", false},
		{"127.0.0.1 not auto-allowed", "http://127.0.0.1:9090/metadata", false},
		{"unknown public host rejected", "https://evil.example.com/metadata", false},
		{"private IP rejected", "https://192.168.1.1/metadata", false},
		{"link-local rejected", "https://169.254.169.254/metadata", false},
		{"invalid url rejected", "not-a-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, allowedCIMDHost(tt.url))
		})
	}
}

func TestCIMDSSRFRejectsUnknownHost(t *testing.T) {
	origHosts := cimdAllowedHosts
	t.Cleanup(func() { cimdAllowedHosts = origHosts })
	cimdAllowedHosts = map[string]bool{}

	o := newTestOAuth(t)

	// 127.0.0.1 is still allowed as a loopback host even with an empty
	// allowlist, so block it explicitly to simulate an unknown external host.
	_, err := o.resolveCIMDClient("https://169.254.169.254/latest/meta-data")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowlisted")

	_, err = o.resolveCIMDClient("https://evil.example.com/metadata")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowlisted")
}

func TestCIMDCacheEvictionInReap(t *testing.T) {
	cimdClientID := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"client_id": "` + cimdClientID + `",
			"client_name": "Test",
			"redirect_uris": ["http://localhost/callback"],
			"token_endpoint_auth_method": "none"
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	allowTestServerHost(t, srv.URL)

	cimdClientID = srv.URL + "/metadata"

	o := newTestOAuth(t)
	cimdClient, err := o.resolveCIMDClient(cimdClientID)
	require.NoError(t, err)

	// Simulate what validateAuthorizeRequest does: register the CIMD client
	// transiently in o.clients.
	o.mu.Lock()
	o.clients[cimdClientID] = cimdClient
	o.mu.Unlock()

	o.cimdCacheMu.Lock()
	assert.NotEmpty(t, o.cimdCache)
	o.cimdCacheMu.Unlock()
	o.mu.Lock()
	_, registered := o.clients[cimdClientID]
	o.mu.Unlock()
	assert.True(t, registered)

	// Expire the cache entry and reap.
	o.cimdCacheMu.Lock()
	e := o.cimdCache[cimdClientID]
	e.fetchedAt = time.Now().Add(-cimdCacheTTL - time.Second)
	o.cimdCache[cimdClientID] = e
	o.cimdCacheMu.Unlock()

	o.mu.Lock()
	o.reapLocked()
	o.mu.Unlock()

	o.cimdCacheMu.Lock()
	_, cachedStill := o.cimdCache[cimdClientID]
	o.cimdCacheMu.Unlock()
	assert.False(t, cachedStill, "expired CIMD cache entry should be reaped")

	o.mu.Lock()
	_, registeredStill := o.clients[cimdClientID]
	o.mu.Unlock()
	assert.False(t, registeredStill, "expired CIMD client registration should be reaped")
}
