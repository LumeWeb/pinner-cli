package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/oauthstore"
	"go.uber.org/zap"
)

// OAuthServer is a deliberately minimal, self-contained OAuth 2.1-shaped
// authorization server for the MCP HTTP transport. It exists so MCP clients
// that REQUIRE an OAuth handshake (ChatGPT, Claude.ai, Microsoft Copilot,
// Google Vertex) can discover and complete an authorization-code flow without
// pinner depending on an external identity provider.
//
// It is intentionally a "dummy" AS: the only credential is the shared --auth-token
// secret, which the resource owner enters on the login page as the password.
// Client registrations are durable in an embedded SQLite store, as are issued
// refresh tokens (which tolerate reuse rather than being invalidated on first
// use, avoiding invalid_grant against clients like Anthropic's that re-present
// them). Authorization codes remain in memory; access tokens are persisted too
// (like refresh tokens) so a connector holding a still-valid token can resume
// after a restart without re-authorizing. The authorization code flow enforces
// S256 PKCE and RFC 8707 resource binding; the shared secret remains the only
// user credential.
type OAuthServer struct {
	mu      sync.Mutex
	secret  []byte
	Issuer  string
	BaseURL string

	store *oauthstore.Store

	// clockSkew is the grace window applied to expired access tokens
	// by the bearer middleware. An access token that expired less than
	// this long ago is still accepted so an in-flight request that
	// started before expiry is not killed mid-pin.
	clockSkew time.Duration

	// registered clients keyed by client ID (backed by store)
	clients map[string]oauthClient
	// authorization codes, one-time use (in-memory)
	codes map[string]authorizationCode
	// access tokens issued to authenticated clients (short-lived, in-memory)
	tokens map[string]time.Time // access token -> expiry
	// access token TTL (refresh tokens live in the store)
	tokenTTL time.Duration
	codeTTL  time.Duration
	// done stops the background reaper.
	done      chan struct{}
	closeOnce sync.Once

	// logger logs authorization events. It defaults to the shared package
	// logger and can be replaced via WithLogger.
	logger *zap.Logger

	// cimdCache stores fetched CIMD client metadata documents keyed by URL.
	// TTL-bounded so a restarted client that rotates its metadata document
	// is picked up without a process restart.
	cimdCache   map[string]cimdEntry
	cimdCacheMu sync.Mutex
}

type oauthClient struct {
	redirectURIs []string
}

// cimdEntry is a cached CIMD metadata document with an expiry.
type cimdEntry struct {
	client   oauthClient
	fetchedAt time.Time
}

// cimdCacheTTL is how long a fetched CIMD document stays fresh before the
// server re-fetches on next use.
const cimdCacheTTL = 5 * time.Minute

// cimdFetchTimeout bounds the outbound HTTP GET so a slow or hostile host
// cannot stall the authorize flow.
const cimdFetchTimeout = 10 * time.Second

// cimdAllowedHosts is the allowlist of hosts whose CIMD documents the server
// will fetch. This prevents SSRF — an attacker cannot use a client_id URL
// pointing at internal/cloud-metadata endpoints. Loopback hosts are
// permitted for development and testing.
var cimdAllowedHosts = map[string]bool{
	"claude.ai":  true,
	"vscode.dev": true,
}

// allowedCIMDHost reports whether host is an allowlisted CIMD metadata host.
// Only known hosts in cimdAllowedHosts are fetched. This prevents SSRF — an
// attacker cannot use a client_id URL pointing at internal/cloud-metadata
// endpoints. Loopback hosts must be explicitly added to cimdAllowedHosts
// (e.g., by tests adding the test server's address).
func allowedCIMDHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if cimdAllowedHosts[host] {
		return true
	}
	// Reject any host that resolves to a private/link-local/multicast IP as
	// a defense-in-depth measure, even if it somehow passed the allowlist.
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return false
		}
	}
	return false
}

type authorizationCode struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	resource            string
	expiry              time.Time
}

