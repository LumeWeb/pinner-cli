package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// pendingLoginTTL bounds how long an out-of-band login request stays valid
// before the human must complete it.
const pendingLoginTTL = 10 * time.Minute

// maxLoginAttempts and loginLockout throttle credential attempts against the
// loopback login endpoint: after maxLoginAttempts consecutive failures an email
// is locked out for a flat loginLockout cooldown. A single, short, flat lockout
// is sufficient to blunt password brute-force — a sustained attacker is held to
// one guess-burst per cooldown, and a legitimate owner is only ever briefly
// delayed rather than locked out for hours.
const maxLoginAttempts = 10
const loginLockout = 30 * time.Second

// loginStatus is the lifecycle state of an out-of-band login request.
type loginStatus string

const (
	loginPending   loginStatus = "pending"   // awaiting the human in the browser
	loginCompleted loginStatus = "completed" // verified in the browser (success or failure)
	loginExpired   loginStatus = "expired"   // elapsed without completion
)

// loginRequest is the state of a single out-of-band login initiated by the
// MCP setup wizard. The credentials are collected in a browser, never through
// the MCP/LLM channel.
type loginRequest struct {
	id           string
	sessionID    string
	email        string
	created      time.Time
	completedAt  time.Time
	mu           sync.Mutex
	status       loginStatus
	loginError   error
	intermediate string
	otpRequired  bool
	// csrfToken is an ephemeral, per-request secret embedded in the rendered
	// login form and required on POST. It is generated at runtime from
	// crypto/rand (randomID) when the request is created in Begin, lives only
	// in memory for the request's lifetime, and is never a hard-coded literal.
	csrfToken string
}

func (r *loginRequest) complete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = loginCompleted
	r.completedAt = time.Now()
	// A successful completion must not report a stale prior failure (e.g. an
	// earlier failed OTP attempt that called fail()).
	r.loginError = nil
}

func (r *loginRequest) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = loginCompleted
	r.completedAt = time.Now()
	r.loginError = err
}

// completeReq marks a request terminal-success under o.mu, serializing the
// status write with Begin's completed-detection (which holds o.mu while reading
// status and deciding whether to delete). This ensures a browser POST that
// completes a login is never dropped by a concurrent Begin: the status is set
// and observed under the same lock before Begin can evict or replace it.
func (o *OutOfBandLogin) completeReq(r *loginRequest) {
	o.mu.Lock()
	defer o.mu.Unlock()
	r.complete()
}

// failReq marks a request terminal-failure under o.mu (see completeReq).
func (o *OutOfBandLogin) failReq(r *loginRequest, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	r.fail(err)
}

// OutOfBandLogin serves branded login pages on a loopback listener (stdio) or
// on the shared HTTP/tunnel mux (remote). It collects credentials in the
// browser and completes the MCP setup login without secrets crossing the MCP
// channel.
type OutOfBandLogin struct {
	auth    AuthService
	keyName string
	// loopback owns the base-URL/loopback-listener mechanics shared with the
	// other hand-offs (seed drop, restore). The login keeps its own request map
	// and throttle because it is a resumable/polling flow with server-side
	// state and brute-force protection, not a simple single-use exchange.
	loopback loopbackServer

	mu       sync.Mutex
	requests map[string]*loginRequest

	// throttle tracks brute-force lockout independently of the per-request
	// lifecycle, keyed by email, so evicting or restarting a login request
	// (Begin) cannot reset the failure counter. A single flat cooldown (no
	// session-scoping, no exponential escalation) is sufficient for a password
	// form whose primary exposure is the owner's own cloud host.
	throttle map[string]*loginThrottle

	// spent records recently-consumed or expired login tokens once they leave
	// the pending set, so a re-opened /login/<id> URL whose request has been
	// evicted by the reaper still renders the branded spent-link page instead
	// of a bare 404. It is pruned once older than spentHoldTTL.
	spent map[string]spentLogin

	reaperCtx    context.Context
	reaperCancel context.CancelFunc
}

// spentLogin is a tombstone for an evicted login token: when it became spent
// and the handoffNotActiveReason to show on a re-open.
type spentLogin struct {
	at     time.Time
	reason handoffNotActiveReason
}

// loginThrottle is the credential attempt counter for an email. lastUsed is
// stamped whenever the counter is touched so the reaper can evict throttles
// that have been idle for throttleIdleTTL, bounding the map over a long-running
// MCP process.
type loginThrottle struct {
	mu          sync.Mutex
	failed      int
	lockedUntil time.Time
	lastUsed    time.Time
}

