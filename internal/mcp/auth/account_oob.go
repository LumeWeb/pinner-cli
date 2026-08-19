package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// OOBAccountChange completes an account credential change (password or email)
// in the browser so the secret never transits the MCP/LLM channel.
//
// The human must already be authenticated to their Pinner account (e.g. via the
// out-of-band sign-in flow); the coordinator enforces that by calling the
// authenticated AuthService.UpdatePassword / UpdateEmail on submit, which fails
// unless a valid session is present. The account_password_update and
// account_email_change tools surface the one-time page only when the account is
// authenticated, steering the human to auth_sso otherwise.
//
// It is a collect-direction hand-off built on the shared handoffEndpoint core,
// which supplies the one-time/expiring URL, loopback-or-shared-mux bootstrap,
// and CSRF origin guard. This type supplies the form (GET) and the update
// (POST) behavior. The browser POST runs synchronously and renders the result
// on the page, so no async resume machinery is needed.
//
// A per-token CSRF token (double-submit) is generated when the page is minted
// and verified on POST, matching the out-of-band login form's protection for a
// credential form.
type OOBAccountChange struct {
	svc  AuthService
	core handoff.Endpoint

	// mu guards the outcome records.
	mu       sync.Mutex
	outcomes map[string]*accountChangeOutcome
}

// accountChangeOp selects which account credential the OOB page changes.
type accountChangeOp string

const (
	opChangePassword accountChangeOp = "change-password"
	opChangeEmail    accountChangeOp = "change-email"
)

// accountChangePayload is the per-token context of a pending change.
type accountChangePayload struct {
	op   accountChangeOp
	csrf string
}

// accountChangeOutcome records the terminal result of a submitted change so
// a status helper (MCP App) can report done vs failed vs expired.
type accountChangeOutcome struct {
	succeeded bool
	err       string
	started   time.Time
}

// DefaultAccountChangeTTL is how long an OOB account change URL stays valid.
const DefaultAccountChangeTTL = 30 * time.Minute

// NewOOBAccountChange creates an out-of-band account-change coordinator backed
// by the given (authenticated) AuthService. When svc is nil the served page
// reports the flow is not configured, mirroring OOBRestore.
func NewOOBAccountChange(svc AuthService, ttl time.Duration) *OOBAccountChange {
	if ttl <= 0 {
		ttl = DefaultAccountChangeTTL
	}
	c := &OOBAccountChange{
		svc:      svc,
		outcomes: map[string]*accountChangeOutcome{},
	}
	c.core = *handoff.New("account", c, ttl)
	return c
}

// SetBaseURL sets the externally reachable base URL used to build page URLs.
func (c *OOBAccountChange) SetBaseURL(baseURL string) {
	c.core.SetBaseURL(baseURL)
}

// WithLogger sets the zap logger the coordinator uses for lifecycle events.
func (c *OOBAccountChange) WithLogger(l *zap.Logger) *OOBAccountChange {
	c.core.WithLogger(l)
	return c
}

// registerHandlers mounts the change pages + POST routes on the shared mux.
func (c *OOBAccountChange) RegisterHandlers(mux *http.ServeMux) {
	c.core.RegisterHandlers(mux)
}

// Register mints a one-time, expiring URL that lets the human change the given
// account credential in the browser. Non-blocking: the change runs on POST.
func (c *OOBAccountChange) Register(op accountChangeOp) string {
	// The CSRF secret rides inside the minted handoff item: mint derives the
	// outbound token from the same payload, so POST reads payload.csrf (on the
	// item) rather than a separately-keyed lookup. No coordinator state is
	// written here and nothing is leaked on every invocation.
	csrf := session.StrongRandomID()
	return c.core.Mint(&accountChangePayload{op: op, csrf: csrf})
}

// Stop shuts down the loopback listener, if any.
func (c *OOBAccountChange) Stop(ctx context.Context) {
	c.core.Stop(ctx)
}

// consumeOnGET reports that a GET does NOT consume the change token (it is
// collected on POST; the form must be viewable/reloadable before submit).
func (c *OOBAccountChange) ConsumeOnGET() bool { return false }

