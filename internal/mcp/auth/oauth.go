package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"go.lumeweb.com/oauth"
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
// All OAuth domain logic (PKCE, authorization codes, access tokens, RFC 9700
// refresh-token rotation + reuse detection, dynamic client registration) is
// delegated to *oauth.AuthorizationServer from go.lumeweb.com/oauth. This
// struct is a thin HTTP + auth layer: it renders the login form, resolves CIMD
// client metadata documents (which stay pinner-cli specific), and adapts the
// typed results into HTTP responses / MCP middleware.
type OAuthServer struct {
	mu      sync.Mutex
	secret  []byte
	Issuer  string
	BaseURL string

	// as is the shared authorization server that owns all domain logic.
	as *oauth.AuthorizationServer
	// store is the library's storage backend, kept here for CIMD client
	// registration/deactivation and for closing the underlying database.
	store oauth.Storage

	// clockSkew is the grace window applied to expired access tokens by the
	// bearer middleware.
	clockSkew time.Duration

	// cimdCache stores fetched CIMD client metadata documents keyed by URL.
	// TTL-bounded so a restarted client that rotates its metadata document
	// is picked up without a process restart.
	cimdCache   map[string]cimdEntry
	cimdCacheMu sync.Mutex

	// done stops the background reaper.
	done      chan struct{}
	closeOnce sync.Once

	// logger logs authorization events. It defaults to the shared package
	// logger and can be replaced via WithLogger.
	logger *zap.Logger
}

// oauthClient is the subset of a client's metadata the HTTP layer needs for
// CIMD resolution. Registered clients live in the shared store; only
// CIMD-resolved clients are tracked here (with a short TTL).
type oauthClient struct {
	redirectURIs      []string
	clientName        string
	tokenEndpointAuth string
}