// throttleIdleTTL is how long an email's throttle entry is kept after its last
// credential attempt before the reaper evicts it. Lockouts last loginLockout,
// so this is comfortably larger, ensuring an active cooldown is never pruned.
const throttleIdleTTL = 24 * time.Hour

// DefaultMCPKeyName is the human-readable name profile under which the
// server-side key-wrap stores the credential it generates during out-of-band
// login. It is a label for server-side key management, not a secret — the
// actual credential is produced and stored by the auth backend, never here.
const DefaultMCPKeyName = "mcp-generated"

// NewOutOfBandLogin creates an out-of-band login coordinator backed by the
// MCP auth service.
func NewOutOfBandLogin(auth AuthService, baseURL, keyName string) *OutOfBandLogin {
	o := &OutOfBandLogin{
		auth:     auth,
		keyName:  keyName,
		requests: make(map[string]*loginRequest),
		throttle: make(map[string]*loginThrottle),
		spent:    make(map[string]spentLogin),
	}
	// Loopback-associated base URL (empty keeps the loopback-derived URL).
	o.loopback.baseURL = strings.TrimRight(baseURL, "/")
	return o
}

// start spins up the loopback HTTP server used when the MCP client runs over
// stdio. It is idempotent: subsequent calls reuse the existing listener. The
// loopback listener also serves the static /assets/ (branded login page).
func (o *OutOfBandLogin) start() error {
	return o.loopback.ensureLoopback(func(mux *http.ServeMux) {
		mux.Handle("/assets/", staticAssetHandler())
		o.registerHandlers(mux)
	})
}

// registerHandlers mounts the out-of-band login routes (login page, login
// redirect) onto the given mux. The /assets/ static handler is deliberately NOT
// registered here: the loopback listener registers it in start() and the shared
// HTTP/tunnel mux in serveHTTP, so mounting it here too would trigger a duplicate
// http.ServeMux.Handle panic ("multiple registrations for /assets/") and crash
// the transport at startup.
func (o *OutOfBandLogin) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/login/", o.loginPage)
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	})
}

// SetBaseURL sets the external URL the human opens (the loopback address or the
// public/tunnel URL). It is safe to call after construction once the remote
// transport has resolved its public URL; empty keeps the derived loopback URL.
func (o *OutOfBandLogin) SetBaseURL(url string) {
	o.loopback.SetBaseURL(url)
}

// loginURLLocked returns the localhost URL the human opens for a request id, or
// the external base URL when a tunnel/public base is configured. Callers must
// hold o.mu (pendingOutcome calls it inside its critical section).
func (o *OutOfBandLogin) loginURLLocked(id string) string {
	return o.loopback.urlLocked("login", id)
}

// loginURL returns the localhost URL the human opens for the given request
// id, or the external base URL when a tunnel/public base is configured.
func (o *OutOfBandLogin) loginURL(id string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.loginURLLocked(id)
}

// origin returns the origin (scheme://host) the login page is served from.
// Cross-origin requests to the login endpoints must have a matching Origin or
// Referer, otherwise they are treated as CSRF (see loginPage).
func (o *OutOfBandLogin) origin() string {
	orig := o.loopback.acceptedOrigins()
	if len(orig) == 0 {
		return ""
	}
	return orig[0]
}

// acceptedOrigins returns the origins a credential POST is allowed to carry.
// It is derived only from the served listener/base origin — never from the
// client-controllable Host header — and includes the localhost spelling of the
// loopback listener so a human who opens the login page via localhost is not
// wrongly rejected. The set is closed and cannot be widened by Host spoofing.
func (o *OutOfBandLogin) acceptedOrigins() []string {
	orig := o.origin()
	accepted := []string{orig}
	// If the loopback listener was addressed by IP, also accept the standard
	// "localhost" name for the same port (both resolve to the loopback). The
	// client's Host header is never consulted.
	if h, _, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(orig, "http://"), "https://")); err == nil && net.ParseIP(h) != nil {
		accepted = append(accepted, strings.Replace(orig, h, "localhost", 1))
	}
	return accepted
}