func NewOAuthServer(secret, BaseURL string, store *oauthstore.Store) *OAuthServer {
	o := &OAuthServer{
		secret:   []byte(secret),
		Issuer:   BaseURL,
		BaseURL:  BaseURL,
		store:    store,
		clients:  make(map[string]oauthClient),
		codes:    make(map[string]authorizationCode),
		tokens:   make(map[string]time.Time),
		tokenTTL:  time.Hour,
		codeTTL:   10 * time.Minute,
		clockSkew: 2 * time.Minute,
		done:      make(chan struct{}),
		logger:    log,
		cimdCache: make(map[string]cimdEntry),
	}
	// Repopulate the in-memory client registry from the durable store so a
	// previously-registered client can complete a fresh authorization-code login
	// after a restart (its client_id and refresh token both outlive the process).
	if store != nil {
		if persisted, err := store.Clients(); err == nil {
			for id, uris := range persisted {
				o.clients[id] = oauthClient{redirectURIs: uris}
			}
		}
	}
	// Reload still-valid access tokens from the durable store. Most connectors
	// (Claude, ChatGPT) refresh on a 401, but Grok's rmcp/connectors-manager
	// does not — it treats "initialize" 401 + invalid_token as fatal and never
	// re-presents a refresh grant. Persisting access tokens is what lets a Grok
	// client holding an unexpired token resume after a restart instead of being
	// forced through a fresh authorize. Unexpired-within-skew tokens are loaded
	// so the middleware's clock-skew tolerance keeps working across the restart.
	if store != nil {
		if persisted, err := store.AccessTokens(); err == nil {
			now := time.Now()
			for tok, exp := range persisted {
				if now.Before(exp.Add(o.clockSkew)) {
					o.tokens[tok] = exp
				}
			}
		}
	}
	// Periodic reaper so long-running public tunnels do not grow the maps
	// without bound as clients rotate and codes go unredeemed.
	go o.sweep()
	return o
}

// WithLogger sets the zap logger the OAuth server uses for authorization
// events. It defaults to the shared package logger.
func (o *OAuthServer) WithLogger(l *zap.Logger) *OAuthServer {
	if l != nil {
		o.logger = l
	}
	return o
}

// logf returns the OAuth server's logger, falling back to the package logger.
func (o *OAuthServer) logf() *zap.Logger {
	if o.logger != nil {
		return o.logger
	}
	return log
}

// Stop terminates the background reaper and closes the durable store. It is
// safe to call multiple times.
func (o *OAuthServer) Stop() {
	o.closeOnce.Do(func() {
		close(o.done)
		if o.store != nil {
			_ = o.store.Close()
		}
	})
}

// sweep periodically drops expired tokens and codes.
func (o *OAuthServer) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			o.mu.Lock()
			o.reapLocked()
			o.mu.Unlock()
		case <-o.done:
			return
		}
	}
}

// reapLocked removes expired in-memory access tokens and codes, expired
// durable refresh tokens/clients via the store, and expired CIMD cache
// entries (including their transient registrations in o.clients). Caller
// must hold o.mu.
func (o *OAuthServer) reapLocked() {
	now := time.Now()
	for tok, exp := range o.tokens {
		o.evictBeyondSkew(tok, exp)
	}
	for code, e := range o.codes {
		if now.After(e.expiry) {
			delete(o.codes, code)
		}
	}
	// Evict expired CIMD cache entries (under cimdCacheMu) and their transient
	// client registrations. o.clients is guarded by o.mu, which the caller
	// already holds (both newCode and sweep acquire it before calling here).
	o.cimdCacheMu.Lock()
	for k, e := range o.cimdCache {
		if now.Sub(e.fetchedAt) >= cimdCacheTTL {
			delete(o.cimdCache, k)
			delete(o.clients, k)
		}
	}
	o.cimdCacheMu.Unlock()
	if o.store != nil {
		_ = o.store.Reap()
	}
}

// asMetadataHandler serves the OAuth 2.0 Authorization Server Metadata
// (RFC 8414) so clients can find the authorize and token endpoints.
func (o *OAuthServer) AsMetadataHandler(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"issuer":                                o.Issuer,
		"authorization_endpoint":                o.BaseURL + "/oauth/authorize",
		"token_endpoint":                        o.BaseURL + "/oauth/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": true,
		"scopes_supported":                      []string{"offline_access"},
	}
	writeJSON(w, http.StatusOK, doc)
}