// renderGET implements handoffHandler: show the one-time form for the op.
func (c *OOBAccountChange) RenderGET(w http.ResponseWriter, r *http.Request, token string, item *handoff.Item) {
	Payload, _ := item.Payload.(*accountChangePayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	action := c.changePath(token)
	switch Payload.op {
	case opChangePassword:
		_ = accountPasswordPage(token, Payload.csrf, action).Render(r.Context(), w)
	case opChangeEmail:
		_ = accountEmailPage(token, Payload.csrf, action).Render(r.Context(), w)
	default:
		http.NotFound(w, r)
	}
}

// accountChangeChangedTitle returns the success-page title for an op.
func accountChangeChangedTitle(op accountChangeOp) string {
	if op == opChangeEmail {
		return "Email changed"
	}
	return "Password changed"
}

// changePath returns the route path for a token (matches the core prefix).
func (c *OOBAccountChange) changePath(token string) string {
	return "/account/" + token
}

// consumePOST implements handoffHandler: run the authenticated change and
// consume the token (single use). The browser POST is synchronous: success or
// failure is rendered on the page.
func (c *OOBAccountChange) ConsumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoff.Item) (consumed bool) {
	Payload, _ := item.Payload.(*accountChangePayload)

	// Per-token double-submit CSRF check, mirroring auth_login. The Origin
	// check in the core is a secondary defense-in-depth layer.
	c.mu.Lock()
	expected := Payload.csrf
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
		w.Write([]byte("Account change is not configured for this server.\n"))
		return
	}

	// Validation failures do NOT consume the token: nothing ran, so the
	// one-time URL stays valid for the human to retry.
	renderErr := func(msg string) {
		w.Header().Set("Cache-Control", "no-store")
		_ = accountChangeErrorPage(msg).Render(r.Context(), w)
	}

	var err error
	switch Payload.op {
	case opChangePassword:
		current := r.FormValue("current_password")
		next := r.FormValue("new_password")
		confirm := r.FormValue("confirm_password")
		if current == "" || next == "" {
			renderErr("Both your current and new password are required.")
			return
		}
		if next != confirm {
			renderErr("The new password and its confirmation do not match.")
			return
		}
		err = c.svc.UpdatePassword(r.Context(), current, next)
	case opChangeEmail:
		email := r.FormValue("email")
		password := r.FormValue("password")
		if email == "" {
			renderErr("A new email address is required.")
			return
		}
		err = c.svc.UpdateEmail(r.Context(), email, password)
	default:
		http.NotFound(w, r)
		return
	}

	// Claim the token atomically BEFORE the blocking update so a concurrent or
	// repeated POST cannot run the change twice.
	if !c.core.Claim(token) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "A change is already in progress or this link was already used.", http.StatusGone)
		return
	}
	consumed = true

	out := &accountChangeOutcome{started: time.Now()}
	c.mu.Lock()
	c.pruneOutcomesLocked(time.Now().Add(-DefaultAccountChangeTTL))
	c.outcomes[token] = out
	c.mu.Unlock()
	settle := func(succeeded bool, errText string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		out.succeeded, out.err = succeeded, errText
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	if err != nil {
		settle(false, err.Error())
		_ = accountChangeErrorPage(err.Error()).Render(r.Context(), w)
		return
	}
	settle(true, "")
	_ = accountChangeChangedPage(Payload.op).Render(r.Context(), w)
	return
}

func (c *OOBAccountChange) Count() int { return c.core.Count() }

func (c *OOBAccountChange) SetNow(f func() time.Time) { c.core.SetNow(f) }

// tokenDone reports the coordinator token's state to a status helper. The
// browser POST is synchronous, so a settled success is done, a settled error is
// failed, an expired token is expired, and everything else is pending.
func (c *OOBAccountChange) tokenDone(token string) (done, failed, expired, pending bool) {
	if token == "" {
		return false, false, false, false
	}
	item, reason := c.core.Resolve(token)
	if item != nil {
		return false, false, false, true // still live: form not yet submitted
	}
	if reason == handoff.ReasonExpired {
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
func (c *OOBAccountChange) forgetOutcome(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.outcomes, token)
}

// pruneOutcomesLocked prunes terminal outcome records older than the TTL.
func (c *OOBAccountChange) pruneOutcomesLocked(cutoff time.Time) {
	for token, out := range c.outcomes {
		if (out.succeeded || out.err != "") && out.started.Before(cutoff) {
			delete(c.outcomes, token)
		}
	}
}