// cimdEntry is a cached CIMD metadata document with an expiry.
type cimdEntry struct {
	client    oauthClient
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

// NewOAuthServer wraps a shared *oauth.AuthorizationServer with the HTTP/auth
// layer. cfg is taken from the authorization server itself; store is the same
// Storage instance the server was built with, reused here for CIMD client
// registration and shutdown.
func NewOAuthServer(secret, BaseURL string, as *oauth.AuthorizationServer, store oauth.Storage) *OAuthServer {
	cfg := oauth.DefaultConfig()
	if as != nil {
		cfg = as.Config()
	}
	o := &OAuthServer{
		secret:    []byte(secret),
		Issuer:    BaseURL,
		BaseURL:   BaseURL,
		as:        as,
		store:     store,
		clockSkew: cfg.ClockSkew,
		done:      make(chan struct{}),
		logger:    log,
		cimdCache: make(map[string]cimdEntry),
	}
	// Periodic reaper so long-running public tunnels do not grow the maps
	// without bound as codes go unredeemed and CIMD entries go stale.
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

// sweep periodically reaps expired durable rows and evicts stale CIMD entries.
func (o *OAuthServer) sweep() {
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

// reapLocked evicts expired durable rows via the authorization server and
// drops stale CIMD cache entries, deactivating their transient client
// registrations in the store so rotated metadata is re-fetched on next use.
func (o *OAuthServer) reapLocked() {
	now := time.Now()
	o.cimdCacheMu.Lock()
	for k, e := range o.cimdCache {
		if now.Sub(e.fetchedAt) >= cimdCacheTTL {
			delete(o.cimdCache, k)
			o.deactivateClient(k)
		}
	}
	o.cimdCacheMu.Unlock()
	if o.as != nil {
		_ = o.as.Reap()
	}
}

// deactivateClient flips a CIMD-derived client's IsActive to false in the
// store. The next authorize for that client re-resolves its metadata document
// and re-activates it, so a rotated document is picked up.
func (o *OAuthServer) deactivateClient(clientID string) {
	if o.store == nil {
		return
	}
	c, err := o.store.GetClient(clientID)
	if err != nil {
		return
	}
	c.IsActive = false
	_ = o.store.SaveClient(c)
}

// asMetadataHandler serves the OAuth 2.0 Authorization Server Metadata
// (RFC 8414) so clients can find the authorize and token endpoints.
func (o *OAuthServer) AsMetadataHandler(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"issuer":                                o.Issuer,
		"authorization_endpoint":                o.BaseURL + "/oauth/authorize",
		"token_endpoint":                        o.BaseURL + "/oauth/token",
		// DCR fallback: Claude Desktop/Web does not fall through to CIMD
		// (anthropics/claude-ai-mcp#433) and needs registration_endpoint or a
		// manual client ID. Claude Code / ChatGPT still prefer CIMD when both
		// client_id_metadata_document_supported and token_endpoint_auth_methods
		// ["none"] are advertised, so the two coexist.
		"registration_endpoint":                 o.BaseURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": true,
		"scopes_supported":                      []string{"offline_access"},
	}
	writeJSON(w, http.StatusOK, doc)
}

// RegisterMCPResource registers the /mcp endpoint as a protected resource
// (RFC 8707 / RFC 9728) with the underlying authorization server, so authorize
// requests resource-bound to it validate and ProtectedResourceHandler can serve
// its metadata from the registry. It must be (re)called whenever o.BaseURL
// changes (e.g. once the tunnel URL is known) so the registered resource
// tracks the advertised origin.
func (o *OAuthServer) RegisterMCPResource() {
	o.as.RegisterResource(oauth.Resource{
		ResourceURL: o.BaseURL + "/mcp",
		Scopes:      []string{"offline_access"},
		DisplayName: "Pinner MCP",
	})
}

// protectedResourceHandler serves the OAuth 2.0 Protected Resource Metadata
// (RFC 9728), pointing clients at this server's authorization server. The
// document is derived from the registered MCP resource (see RegisterMCPResource)
// so resource_name and scopes_supported flow from the registry rather than a
// hardcoded document.
func (o *OAuthServer) ProtectedResourceHandler(mcpPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resourceURL := o.BaseURL + mcpPath
		if reg, ok := o.as.GetResource(resourceURL); ok {
			writeJSON(w, http.StatusOK, oauth.BuildProtectedResourceMetadataFromResource(reg, o.Issuer))
			return
		}
		// Fallback for a resource that has not been registered yet (e.g. a
		// request arriving before tunnel startup): emit a minimal document.
		writeJSON(w, http.StatusOK, oauth.BuildProtectedResourceMetadata(resourceURL, o.Issuer))
	}
}

// authorizeGET renders the login page.
func (o *OAuthServer) AuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, err := o.validateAuthorizeQuery(q); err != nil {
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

// oauthReqFromValues builds an oauth.AuthorizeRequest from URL/form values.
func oauthReqFromValues(q url.Values) oauth.AuthorizeRequest {
	return oauth.AuthorizeRequest{
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		Resource:            q.Get("resource"),
		Scope:               q.Get("scope"),
	}
}

// validateAuthorizeQuery resolves any CIMD client_id, then delegates the
// authorization-request validation to the shared authorization server. It
// returns the built request so callers can re-use it for code issuance.
func (o *OAuthServer) validateAuthorizeQuery(q url.Values) (oauth.AuthorizeRequest, error) {
	req := oauthReqFromValues(q)
	if err := o.ensureClient(req.ClientID); err != nil {
		return req, err
	}
	if o.as == nil {
		return req, fmt.Errorf("oauth server unavailable")
	}
	return req, o.as.ValidateAuthorizeRequest(req)
}

// ensureClient makes a CIMD-derived client_id resolvable by the shared
// authorization server. CIMD clients are always run through the TTL-bounded
// metadata resolve (resolveCIMDClient) and re-registered as active, so a
// client deactivated by reap (see deactivateClient) is re-activated and any
// rotated metadata document is picked up on the next authorize. Non-CIMD
// client IDs are left untouched for the library to reject or accept on their
// persisted registration.
func (o *OAuthServer) ensureClient(clientID string) error {
	if o.store == nil || clientID == "" {
		return nil
	}
	if !isCIMDClientID(clientID) {
		return nil
	}
	cimdClient, err := o.resolveCIMDClient(clientID)
	if err != nil {
		return err
	}
	if err := o.store.SaveClient(oauth.Client{
		ClientID:          clientID,
		ClientName:        cimdClient.clientName,
		RedirectURIs:      cimdClient.redirectURIs,
		GrantTypes:        []string{"authorization_code", "refresh_token"},
		ResponseTypes:     []string{"code"},
		TokenEndpointAuth: cimdClient.tokenEndpointAuth,
		IsActive:          true,
	}); err != nil {
		return fmt.Errorf("register CIMD client: %w", err)
	}
	return nil
}

// RegisterHandler handles Dynamic Client Registration (RFC 7591 §3.1).
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
		if !oauth.AllowedClientRedirect(redirectURI) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
			return
		}
	}
	if request.TokenEndpointAuthMethod != "" && request.TokenEndpointAuthMethod != "none" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	if o.as == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	client, err := o.as.RegisterClient(oauth.ClientRegistration{
		ClientName:        request.ClientName,
		RedirectURIs:      request.RedirectURIs,
		TokenEndpointAuth: request.TokenEndpointAuthMethod,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuth,
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
	req, err := o.validateAuthorizeQuery(r.PostForm)
	if err != nil {
		o.logf().Warn("OAuth authorize rejected: invalid request", zap.String("client_id", r.PostFormValue("client_id")), zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// This is a single shared-secret resource owner; no per-user identity is
	// tracked, so the code is bound to user 0.
	code, err := o.as.IssueAuthorizationCode(req, 0)
	if err != nil {
		o.logf().Warn("OAuth authorize rejected: code issuance failed", zap.String("client_id", req.ClientID), zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	o.logf().Info("OAuth authorization code issued", zap.String("client_id", req.ClientID), zap.String("resource", req.Resource), zap.String("remote", r.RemoteAddr))
	redirectURI := req.RedirectURI
	params := url.Values{"code": {code}}
	if state := req.State; state != "" {
		params.Set("state", state)
	}
	loc := redirectURI + "?" + params.Encode()
	if strings.Contains(redirectURI, "?") {
		loc = redirectURI + "&" + params.Encode()
	}
	http.Redirect(w, r, loc, http.StatusFound)
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
// URL, validates it, and returns the parsed client. Results are cached for
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
		ClientName              string   `json:"client_name"`
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

	oc := oauthClient{
		redirectURIs:      doc.RedirectURIs,
		clientName:        doc.ClientName,
		tokenEndpointAuth: doc.TokenEndpointAuthMethod,
	}
	o.cimdCacheMu.Lock()
	o.cimdCache[clientID] = cimdEntry{client: oc, fetchedAt: time.Now()}
	o.cimdCacheMu.Unlock()
	return oc, nil
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
		o.exchangeCode(w, r)
	case "refresh_token":
		o.exchangeRefreshToken(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (o *OAuthServer) exchangeCode(w http.ResponseWriter, r *http.Request) {
	req := oauth.TokenRequest{
		GrantType:    r.PostFormValue("grant_type"),
		Code:         r.PostFormValue("code"),
		ClientID:     r.PostFormValue("client_id"),
		RedirectURI:  r.PostFormValue("redirect_uri"),
		CodeVerifier: r.PostFormValue("code_verifier"),
		Resource:     r.PostFormValue("resource"),
		RefreshToken: r.PostFormValue("refresh_token"),
	}
	resp, err := o.as.ExchangeCode(req)
	if err != nil {
		writeTokenError(w, err)
		return
	}
	o.logf().Info("OAuth token issued (authorization_code)", zap.String("client_id", req.ClientID), zap.String("resource", req.Resource), zap.String("remote", r.RemoteAddr))
	writeTokens(w, resp)
}

func (o *OAuthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	req := oauth.TokenRequest{
		GrantType:    r.PostFormValue("grant_type"),
		ClientID:     r.PostFormValue("client_id"),
		Resource:     r.PostFormValue("resource"),
		RefreshToken: r.PostFormValue("refresh_token"),
	}
	resp, err := o.as.RefreshToken(req)
	if err != nil {
		o.logf().Warn("OAuth refresh token rejected", zap.String("client_id", req.ClientID), zap.String("resource", req.Resource), zap.Error(err))
		writeTokenError(w, err)
		return
	}
	o.logf().Info("OAuth token issued (refresh_token)", zap.String("client_id", req.ClientID), zap.String("resource", req.Resource), zap.String("remote", r.RemoteAddr))
	writeTokens(w, resp)
}

// writeTokenError maps a library oauth error to an RFC 6749 §5.2 token
// endpoint error response. Non-oauth errors surface as server_error.
func writeTokenError(w http.ResponseWriter, err error) {
	var oauthErr *oauth.OAuthError
	if errors.As(err, &oauthErr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             oauthErr.Code,
			"error_description": oauthErr.Description,
		})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
}

// writeTokens writes the RFC 6749 §5.1 success response.
func writeTokens(w http.ResponseWriter, resp *oauth.TokenResponse) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  resp.AccessToken,
		"token_type":    resp.TokenType,
		"expires_in":    resp.ExpiresIn,
		"refresh_token": resp.RefreshToken,
	})
}

// validToken reports whether the given bearer token is one this AS issued
// and has not expired (within the clock-skew grace). Delegates to the shared
// authorization server, which reads persisted tokens.
func (o *OAuthServer) validToken(tok string) bool {
	_, ok := o.validTokenExpiry(tok)
	return ok
}

func (o *OAuthServer) validTokenExpiry(tok string) (time.Time, bool) {
	if o.as == nil {
		return time.Time{}, false
	}
	_, exp, ok := o.as.ValidateAccessToken(tok)
	return exp, ok
}

func (o *OAuthServer) OfficialMiddleware(next http.Handler) http.Handler {
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		exp, ok := o.validTokenExpiry(token)
		if !ok {
			return nil, fmt.Errorf("%w: the access token is unknown or has expired", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{Expiration: exp, UserID: tokenPrincipal(token)}, nil
	}
	protected := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: strings.TrimRight(o.BaseURL, "/") + "/.well-known/oauth-protected-resource",
		ClockSkew:           o.clockSkew,
	})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected.ServeHTTP(&oauthChallengeWriter{
			ResponseWriter:   w,
			status:           0,
			metadataURL:      strings.TrimRight(o.BaseURL, "/") + "/.well-known/oauth-protected-resource",
			errorDescription: "the access token is invalid, expired, or has been revoked",
		}, r)
	})
}