// protectedResourceHandler serves the OAuth 2.0 Protected Resource Metadata
// (RFC 9728), pointing clients at this server's authorization server.
func (o *OAuthServer) ProtectedResourceHandler(mcpPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]any{
			"resource":                 o.BaseURL + mcpPath,
			"authorization_servers":    []string{o.Issuer},
			"bearer_methods_supported": []string{"header"},
			"scopes_supported":         []string{"offline_access"},
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

// authorizeGET renders the login page.
func (o *OAuthServer) AuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := o.validateAuthorizeRequest(q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := oauthLoginPage(oauthAuthorizeData{
		Action:              o.BaseURL + "/oauth/authorize",
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		Resource:            q.Get("resource"),
	}).Render(r.Context(), w)
	if err != nil {
		// Render already partially wrote the response; nothing safe to do but
		// to stop. A template render failure here is essentially unreachable.
		return
	}
}

func (o *OAuthServer) validateAuthorizeRequest(q url.Values) error {
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
	if !ok && o.store != nil {
		// Client not in the in-memory registry — load it from the durable store
		// (it may have been registered by a previous process sharing the DB).
		uris, err := o.store.ClientRedirectURIs(clientID)
		if err == nil && len(uris) > 0 {
			client = oauthClient{redirectURIs: uris}
			ok = true
			o.mu.Lock()
			o.clients[clientID] = client
			o.mu.Unlock()
		}
	}
	if !ok && isCIMDClientID(clientID) {
		cimdClient, err := o.resolveCIMDClient(clientID)
		if err != nil {
			return fmt.Errorf("could not resolve client metadata: %w", err)
		}
		o.mu.Lock()
		o.clients[clientID] = cimdClient
		o.mu.Unlock()
		client = cimdClient
		ok = true
	}
	if !ok || !matchRedirectURI(client.redirectURIs, redirectURI) {
		return fmt.Errorf("unregistered client or redirect_uri")
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return fmt.Errorf("S256 PKCE is required")
	}
	if !base64URLChars(q.Get("code_challenge")) {
		return fmt.Errorf("code_challenge must be base64url (RFC 7636)")
	}
	if q.Get("resource") != o.BaseURL+"/mcp" {
		return fmt.Errorf("invalid resource")
	}
	return nil
}

// base64URLChars reports whether s contains only the RFC 4648 base64url
// alphabet plus the RFC 7636 additional allowed chars (-._~), which is the
// only legitimately valid shape for an S256 PKCE code_challenge. Rejecting
// anything else keeps attacker-controlled raw markup out of the authorize page
// even if a render path were to regress.
func base64URLChars(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '.', r == '_', r == '~':
		default:
			return false
		}
	}
	return true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (o *OAuthServer) RegisterHandler(w http.ResponseWriter, r *http.Request) {
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
	if o.store != nil {
		if err := o.store.SaveClient(clientID, request.ClientName, request.RedirectURIs); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
	}
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
func (o *OAuthServer) AuthorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	password := r.PostFormValue("password")
	if subtle.ConstantTimeCompare([]byte(password), o.secret) != 1 {
		o.logf().Warn("OAuth authorize rejected: bad resource-owner secret", zap.String("client_id", r.PostFormValue("client_id")), zap.String("remote", r.RemoteAddr))
		// Re-render the login page so a human who mistyped the shared secret
		// sees a branded retry form rather than a bare text/JSON error. Keep
		// 401 so programmatic clients still observe the failed authorization.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = oauthLoginPage(oauthAuthorizeData{
			Action:              o.BaseURL + "/oauth/authorize",
			ResponseType:        r.PostFormValue("response_type"),
			ClientID:            r.PostFormValue("client_id"),
			RedirectURI:         r.PostFormValue("redirect_uri"),
			State:               r.PostFormValue("state"),
			CodeChallenge:       r.PostFormValue("code_challenge"),
			CodeChallengeMethod: r.PostFormValue("code_challenge_method"),
			Resource:            r.PostFormValue("resource"),
			Error:               "Invalid auth secret. Please try again.",
		}).Render(r.Context(), w)
		return
	}
	if err := o.validateAuthorizeRequest(r.PostForm); err != nil {
		o.logf().Warn("OAuth authorize rejected: invalid request", zap.String("client_id", r.PostFormValue("client_id")), zap.Error(err))
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
	o.logf().Info("OAuth authorization code issued", zap.String("client_id", r.PostFormValue("client_id")), zap.String("resource", r.PostFormValue("resource")), zap.String("remote", r.RemoteAddr))
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

// isCIMDClientID reports whether clientID is a URL-form client identifier
// that the server should resolve as a CIMD document. Per the spec the URL
// must use https and contain a path component. http is accepted for loopback
// hosts so development and test environments can exercise the CIMD path
// without provisioning TLS certificates.
func isCIMDClientID(clientID string) bool {
	u, err := url.Parse(clientID)
	if err != nil {
		return false
	}
	if u.Host == "" || u.Path == "" || u.Path == "/" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return true
		}
	}
	return false
}

