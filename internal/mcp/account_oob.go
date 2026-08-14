package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OOBAccountPasswordChange completes an account password change in the browser
// so the current + new passwords never transit the MCP/LLM channel.
//
// The human must already be authenticated to their Pinner account (e.g. via the
// out-of-band sign-in flow); the coordinator enforces that by calling the
// authenticated AuthService.UpdatePassword on submit, which fails unless a
// valid session is present. The account_password_update tool surfaces the
// one-time /account/password/<token> page only when the account is
// authenticated, steering the human to auth_sso otherwise.
//
// It is a collect-direction hand-off built on the shared handoffEndpoint core,
// which supplies the one-time/expiring URL, loopback-or-shared-mux bootstrap,
// and CSRF origin guard. This type supplies the password form (GET) and the
// update (POST) behavior. The browser POST runs synchronously and renders the
// result on the page, so no async resume machinery is needed.
//
// A per-token CSRF token (double-submit) is generated when the page is minted
// and verified on POST, matching the out-of-band login form's protection for a
// credential form.
type OOBAccountPasswordChange struct {
	svc AuthService
	core handoffEndpoint

	// mu guards the per-token CSRF tokens and outcomes.
	mu        sync.Mutex
	csrf      map[string]string
	outcomes  map[string]*accountPasswordOutcome
}

// accountPasswordPayload is the per-token context of a pending password change.
// csrf is the per-token double-submit secret embedded in the form and required
// back on POST.
type accountPasswordPayload struct {
	csrf string
}

// accountPasswordOutcome records the terminal result of a submitted change so
// a status helper (MCP App) can report done vs failed vs expired.
type accountPasswordOutcome struct {
	succeeded bool
	err       string
	started   time.Time
}

// DefaultAccountPasswordTTL is how long an OOB account password-change URL
// stays valid.
const DefaultAccountPasswordTTL = 30 * time.Minute

// NewOOBAccountPasswordChange creates an out-of-band password-change
// coordinator backed by the given (authenticated) AuthService. When svc is nil
// the served page reports the flow is not configured, mirroring OOBRestore.
func NewOOBAccountPasswordChange(svc AuthService, ttl time.Duration) *OOBAccountPasswordChange {
	if ttl <= 0 {
		ttl = DefaultAccountPasswordTTL
	}
	c := &OOBAccountPasswordChange{
		svc:      svc,
		csrf:     map[string]string{},
		outcomes: map[string]*accountPasswordOutcome{},
	}
	c.core = *newHandoff("account/password", c, ttl)
	return c
}

// SetBaseURL sets the externally reachable base URL used to build the page URL.
func (c *OOBAccountPasswordChange) SetBaseURL(baseURL string) {
	c.core.SetBaseURL(baseURL)
}

// WithLogger sets the zap logger the coordinator uses for lifecycle events.
func (c *OOBAccountPasswordChange) WithLogger(l *zap.Logger) *OOBAccountPasswordChange {
	c.core.WithLogger(l)
	return c
}

// registerHandlers mounts the password page + POST routes on the shared mux.
func (c *OOBAccountPasswordChange) registerHandlers(mux *http.ServeMux) {
	c.core.registerHandlers(mux)
}

// Register mints a one-time, expiring URL that lets the human change their
// password in the browser. Non-blocking: the change runs on form POST.
func (c *OOBAccountPasswordChange) Register() string {
	csrf := strongRandomID()
	c.mu.Lock()
	defer c.mu.Unlock()
	token := strongRandomID()
	c.csrf[token] = csrf
	return c.core.mint(&accountPasswordPayload{csrf: csrf})
}

// Stop shuts down the loopback listener, if any.
func (c *OOBAccountPasswordChange) Stop(ctx context.Context) {
	c.core.Stop(ctx)
}

// consumeOnGET reports that a GET does NOT consume the password-change token
// (it is collected on POST; the form must be viewable/reloadable before submit).
func (c *OOBAccountPasswordChange) consumeOnGET() bool { return false }