// Begin starts an out-of-band login for a wizard session and returns the
// request id and the URL the human must open. The stdio path starts the
// loopback server on demand; a non-empty baseURL (tunnel/public) is used when
// the server is remote.
func (o *OutOfBandLogin) Begin(sessionID, email string) (id, url string, err error) {
	if err := o.start(); err != nil {
		return "", "", err
	}
	o.startReaper()
	req := &loginRequest{
		id:        randomID(),
		csrfToken: randomID(),
		sessionID: sessionID,
		email:     email,
		created:   time.Now(),
		status:    loginPending,
	}
	o.mu.Lock()
	// A session may only have one active out-of-band login per email. Evict any
	// prior request for the same session+email (e.g. an earlier failed attempt)
	// so pendingOutcome can never resolve to a stale request. Requests from
	// other sessions are left untouched. Exception: if a prior request has
	// already completed successfully in the browser (an in-flight POST may have
	// just accepted the credentials), reuse it so the accepted login is not
	// discarded by a redundant Begin, which would force the user to sign in
	// again.
	var completed *loginRequest
	for id, r := range o.requests {
		if r.email == email && r.sessionID == sessionID {
			r.mu.Lock()
			done := r.status == loginCompleted && r.loginError == nil
			r.mu.Unlock()
			if done {
				completed = r
				break
			}
			delete(o.requests, id)
		}
	}
	if completed != nil {
		o.mu.Unlock()
		return completed.id, o.loginURL(completed.id), nil
	}
	o.requests[req.id] = req
	o.mu.Unlock()
	return req.id, o.loginURL(req.id), nil
}

// startReaper launches the expiry reaper exactly once.
func (o *OutOfBandLogin) startReaper() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.reaperCtx != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.reaperCtx = ctx
	o.reaperCancel = cancel
	go o.reaper(ctx)
}

// pendingOutcome reports the current state of the out-of-band login for a
// wizard session: the URL the human must open while it is still pending, or a
// done flag once it has completed. A non-nil err is returned when the login
// attempt failed or expired, so the caller keeps the session on the auth step
// and lets the user retry. It is bound to the originating session id so a
// completed login in one session never satisfies authentication in another.
func (o *OutOfBandLogin) pendingOutcome(sessionID, email string) (url string, done bool, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var req *loginRequest
	for _, r := range o.requests {
		if r.email == email && r.sessionID == sessionID {
			req = r
		}
	}
	if req == nil {
		return "", false, nil
	}
	req.mu.Lock()
	defer req.mu.Unlock()
	switch req.status {
	case loginPending:
		// We hold o.mu here, so use the locked URL helper (loginURL would
		// re-acquire the lock and deadlock).
		return o.loginURLLocked(req.id), false, nil
	case loginCompleted:
		// Terminal outcome: the polling session consumes it. Remove the
		// request so a later session signing in with the same email cannot see
		// a stale "done" for credentials it never entered, and so completed
		// requests do not accumulate for the process lifetime (see reaper).
		delete(o.requests, req.id)
		if req.loginError != nil {
			return "", true, req.loginError
		}
		return "", true, nil
	default: // expired
		delete(o.requests, req.id)
		return "", true, fmt.Errorf("out-of-band login expired, please retry")
	}
}

// Stop shuts down the loopback login server and reaper (used on program
// exit). Shutdown is called WITHOUT holding o.mu so a concurrent in-flight
// /login POST handler (which needs o.mu via throttleLocked/failReq/noteFailure)
// can finish instead of stalling teardown for up to the shutdown context
// deadline.
func (o *OutOfBandLogin) Stop(ctx context.Context) {
	o.mu.Lock()
	if o.reaperCancel != nil {
		o.reaperCancel()
		o.reaperCancel = nil
		// Clear reaperCtx too so a later Begin()/startReaper() (after a
		// stop/restart cycle) starts a fresh reaper instead of returning on the
		// stale non-nil guard and letting pending requests accumulate unpruned.
		o.reaperCtx = nil
	}
	o.mu.Unlock()
	// Shutdown runs without holding o.mu (see method doc) so in-flight /login
	// POST handlers needing o.mu can finish instead of stalling teardown.
	o.loopback.Stop(ctx)
}