// resolveCIMDClient fetches the client metadata document at the given HTTPS
// URL, validates it, and returns the redirect URIs. Results are cached for
// cimdCacheTTL so repeated authorize requests from the same client within
// the window do not re-fetch.
func (o *OAuthServer) resolveCIMDClient(clientID string) (oauthClient, error) {
	o.cimdCacheMu.Lock()
	if entry, ok := o.cimdCache[clientID]; ok {
		if time.Since(entry.fetchedAt) < cimdCacheTTL {
			o.cimdCacheMu.Unlock()
			return entry.client, nil
		}
	}
	o.cimdCacheMu.Unlock()

	if !allowedCIMDHost(clientID) {
		o.logf().Warn("CIMD fetch rejected: host not allowlisted", zap.String("client_id", clientID))
		return oauthClient{}, fmt.Errorf("client metadata host is not allowlisted")
	}

	client := &http.Client{
		Timeout: cimdFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(clientID)
	if err != nil {
		o.logf().Warn("CIMD fetch failed", zap.String("client_id", clientID), zap.Error(err))
		return oauthClient{}, fmt.Errorf("could not fetch client metadata document")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		o.logf().Warn("CIMD fetch returned non-200", zap.String("client_id", clientID), zap.Int("status", resp.StatusCode))
		return oauthClient{}, fmt.Errorf("client metadata document returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return oauthClient{}, fmt.Errorf("could not read client metadata document")
	}

	var doc struct {
		ClientID                string   `json:"client_id"`
		ClientName             string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return oauthClient{}, fmt.Errorf("invalid client metadata document JSON")
	}
	if doc.ClientID != clientID {
		o.logf().Warn("CIMD client_id mismatch", zap.String("expected", clientID), zap.String("got", doc.ClientID))
		return oauthClient{}, fmt.Errorf("client_id in metadata document does not match the request URL")
	}
	if len(doc.RedirectURIs) == 0 {
		return oauthClient{}, fmt.Errorf("client metadata document has no redirect_uris")
	}

	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
		return oauthClient{}, fmt.Errorf("client metadata requires unsupported token_endpoint_auth_method: %s", doc.TokenEndpointAuthMethod)
	}

	oc := oauthClient{redirectURIs: doc.RedirectURIs}
	o.cimdCacheMu.Lock()
	o.cimdCache[clientID] = cimdEntry{client: oc, fetchedAt: time.Now()}
	o.cimdCacheMu.Unlock()
	return oc, nil
}

// matchRedirectURI reports whether the requested redirect_uri is allowed for
// the given registered URIs. For loopback redirect URIs (localhost, 127.0.0.1,
// ::1), any port is accepted per RFC 8252 §7.3, because native clients like
// Claude Code use an OS-assigned port at runtime. For non-loopback URIs, an
// exact match is required.
func matchRedirectURI(registered []string, requested string) bool {
	parsedReq, err := url.Parse(requested)
	if err != nil {
		return false
	}

	for _, reg := range registered {
		if reg == requested {
			return true
		}
		parsedReg, err := url.Parse(reg)
		if err != nil {
			continue
		}

		if isLoopbackRedirectURI(parsedReg) && isLoopbackRedirectURI(parsedReq) {
			parsedRegCopy := *parsedReg
			parsedReqCopy := *parsedReq
			parsedRegCopy.Host = parsedReg.Hostname()
			parsedReqCopy.Host = parsedReq.Hostname()
			if parsedRegCopy.String() == parsedReqCopy.String() {
				return true
			}
		}
	}
	return false
}

// isLoopbackRedirectURI reports whether the parsed URL uses a loopback host.
func isLoopbackRedirectURI(u *url.URL) bool {
	if u == nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// tokenHandler exchanges an authorization code or refresh token for tokens.
func (o *OAuthServer) TokenHandler(w http.ResponseWriter, r *http.Request) {
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

func (o *OAuthServer) exchangeCode(w http.ResponseWriter, r *http.Request) error {
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
	o.storeTokens(pair, entry.clientID, entry.resource)
	o.logf().Info("OAuth token issued (authorization_code)", zap.String("client_id", entry.clientID), zap.String("resource", entry.resource), zap.String("remote", r.RemoteAddr))
	issueTokens(w, pair, o.tokenTTL)
	return nil
}

func (o *OAuthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	refresh := r.PostFormValue("refresh_token")
	clientID := r.PostFormValue("client_id")
	resource := r.PostFormValue("resource")
	// Without a durable store there is no way to rotate or validate a refresh
	// token, so fall back to a controlled error rather than nil-deref panicking
	// (NewOAuthServer permits a nil store).
	if o.store == nil {
		writeInvalidGrant(w)
		return
	}
	_, successor, status, err := o.store.RotateRefreshToken(refresh, clientID, resource)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	pair := tokenPair{access: newToken(32), refresh: successor}
	switch status {
	case oauthstore.RotateOK, oauthstore.RotateOKReused:
		// Accepted (fresh rotation or benign reuse within the window). The
		// successor refresh token was already issued by RotateRefreshToken, so
		// only the fresh access token needs in-memory + durable registration.
		o.storeAccessToken(pair.access, clientID, resource, o.tokenTTL)
		o.logf().Info("OAuth token issued (refresh_token)", zap.String("client_id", clientID), zap.String("resource", resource), zap.String("remote", r.RemoteAddr))
		issueTokens(w, pair, o.tokenTTL)
	default:
		// RotateReplay (rotated beyond reuse window, revoked, expired, or
		// unknown) → invalid_grant per RFC 6749 §5.2.
		desc := "the refresh token is invalid, expired, or unknown"
		if status == oauthstore.RotateReplay {
			desc = "the refresh token has been replayed and the grant has been revoked"
		}
		o.logf().Warn("OAuth refresh token rejected", zap.String("client_id", clientID), zap.String("resource", resource))
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_grant",
			"error_description": desc,
		})
	}
}

func writeInvalidGrant(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error":             "invalid_grant",
		"error_description": "the refresh token is invalid, expired, or unknown",
	})
}