// oauthChallengeWriter upgrades the go-sdk's bearer auth 401 into a full
// RFC 6750 invalid_token challenge. The SDK's RequireBearerToken only emits a
// bare `resource_metadata` attribute on the WWW-Authenticate header and a
// plain-text body. Some MCP connectors (notably Grok's rmcp) key off the
// `error="invalid_token"` parameter to decide whether to refresh an access
// token instead of treating the 401 as fatal, so the challenge must carry it.
// Valid-token requests pass through untouched (headers committed at the real
// WriteHeader), so the SDK's TokenInfo context binding still works.
type oauthChallengeWriter struct {
	http.ResponseWriter
	status           int
	metadataURL      string
	errorDescription string
	body             []byte // pre-marshaled JSON error body for an upgraded 401
	upgraded         bool   // true once this 401 is a genuine bearer-auth failure
	wrote            bool   // true once the JSON body has been written
}

// isOAuthBearerFailure reports whether the response is a bearer-auth 401 rather
// than a downstream 401. The go-sdk's RequireBearerToken sets a
// `WWW-Authenticate: Bearer resource_metadata="..."` challenge only when it
// rejects the request (missing or invalid bearer token); downstream handlers'
// own 401s carry no such header. Only genuine auth failures should be upgraded
// to an invalid_token challenge.
func isOAuthBearerFailure(h http.Header) bool {
	for _, v := range h.Values("WWW-Authenticate") {
		lower := strings.ToLower(v)
		if strings.Contains(lower, "bearer") && strings.Contains(lower, "resource_metadata=") {
			return true
		}
	}
	return false
}