// loginPage renders the branded login form for a pending request. The browser
// POSTs credentials to the same route; they never appear on the MCP channel.
func (o *OutOfBandLogin) loginPage(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/login/")
	o.mu.Lock()
	req, ok := o.requests[path]
	sl, spent := o.spent[path]
	o.mu.Unlock()
	if !ok {
		// The request is gone from the pending set. If it was evicted as spent
		// (consumed or expired, e.g. after the reaper's pendingLoginTTL grace
		// window), render the branded spent-link page; only a token that never
		// existed gets a bare 404.
		if spent {
			o.loginNotActive(w, r, sl.reason)
			return
		}
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// A sign-in link is single-use: opening (or reloading) a URL whose
		// request already completed successfully or expired must show the
		// spent-link page immediately, without requiring a form submit. Only
		// a FAILED attempt (completed with an error) keeps showing the login
		// form so the human can retry in place.
		if reason := o.loginSpentReason(req); reason != "" {
			o.loginNotActive(w, r, reason)
			return
		}
		o.authLoginPage(w, r, req)
	case http.MethodPost:
		// CSRF guard: a browser form-POST carries an Origin (and usually a
		// Referer) matching the origin the page was served from; a cross-origin
		// web page would carry a foreign origin. Only the loopback origin
		// (127.0.0.1 or localhost for the same listener port) is accepted, and
		// it is derived from the served listener/base, never from the
		// client-controllable Host header. Requests carrying neither header are
		// rejected (the endpoint is browser-only).
		if ok := sameOrigin(r, o.acceptedOrigins()...); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Only a login that is still actionable may accept a credential POST.
		// A terminal SUCCESS (completed, no error) must not be flipped into a
		// reported failure by a stale re-POST (e.g. via the Back button), and an
		// expired request must not run the auth backend via a URL relayed before
		// expiry — both would leave a backend session no wizard consumes.
		// Render the spent-link page and let the wizard restart sign-in. A
		// FAILED attempt (completed with an error) stays POST-able so the human
		// can retry the form in place; only the throttle blunts repeated guesses.
		if reason := o.loginSpentReason(req); reason != "" {
			o.loginNotActive(w, r, reason)
			return
		}
		o.authLoginSubmit(w, r, req)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// authLoginSubmit validates credentials the human typed and completes the
// request, so an awaiting setup step unblocks. Attempts are throttled per
// request to blunt brute-force guessing against the loopback endpoint: after a
// burst of failures the request is locked out for a short cooldown.
func (o *OutOfBandLogin) authLoginSubmit(w http.ResponseWriter, r *http.Request, req *loginRequest) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// The human may edit the account identifier on the page (the agent-supplied
	// email for pinner_auth_sso is only a prefill). Bind the posted value into
	// the request so LoginCheck, the brute-force throttle, and the OTP stage all
	// key on the account the human actually entered.
	if email := strings.TrimSpace(r.FormValue("email")); email != "" {
		req.mu.Lock()
		req.email = email
		req.mu.Unlock()
	}
	password := r.FormValue("password")
	otp := r.FormValue("otp")
	if password == "" {
		http.Error(w, "password is required", http.StatusBadRequest)
		return
	}

	// CSRF token check: the credential POST must carry the per-request token
	// that the rendered form embedded for this login (a double-submit token,
	// distinct from the unguessable request id in the URL path). A local process
	// that somehow learns the request id but not this token cannot forge a
	// credential POST. This is the token-based CSRF bound; the Origin check is
	// a secondary defense-in-depth layer.
	req.mu.Lock()
	expectedCSRF := req.csrfToken
	req.mu.Unlock()
	if expectedCSRF == "" || subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(expectedCSRF)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Throttle: reject attempts while this email is in a flat lockout cooldown.
	// The counter is keyed on email and lives on the coordinator, so restarting a
	// login cannot reset it. A short flat cooldown blunts brute-force without the
	// complexity of session-scoped or escalating counters.
	if o.throttleLocked(req.email) {
		o.authLoginPage(w, r, req)
		return
	}

	res, err := o.auth.LoginCheck(r.Context(), req.email, password)
	if err != nil {
		// Record the failure so the rendered page shows the message, and mark
		// the request terminal so the wizard restarts sign-in for a fresh
		// attempt. (Throttling is tracked on the coordinator, see noteFailure.)
		o.noteFailure(req.email)
		o.failReq(req, fmt.Errorf("authentication failed: %w", err))
		o.authLoginPage(w, r, req)
		return
	}
	if res.OTPRequired {
		if otp == "" {
			// Show the OTP stage of the page; keep the request pending.
			req.mu.Lock()
			req.intermediate = res.IntermediateJWT
			req.otpRequired = true
			req.mu.Unlock()
			o.authLoginPage(w, r, req)
			return
		}
		if err := o.auth.LoginWithOTP(r.Context(), res.IntermediateJWT, otp, o.keyName, false); err != nil {
			// Keep the request PENDING and record the OTP error so the page
			// re-renders with the message and the human can retry the code on
			// the same page. A terminal fail() here would make pendingOutcome
			// report done+err and the wizard's Begin would evict this page,
			// forcing a full restart for a correctable typo.
			o.noteFailure(req.email)
			req.mu.Lock()
			req.loginError = fmt.Errorf("OTP authentication failed: %w", err)
			req.otpRequired = true
			req.mu.Unlock()
			o.authLoginPage(w, r, req)
			return
		}
		// A verified OTP authenticates the owner, so discharge any
		// accumulated brute-force ban for this email.
		o.resetThrottle(req.email)
	} else {
		if err := o.auth.CompleteLogin(r.Context(), res.Token, o.keyName, false); err != nil {
			// LoginCheck already verified the password; a completion failure is a
			// transient backend error, not a credential guess, so it must not
			// advance the brute-force lockout counter (which could lock the
			// account owner out for a valid password across repeated backend
			// errors). Fail the request so the wizard can retry sign-in, and render
			// the failure page so the browser shows the error instead of a silent
			// empty response (matching the failed-password path).
			o.failReq(req, fmt.Errorf("login completion failed: %w", err))
			o.authLoginPage(w, r, req)
			return
		}
		// The password was verified and the login completed: the owner is
		// present, so discharge any accumulated brute-force ban for this email.
		o.resetThrottle(req.email)
	}
	o.completeReq(req)
	o.authSuccessPage(w, r)
}

// throttleKey identifies the brute-force counter for an email. It is keyed on
// the email alone; the per-request CSRF token protects against cross-origin
// abuse, and the flat cooldown keeps an attacker to one guess-burst per window.
func throttleKey(email string) string { return email }

// throttleFor returns (creating if needed) the attempt counter for an email.
// Callers must hold o.mu.
func (o *OutOfBandLogin) throttleFor(email string) *loginThrottle {
	key := throttleKey(email)
	t, ok := o.throttle[key]
	if !ok {
		t = &loginThrottle{}
		o.throttle[key] = t
	}
	return t
}

// throttleLocked reports whether an email is currently locked out. It does NOT
// refresh the idle-decay timestamp: only an actual credential attempt
// (noteFailure) touches lastUsed, so a blocked attacker cannot keep the cooldown
// alive by hammering the endpoint.
func (o *OutOfBandLogin) throttleLocked(email string) bool {
	o.mu.Lock()
	t := o.throttleFor(email)
	o.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	return time.Now().Before(t.lockedUntil)
}

// resetThrottle clears the brute-force counter for an email after a legitimate
// login succeeds (correct password, or a verified OTP). The owner authenticating
// proves the email's holder is present, so the accumulated ban is discharged. It
// also resets lastUsed so the decay clock starts fresh.
func (o *OutOfBandLogin) resetThrottle(email string) {
	o.mu.Lock()
	t := o.throttleFor(email)
	o.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = 0
	t.lockedUntil = time.Time{}
	t.lastUsed = time.Time{}
}

// noteFailure records a failed credential attempt for an email and, once the
// burst threshold is reached, locks it out for a flat loginLockout cooldown.
// Keyed on email and living on the coordinator, neither restarting a login nor
// churning sessions can reset the counter. A short flat cooldown blunts password
// brute-force without escalation or session-scoping complexity; a successful
// login (resetThrottle) discharges it.
func (o *OutOfBandLogin) noteFailure(email string) {
	o.mu.Lock()
	t := o.throttleFor(email)
	o.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if now.Sub(t.lastUsed) > throttleIdleTTL {
		t.failed = 0
	}
	t.lastUsed = now
	t.failed++
	if t.failed >= maxLoginAttempts {
		t.lockedUntil = now.Add(loginLockout)
		t.failed = 0
	}
}

// authLoginPage renders the branded password/OTP form for a pending login
// request.
func (o *OutOfBandLogin) authLoginPage(w http.ResponseWriter, r *http.Request, req *loginRequest) {
	req.mu.Lock()
	otpStage := req.otpRequired
	var errMsg string
	if req.loginError != nil {
		errMsg = req.loginError.Error()
	}
	csrfToken := req.csrfToken
	req.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = authLoginPage(authLoginPageData{
		Action:    o.loginURL(req.id),
		Email:     req.email,
		CSRFToken: csrfToken,
		OTPStage:  otpStage,
		Error:     errMsg,
	}).Render(r.Context(), w)
}

// authSuccessPage confirms an out-of-band login completed.
func (o *OutOfBandLogin) authSuccessPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = authSuccessPage().Render(r.Context(), w)
}