func (o *OAuthServer) newTokens() tokenPair {
	return tokenPair{access: newToken(32), refresh: newToken(32)}
}

type tokenPair struct {
	access  string
	refresh string
}

func (o *OAuthServer) storeTokens(pair tokenPair, clientID, resource string) {
	o.storeAccessToken(pair.access, clientID, resource, o.tokenTTL)
	if o.store != nil {
		_ = o.store.IssueRefreshToken(pair.refresh, clientID, resource)
	}
}

// storeAccessToken registers an access token in the in-memory map AND persists
// it so it survives a process restart (see NewOAuthServer's reload). It takes
// o.mu itself, so callers must not already hold it.
func (o *OAuthServer) storeAccessToken(token, clientID, resource string, ttl time.Duration) {
	expiry := time.Now().Add(ttl)
	o.mu.Lock()
	o.tokens[token] = expiry
	o.mu.Unlock()
	if o.store != nil {
		_ = o.store.SaveAccessToken(token, clientID, resource, expiry)
	}
}

// evictBeyondSkew removes an access token that is past the clock-skew grace
// from both the in-memory map and the durable store, and reports whether it
// was evicted. It is the single place access-token eviction happens, keeping
// reap/sweep/middleware consistent so a restart never resurrects a token the
// running server already invalidated. The caller must hold o.mu.
func (o *OAuthServer) evictBeyondSkew(tok string, exp time.Time) bool {
	if !time.Now().After(exp.Add(o.clockSkew)) {
		return false
	}
	delete(o.tokens, tok)
	if o.store != nil {
		_ = o.store.DeleteAccessToken(tok)
	}
	return true
}