// renderGET implements handoffHandler: show the one-time password form.
func (c *OOBAccountPasswordChange) renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) {
	payload, _ := item.payload.(*accountPasswordPayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = accountPasswordPage(token, payload.csrf, "/account/password/"+token).Render(r.Context(), w)
}

// consumePOST implements handoffHandler: run the authenticated password update
// and consume the token (single use). The browser POST is synchronous: success
// or failure is rendered on the page, so the human (and, via the page, the
// agent/human collaborator) learns the outcome immediately.
func (c *OOBAccountPasswordChange) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) (consumed bool) {
	payload, _ := item.payload.(*accountPasswordPayload)

	// Per-token double-submit CSRF check, mirroring auth_login. The Origin
	// check in the core is a secondary defense-in-depth layer.
	c.mu.Lock()
	expected := payload.csrf
	c.mu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(expected)) != 1 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Missing service is a server misconfiguration: do NOT consume the token.
	if c.svc == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Account password change is not configured for this server.\n"))
		return
	}

	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	// Validation failures do NOT consume the token: nothing ran, so the
	// one-time URL stays valid for the human to retry.
	if current == "" || next == "" {
		w.Header().Set("Cache-Control", "no-store")
		_ = accountPasswordChangeErrorPage("Both your current and new password are required.").Render(r.Context(), w)
		return
	}
	if next != confirm {
		w.Header().Set("Cache-Control", "no-store")
		_ = accountPasswordChangeErrorPage("The new password and its confirmation do not match.").Render(r.Context(), w)
		return
	}

	// Claim the token atomically BEFORE the blocking update so a concurrent or
	// repeated POST cannot run the change twice.
	if !c.core.claim(token) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "A password change is already in progress or this link was already used.", http.StatusGone)
		return
	}
	consumed = true

	out := &accountPasswordOutcome{started: time.Now()}
	c.mu.Lock()
	c.pruneOutcomesLocked(time.Now().Add(-DefaultAccountPasswordTTL))
	c.outcomes[token] = out
	c.mu.Unlock()
	settle := func(succeeded bool, errText string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		out.succeeded, out.err = succeeded, errText
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	if err := c.svc.UpdatePassword(r.Context(), current, next); err != nil {
		settle(false, err.Error())
		_ = accountPasswordChangeErrorPage(err.Error()).Render(r.Context(), w)
		return
	}
	settle(true, "")
	_ = accountPasswordChangedPage().Render(r.Context(), w)
	return
}

func (c *OOBAccountPasswordChange) count() int { return c.core.count() }

func (c *OOBAccountPasswordChange) setNow(f func() time.Time) { c.core.setNow(f) }

// tokenDone reports the coordinator token's state to a status helper. The
// browser POST is synchronous, so a settled success is done, a settled error is
// failed, an expired token is expired, and everything else is pending.
func (c *OOBAccountPasswordChange) tokenDone(token string) (done, failed, expired, pending bool) {
	if token == "" {
		return false, false, false, false
	}
	item, reason := c.core.resolve(token)
	if item != nil {
		return false, false, false, true // still live: form not yet submitted
	}
	if reason == handoffExpired {
		return false, false, true, false
	}
	c.mu.Lock()
	out, ok := c.outcomes[token]
	var succeeded bool
	var errText string
	if ok {
		succeeded, errText = out.succeeded, out.err
	}
	c.mu.Unlock()
	if !ok {
		return false, false, false, false // spent tombstone evicted; dead
	}
	if errText != "" {
		return false, true, false, false
	}
	if !succeeded {
		return false, false, false, true
	}
	return true, false, false, false
}

// forgetOutcome drops the outcome record for a token once a continuation has
// consumed its terminal result.
func (c *OOBAccountPasswordChange) forgetOutcome(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.outcomes, token)
}

// pruneOutcomesLocked prunes terminal outcome records older than the TTL.
func (c *OOBAccountPasswordChange) pruneOutcomesLocked(cutoff time.Time) {
	for token, out := range c.outcomes {
		if (out.succeeded || out.err != "") && out.started.Before(cutoff) {
			delete(c.outcomes, token)
		}
	}
}
