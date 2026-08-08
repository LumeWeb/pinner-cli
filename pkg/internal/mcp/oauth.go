package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// oauthServer is a deliberately minimal, self-contained OAuth 2.1-shaped
// authorization server for the MCP HTTP transport. It exists so MCP clients
// that REQUIRE an OAuth handshake (ChatGPT, Claude.ai, Microsoft Copilot,
// Google Vertex) can discover and complete an authorization-code flow without
// pinner depending on an external identity provider.
//
// It is intentionally a "dummy" AS: the only credential is the shared --auth-token
// secret, which the resource owner enters on the login page as the password.
// Client registrations and issued tokens are in-memory. The authorization
// code flow enforces S256 PKCE and RFC 8707 resource binding; the shared secret
// remains the only user credential.
type oauthServer struct {
	mu      sync.Mutex
	secret  []byte
	issuer  string
	baseURL string

	// registered clients keyed by client ID
	clients map[string]oauthClient
	// authorization codes, one-time use
	codes map[string]authorizationCode
	// access and refresh tokens issued to authenticated clients
	tokens        map[string]time.Time // access token -> expiry
	refreshTokens map[string]time.Time // refresh token -> expiry
	tokenTTL      time.Duration
	refreshTTL    time.Duration
	codeTTL       time.Duration
	// done stops the background reaper.
	done      chan struct{}
	closeOnce sync.Once
}

type oauthClient struct {
	redirectURIs []string
}

type authorizationCode struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	resource            string
	expiry              time.Time
}

func newOAuthServer(secret, baseURL string) *oauthServer {
	o := &oauthServer{
		secret:        []byte(secret),
		issuer:        baseURL,
		baseURL:       baseURL,
		clients:       make(map[string]oauthClient),
		codes:         make(map[string]authorizationCode),
		tokens:        make(map[string]time.Time),
		refreshTokens: make(map[string]time.Time),
		tokenTTL:      time.Hour,
		refreshTTL:    30 * 24 * time.Hour,
		codeTTL:       10 * time.Minute,
		done:          make(chan struct{}),
	}
	// Periodic reaper so long-running public tunnels do not grow the maps
	// without bound as clients rotate and codes go unredeemed.
	go o.sweep()
	return o
}

// Stop terminates the background reaper. It is safe to call multiple times.
func (o *oauthServer) Stop() {
	o.closeOnce.Do(func() { close(o.done) })
}

// sweep periodically drops expired tokens and codes.
func (o *oauthServer) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			o.reapLocked()
		case <-o.done:
			return
		}
	}
}

// reapLocked removes expired tokens and codes. Caller must hold o.mu.
func (o *oauthServer) reapLocked() {
	now := time.Now()
	for tok, exp := range o.tokens {
		if now.After(exp) {
			delete(o.tokens, tok)
		}
	}
	for tok, exp := range o.refreshTokens {
		if now.After(exp) {
			delete(o.refreshTokens, tok)
		}
	}
	for code, e := range o.codes {
		if now.After(e.expiry) {
			delete(o.codes, code)
		}
	}
}

// asMetadataHandler serves the OAuth 2.0 Authorization Server Metadata
// (RFC 8414) so clients can find the authorize and token endpoints.
func (o *oauthServer) asMetadataHandler(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"issuer":                                o.issuer,
		"authorization_endpoint":                o.baseURL + "/oauth/authorize",
		"token_endpoint":                        o.baseURL + "/oauth/token",
		"registration_endpoint":                 o.baseURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": false,
		"scopes_supported":                      []string{"offline_access"},
	}
	writeJSON(w, http.StatusOK, doc)
}

// protectedResourceHandler serves the OAuth 2.0 Protected Resource Metadata
// (RFC 9728), pointing clients at this server's authorization server.
func (o *oauthServer) protectedResourceHandler(mcpPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]any{
			"resource":                 o.baseURL + mcpPath,
			"authorization_servers":    []string{o.issuer},
			"bearer_methods_supported": []string{"header"},
			"scopes_supported":         []string{},
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

// authorizeGET renders the login page.
func (o *oauthServer) authorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := o.validateAuthorizeRequest(q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{
		"Action":              o.baseURL + "/oauth/authorize",
		"ResponseType":        q.Get("response_type"),
		"ClientID":            q.Get("client_id"),
		"RedirectURI":         q.Get("redirect_uri"),
		"State":               q.Get("state"),
		"CodeChallenge":       q.Get("code_challenge"),
		"CodeChallengeMethod": q.Get("code_challenge_method"),
		"Resource":            q.Get("resource"),
	})
}