// loginNotActive renders the shared branded "link no longer active" page for a
// login URL that is no longer actionable (already used or expired). It keeps a
// 410 Gone status so programmatic clients still see a spent resource, while
// rendering a human-readable page body in place of the old bare "410 gone"
// text.
func (o *OutOfBandLogin) loginNotActive(w http.ResponseWriter, r *http.Request, reason handoffNotActiveReason) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	_ = handoffNotActivePage(reason, "This sign-in link cannot be used again.").Render(r.Context(), w)
}

// loginSpentReason reports whether a login request is no longer actionable
// because its link has been spent: handoffUsed (loginCompleted, no error) means
// the URL was already used, and handoffExpired means its TTL elapsed. It returns
// "" for a still-pending request or one that FAILED (completed with an error),
// which must keep showing the login form so the human can retry.
func (o *OutOfBandLogin) loginSpentReason(req *loginRequest) handoffNotActiveReason {
	req.mu.Lock()
	defer req.mu.Unlock()
	if req.status == loginExpired {
		return handoffExpired
	}
	if req.status == loginCompleted && req.loginError == nil {
		return handoffUsed
	}
	return ""
}

// reaper periodically expires stale login requests.
func (o *OutOfBandLogin) reaper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			o.mu.Lock()
			for id, req := range o.requests {
				req.mu.Lock()
				if req.status == loginPending && now.Sub(req.created) > pendingLoginTTL {
					req.status = loginExpired
				}
				// Remove requests in a terminal state. Expired requests are always
				// removed; completed requests are removed after a grace period so
				// an abandoned session (one that never polls pendingOutcome again)
				// cannot leak the request in memory for the process lifetime.
				expired := req.status == loginExpired
				completedStale := req.status == loginCompleted && !req.completedAt.IsZero() && now.Sub(req.completedAt) > pendingLoginTTL
				var reason handoffNotActiveReason
				if req.status == loginExpired {
					reason = handoffExpired
				} else if req.status == loginCompleted && req.loginError == nil {
					reason = handoffUsed
				}
				req.mu.Unlock()
				if expired || completedStale {
					// Record a tombstone so a re-opened URL after eviction still
					// renders the spent page instead of a bare 404. A completed
					// request with an error is retryable while pending; once it is
					// evicted as completed-stale it is no longer actionable, so
					// default to the "used" reason.
					if reason == "" {
						reason = handoffUsed
					}
					o.spent[id] = spentLogin{at: now, reason: reason}
					delete(o.requests, id)
				}
			}
			// Prune spent tombstones older than spentHoldTTL so the map cannot
			// grow without bound over a long-running MCP process.
			for id, sl := range o.spent {
				if now.Sub(sl.at) > spentHoldTTL {
					delete(o.spent, id)
				}
			}
			// Evict credential throttles idle past their TTL so the map cannot
			// grow without bound over the process lifetime. A throttle in an
			// active lockout is never pruned, so the cooldown still holds.
			for key, th := range o.throttle {
				th.mu.Lock()
				idle := now.Sub(th.lastUsed) > throttleIdleTTL
				locked := now.Before(th.lockedUntil)
				th.mu.Unlock()
				if idle && !locked {
					delete(o.throttle, key)
				}
			}
			o.mu.Unlock()
		}
	}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// strongRandomID returns a 128-bit random identifier (16 bytes, hex-encoded).
// It backs one-time hand-off URLs that guard high-value secrets (a vault
// recovery mnemonic), where 64-bit entropy in randomID is too guessable on an
// otherwise unauthenticated route.
func strongRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sameOrigin reports whether an inbound request originates from one of the
// given acceptable origins. The login endpoint is browser-only: a browser form
// POST always carries an Origin header (and usually a Referer), so a request
// whose Origin matches none of the accepted origins, or that carries neither
// header, is rejected. This blocks CSRF from a cross-origin web page and
// prevents non-browser clients from driving credential attempts against the
// loopback endpoint.
func sameOrigin(r *http.Request, accepted ...string) bool {
	matches := func(candidate string) bool {
		if candidate == "" {
			return false
		}
		for _, a := range accepted {
			if strings.EqualFold(candidate, a) {
				return true
			}
		}
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return matches(origin)
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		if err != nil {
			return false
		}
		return matches(u.Scheme + "://" + u.Host)
	}
	return false
}