// Unwrap exposes the wrapped ResponseWriter so http.NewResponseController can
// reach the underlying http.Flusher. The standalone SSE stream (GET) path in
// sdk.StreamableHTTPHandler flushes via NewResponseController; without Unwrap
// a flush behind OAuth returns ErrNotSupported and the stream stalls.
func (w *oauthChallengeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *oauthChallengeWriter) WriteHeader(code int) {
	if w.status != 0 {
		return // headers already committed; ignore duplicates
	}
	w.status = code
	if code == http.StatusUnauthorized && isOAuthBearerFailure(w.Header()) {
		w.upgraded = true
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			`Bearer resource_metadata=%q, error="invalid_token", error_description=%q`,
			w.metadataURL, w.errorDescription))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The wrapped plain-text body it was sized for no longer matches the
		// JSON we emit; clear any stale Content-Length so the transport
		// recomputes it from our body instead of truncating the response.
		w.Header().Del("Content-Length")
		if j, err := json.Marshal(map[string]string{
			"error":             "invalid_token",
			"error_description": w.errorDescription,
		}); err == nil {
			w.body = j
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *oauthChallengeWriter) Write(b []byte) (int, error) {
	if w.upgraded {
		if !w.wrote && len(w.body) > 0 {
			w.wrote = true
			return w.ResponseWriter.Write(w.body)
		}
		// Stop any trailing writes from the original plain-text error from
		// leaking past the JSON body, which would yield a malformed body.
		return len(b), nil
	}
	// Non-auth 401s (0xx downstream) pass through with their original body.
	return w.ResponseWriter.Write(b)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
