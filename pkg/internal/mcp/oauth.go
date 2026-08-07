package mcp

import (
	"crypto/rand"
	"crypto/subtle"
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
// No user database, no PKCE/DCR enforcement, no refresh tokens. The result is a
// standards-shaped OAuth exchange that any compliant MCP client can complete,
// backed by exactly one secret.
type oauthServer struct {
	mu      sync.Mutex
	secret  []byte
	issuer  string
	baseURL string

	// authorization codes, one-time use
	codes map[string]expiring // code -> clientID + expiry
	// access tokens issued to authenticated clients
	tokens   map[string]time.Time // token -> expiry
	tokenTTL time.Duration
	codeTTL  time.Duration
	// done stops the background reaper.
	done      chan struct{}
	closeOnce sync.Once
}

// expiring is a code entry with its issuance expiry so codes can be reaped.
type expiring struct {
	clientID string
	expiry   time.Time
}

func newOAuthServer(secret, baseURL string) *oauthServer {
	o := &oauthServer{
		secret:   []byte(secret),
		issuer:   baseURL,
		baseURL:  baseURL,
		codes:    make(map[string]expiring),
		tokens:   make(map[string]time.Time),
		tokenTTL: time.Hour,
		codeTTL:  10 * time.Minute,
		done:     make(chan struct{}),
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
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"plain", "S256"},
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
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" {
		http.Error(w, "missing client_id", http.StatusBadRequest)
		return
	}
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, map[string]any{
		"Action":        o.baseURL + "/oauth/authorize",
		"ClientID":      clientID,
		"RedirectURI":   redirectURI,
		"State":         q.Get("state"),
		"CodeChallenge": q.Get("code_challenge"),
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
	clientID := r.PostFormValue("client_id")
	redirectURI := r.PostFormValue("redirect_uri")
	state := r.PostFormValue("state")

	// The only credential: the shared auth secret entered as a password.
	if subtle.ConstantTimeCompare([]byte(password), o.secret) != 1 {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	if clientID == "" || redirectURI == "" {
		http.Error(w, "missing client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	// Reject cross-host redirects. Without a client registry, only loopback
	// callback URIs are allowed so an attacker cannot lure the resource owner
	// into submitting the secret and exfiltrate the code to their own host.
	if !allowedRedirect(redirectURI) {
		http.Error(w, "redirect_uri must be a loopback callback", http.StatusBadRequest)
		return
	}

	code := o.newCode(clientID)
	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	loc := fmt.Sprintf("%s%scode=%s&state=%s", redirectURI, sep, code, state)
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

// tokenHandler exchanges an authorization code for an access token.
func (o *oauthServer) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	code := r.PostFormValue("code")
	grantType := r.PostFormValue("grant_type")
	if grantType != "authorization_code" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}

	o.mu.Lock()
	entry, ok := o.codes[code]
	if ok {
		delete(o.codes, code) // one-time use
		if time.Now().After(entry.expiry) {
			ok = false // expired
		}
	}
	o.reapLocked()
	o.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}

	token := newToken(32)
	expiry := time.Now().Add(o.tokenTTL)
	o.mu.Lock()
	o.tokens[token] = expiry
	o.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(o.tokenTTL.Seconds()),
	})
}

func (o *oauthServer) newCode(clientID string) string {
	code := newToken(24)
	o.mu.Lock()
	o.codes[code] = expiring{clientID: clientID, expiry: time.Now().Add(o.codeTTL)}
	o.reapLocked()
	o.mu.Unlock()
	return code
}

// validToken reports whether the given bearer token is one this AS issued
// and has not expired. It does a single-map lookup only; full-map cleanup of
// expired entries is deferred to the periodic sweep (see sweep/reapLocked) so
// per-request cost stays O(1) and does not serialize on the mutex.
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
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <label for="password">Auth secret</label>
    <input type="password" id="password" name="password" required autofocus autocomplete="current-password">
    <button type="submit">Authorize</button>
  </form>
</body>
</html>`))