func (o *oauthServer) validateAuthorizeRequest(q url.Values) error {
	if q.Get("response_type") != "code" {
		return fmt.Errorf("response_type must be code")
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		return fmt.Errorf("missing client_id or redirect_uri")
	}
	o.mu.Lock()
	client, ok := o.clients[clientID]
	o.mu.Unlock()
	if !ok || !contains(client.redirectURIs, redirectURI) {
		return fmt.Errorf("unregistered client or redirect_uri")
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return fmt.Errorf("S256 PKCE is required")
	}
	if q.Get("resource") != o.baseURL+"/mcp" {
		return fmt.Errorf("invalid resource")
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (o *oauthServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		ApplicationType         string   `json:"application_type"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.RedirectURIs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	for _, redirectURI := range request.RedirectURIs {
		if !allowedClientRedirect(redirectURI) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
			return
		}
	}
	if request.TokenEndpointAuthMethod != "" && request.TokenEndpointAuthMethod != "none" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	clientID := "client_" + newToken(16)
	o.mu.Lock()
	o.clients[clientID] = oauthClient{redirectURIs: request.RedirectURIs}
	o.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_name":                request.ClientName,
		"redirect_uris":              request.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authorizePOST verifies the secret-as-password and issues an authorization
// code, then redirects the client to its redirect_uri.
func (o *oauthServer) authorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	password := r.PostFormValue("password")
	if subtle.ConstantTimeCompare([]byte(password), o.secret) != 1 {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	if err := o.validateAuthorizeRequest(r.PostForm); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	code := o.newCode(authorizationCode{
		clientID:            r.PostFormValue("client_id"),
		redirectURI:         r.PostFormValue("redirect_uri"),
		codeChallenge:       r.PostFormValue("code_challenge"),
		codeChallengeMethod: r.PostFormValue("code_challenge_method"),
		resource:            r.PostFormValue("resource"),
		expiry:              time.Now().Add(o.codeTTL),
	})
	redirectURI := r.PostFormValue("redirect_uri")
	params := url.Values{"code": {code}}
	if state := r.PostFormValue("state"); state != "" {
		params.Set("state", state)
	}
	loc := redirectURI + "?" + params.Encode()
	if strings.Contains(redirectURI, "?") {
		loc = redirectURI + "&" + params.Encode()
	}
	http.Redirect(w, r, loc, http.StatusFound)
}

// allowedRedirect reports whether redirect_uri is a loopback callback that a
// public OAuth client (MCP desktop/mobile) is legitimately allowed to use.
// Only http(s) with a loopback host is accepted; any cross-origin host is
// rejected, which is what prevents code exfiltration in the absence of a
// client registry.
func allowedRedirect(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// allowedClientRedirect permits HTTPS callbacks advertised by registered web
// clients, while native clients remain limited to loopback HTTP callbacks.
func allowedClientRedirect(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	if err != nil || u.Hostname() == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return allowedRedirect(redirectURI)
}

// tokenHandler exchanges an authorization code or refresh token for tokens.
func (o *oauthServer) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		if err := o.exchangeCode(w, r); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": err.Error()})
		}
	case "refresh_token":
		o.exchangeRefreshToken(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (o *oauthServer) exchangeCode(w http.ResponseWriter, r *http.Request) error {
	code := r.PostFormValue("code")
	clientID := r.PostFormValue("client_id")
	redirectURI := r.PostFormValue("redirect_uri")
	verifier := r.PostFormValue("code_verifier")
	resource := r.PostFormValue("resource")
	o.mu.Lock()
	entry, ok := o.codes[code]
	o.mu.Unlock()
	if !ok || time.Now().After(entry.expiry) {
		return fmt.Errorf("invalid or expired authorization code")
	}
	if clientID != entry.clientID || redirectURI != entry.redirectURI || resource != entry.resource || !verifyPKCE(verifier, entry.codeChallenge) {
		return fmt.Errorf("invalid client, redirect_uri, code_verifier, or resource")
	}
	// Consume only after validation. Recheck under the lock so concurrent
	// redemption can succeed only once.
	o.mu.Lock()
	if _, ok := o.codes[code]; !ok {
		o.mu.Unlock()
		return fmt.Errorf("authorization code already used")
	}
	delete(o.codes, code)
	o.mu.Unlock()
	pair := o.newTokens()
	o.storeTokens(pair)
	issueTokens(w, pair)
	return nil
}

func (o *oauthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	refresh := r.PostFormValue("refresh_token")
	o.mu.Lock()
	expiry, ok := o.refreshTokens[refresh]
	if ok {
		delete(o.refreshTokens, refresh) // rotate refresh tokens on use
		if time.Now().After(expiry) {
			ok = false
		}
	}
	o.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	pair := o.newTokens()
	o.storeTokens(pair)
	issueTokens(w, pair)
}

func (o *oauthServer) newTokens() tokenPair {
	return tokenPair{access: newToken(32), refresh: newToken(32)}
}

type tokenPair struct {
	access  string
	refresh string
}

func (o *oauthServer) storeTokens(pair tokenPair) {
	now := time.Now()
	o.mu.Lock()
	o.tokens[pair.access] = now.Add(o.tokenTTL)
	o.refreshTokens[pair.refresh] = now.Add(o.refreshTTL)
	o.mu.Unlock()
}

func issueTokens(w http.ResponseWriter, pair tokenPair) {
	// Storage is performed by the caller's oauthServer before this response.
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.access,
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": pair.refresh,
	})
}

func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (o *oauthServer) newCode(entry authorizationCode) string {
	code := newToken(24)
	o.mu.Lock()
	o.codes[code] = entry
	o.reapLocked()
	o.mu.Unlock()
	return code
}

// validToken reports whether the given bearer token is one this AS issued
// and has not expired. It does a single-map lookup only; full-map cleanup of
// expired entries is deferred to the periodic sweep (see sweep/reapLocked),
// so the per-request cost stays O(1). The mutex is still held for the single
// lookup (removing it would race with writers in tokenHandler/newCode/sweep).
func (o *oauthServer) validToken(tok string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	exp, ok := o.tokens[tok]
	if ok && time.Now().After(exp) {
		delete(o.tokens, tok)
		ok = false
	}
	return ok
}

// protectMCP wraps the MCP resource-server handler with OAuth bearer-token
// validation. Unauthenticated and invalid requests get a 401 with an RFC 9728
// WWW-Authenticate challenge pointing at the protected-resource metadata, so
// OAuth-capable MCP clients can discover and complete the flow.
func (o *oauthServer) protectMCP(mcpPath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token != "" && o.validToken(token) {
			next.ServeHTTP(w, r)
			return
		}
		deny(w, fmt.Sprintf(
			`Bearer resource_metadata="%s/.well-known/oauth-protected-resource", error="invalid_token", error_description="OAuth authorization required"`,
			o.baseURL))
	})
}

// deny writes an HTTP 401 with a WWW-Authenticate challenge. It is shared by
// the OAuth resource-server path (protectMCP) and the static-bearer path
// (beforeAuthorization) so both reject unauthorized access the same way.
func deny(w http.ResponseWriter, authenticate string) {
	w.Header().Set("WWW-Authenticate", authenticate)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token", "error_description": "unauthorized"})
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

func newToken(nbytes int) string {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var tmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorize pinner MCP</title></head>
<body style="font-family:system-ui,sans-serif;max-width:24rem;margin:4rem auto;padding:0 1rem">
  <h1>Authorize pinner MCP access</h1>
  <p>Client <code>{{.ClientID}}</code> is requesting access to this MCP server
  which executes CLI tools in-process. Enter the shared auth secret to authorize.</p>
  <form method="post" action="{{.Action}}">
    <input type="hidden" name="response_type" value="{{.ResponseType}}">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <input type="hidden" name="resource" value="{{.Resource}}">
    <label for="password">Auth secret</label>
    <input type="password" id="password" name="password" required autofocus autocomplete="current-password">
    <button type="submit">Authorize</button>
  </form>
</body>
</html>`))