func issueTokens(w http.ResponseWriter, pair tokenPair, ttl time.Duration) {
	// Storage is performed by the caller's OAuthServer before this response.
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.access,
		"token_type":    "Bearer",
		"expires_in":    int(ttl.Seconds()),
		"refresh_token": pair.refresh,
	})
}

func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (o *OAuthServer) newCode(entry authorizationCode) string {
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
func (o *OAuthServer) validToken(tok string) bool {
	_, ok := o.tokenExpiry(tok)
	return ok
}

func (o *OAuthServer) tokenExpiry(tok string) (time.Time, bool) {
	o.mu.Lock()
	exp, ok := o.tokens[tok]
	if !ok {
		o.mu.Unlock()
		return exp, false
	}
	if o.evictBeyondSkew(tok, exp) {
		// Past the skew boundary: evicted from memory AND the durable store (by
		// evictBeyondSkew) so a restart never resurrects an invalidated token.
		o.mu.Unlock()
		return exp, false
	}
	// Within the clock-skew grace (or still valid): mirror OfficialMiddleware
	// exactly, keeping this granted until exp+clockSkew, so protectMCP/validToken
	// admit precisely what the resource middleware admits.
	o.mu.Unlock()
	return exp, true
}

func (o *OAuthServer) OfficialMiddleware(next http.Handler) http.Handler {
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		o.mu.Lock()
		exp, ok := o.tokens[token]
		if ok {
			// Keep the token in the map while it is still within the
			// ClockSkew grace window so concurrent in-flight requests
			// that present the same expired token also receive the
			// TokenInfo and let the SDK apply the same tolerance.
			// Tokens expired beyond the skew are evicted here (in-memory and
			// durable) to bound map growth without breaking sibling requests.
			if o.evictBeyondSkew(token, exp) {
				o.mu.Unlock()
				return nil, fmt.Errorf("%w: the access token has expired", auth.ErrInvalidToken)
			}
			o.mu.Unlock()
			return &auth.TokenInfo{Expiration: exp, UserID: tokenPrincipal(token)}, nil
		}
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: the access token is unknown or has been revoked", auth.ErrInvalidToken)
	}
	return auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: strings.TrimRight(o.BaseURL, "/") + "/.well-known/oauth-protected-resource",
		ClockSkew:           o.clockSkew,
	})(next)
}

func StaticBearerMiddleware(secret string, next http.Handler) http.Handler {
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			Expiration: time.Now().Add(time.Hour),
			UserID:     tokenPrincipal(token),
		}, nil
	}
	return auth.RequireBearerToken(verifier, nil)(next)
}

func tokenPrincipal(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// protectMCP wraps the MCP resource-server handler with OAuth bearer-token
// validation. Unauthenticated and invalid requests get a 401 with an RFC 9728
// WWW-Authenticate challenge pointing at the protected-resource metadata, so
// OAuth-capable MCP clients can discover and complete the flow.
func (o *OAuthServer) protectMCP(mcpPath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token != "" && o.validToken(token) {
			next.ServeHTTP(w, r)
			return
		}
		var desc string
		if token == "" {
			desc = "a valid bearer token is required"
		} else {
			desc = "the access token has expired or been revoked"
		}
		o.logf().Warn("MCP endpoint denied: missing or invalid bearer token",
			zap.String("path", r.URL.Path), zap.String("remote", r.RemoteAddr), zap.Bool("presented_token", token != ""))
		deny(w, fmt.Sprintf(
			`Bearer resource_metadata="%s/.well-known/oauth-protected-resource", error="invalid_token", error_description="%s"`,
			o.BaseURL, desc))
	})
}

// deny writes an HTTP 401 with a WWW-Authenticate challenge for the OAuth
// resource-server path.
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
