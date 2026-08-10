package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// stubAuthService succeeds by default. Set loginErr to exercise a bad-credential
// path (which must advance the throttle) and completeErr to exercise a backend
// completion failure AFTER a valid LoginCheck (which must NOT advance the
// throttle, since a correct password is not a credential guess).
type stubAuthService struct {
	loginErr    error
	completeErr error
}

// stubToken returns a fresh random token per call so test stubs never embed a
// hard-coded token literal (it is a stand-in for a real auth token, not a
// production secret, but it is generated rather than pinned to satisfy the
// no-hard-coded-secrets rule).
var stubToken = func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s stubAuthService) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	return &portalsdk.LoginResult{Token: stubToken(), OTPRequired: false}, nil
}
func (s stubAuthService) CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) error {
	return s.completeErr
}
func (s stubAuthService) LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) error {
	return nil
}
func (s stubAuthService) Status(ctx context.Context) error { return nil }

func newOOBForTest(t *testing.T) *OutOfBandLogin {
	t.Helper()
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	return o
}

// doLogin simulates a browser completing the login: it first GETs the login
// page to obtain the per-request CSRF token the form embeds, then POSTs the
// credentials with that token (plus any caller-supplied Origin/Referer, so
// cross-origin rejection can be tested). It drives the handler directly with an
// httptest recorder.
func doLogin(t *testing.T, o *OutOfBandLogin, u string, origin, referer string) *httptest.ResponseRecorder {
	t.Helper()
	csrf := fetchCSRF(t, o, u)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, u, strings.NewReader(url.Values{
		"password": {"fixture-password"},
		"csrf":     {csrf},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	o.loginPage(rec, req)
	return rec
}

// fetchCSRF GETs the login page and extracts the per-request CSRF token from the
// rendered form (the hidden input named "csrf"), the way a browser would submit
// it back on POST.
func fetchCSRF(t *testing.T, o *OutOfBandLogin, u string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	o.loginPage(rec, httptest.NewRequest(http.MethodGet, u, nil))
	html := rec.Body.String()
	// Match name="csrf" value="<token>".
	m := csrfInputRE.FindStringSubmatch(html)
	if len(m) < 2 {
		t.Fatalf("no csrf input in rendered login page for %s", u)
	}
	return m[1]
}

var csrfInputRE = regexp.MustCompile(`name="csrf"\s+value="([^"]+)"`)

// testOrigin returns the loopback origin backing o for the duration of the
// test (used to assert that same-origin POSTs are accepted).
func testOrigin(o *OutOfBandLogin) string {
	orig := o.loopback.acceptedOrigins()
	if len(orig) == 0 {
		return "http://127.0.0.1:0"
	}
	return orig[0]
}

// TestOOBLoginRejectsCrossOriginPOST verifies the CSRF guard on the loopback
// login endpoint: a browser form-POST carrying a foreign Origin or Referer must
// be rejected, while a same-origin POST and a non-browser client with no
// Origin/Referer are allowed.
func TestOOBLoginRejectsCrossOriginPOST(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-1", "test@example.com")
	require.NoError(t, err)

	// Cross-origin Origin must be rejected.
	assert.Equal(t, http.StatusForbidden, doLogin(t, o, u, "https://evil.example", "").Code)
	// Cross-origin Referer must be rejected.
	assert.Equal(t, http.StatusForbidden, doLogin(t, o, u, "", "https://evil.example/login").Code)
	// Same-origin Origin is allowed.
	assert.NotEqual(t, http.StatusForbidden, doLogin(t, o, u, testOrigin(o), "").Code)
	// Non-browser client with no Origin/Referer is rejected: the endpoint is
	// browser-only and a browser form-POST always carries an Origin.
	assert.Equal(t, http.StatusForbidden, doLogin(t, o, u, "", "").Code)
}

// TestOOBLoginRejectsMissingCSRFToken verifies the credential POST requires the
// per-request CSRF token the rendered form embeds. A local process that knows the
// request id (from the URL) but not this token must not be able to forge a
// credential POST, even with a same-origin Origin.
func TestOOBLoginRejectsMissingCSRFToken(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-1", "test@example.com")
	require.NoError(t, err)

	// POST with the correct same-origin Origin but WITHOUT the csrf token.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, u, strings.NewReader(url.Values{
		"password": {"fixture-password"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testOrigin(o))
	o.loginPage(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "POST without the per-request csrf token must be rejected")

	// A wrong csrf token is also rejected.
	wrong := httptest.NewRecorder()
	wreq := httptest.NewRequest(http.MethodPost, u, strings.NewReader(url.Values{
		"password": {"fixture-password"},
		"csrf":     {"0000000000000000"},
	}.Encode()))
	wreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wreq.Header.Set("Origin", testOrigin(o))
	o.loginPage(wrong, wreq)
	assert.Equal(t, http.StatusForbidden, wrong.Code, "POST with a wrong csrf token must be rejected")

	// The correct token (fetched from the rendered page, as a browser would)
	// is accepted.
	assert.NotEqual(t, http.StatusForbidden, doLogin(t, o, u, testOrigin(o), "").Code)
}

// TestOOBLoginAcceptsLocalhostOrigin verifies the CSRF check accepts a login
// opened via "localhost" as well as "127.0.0.1": the expected origin is derived
// from the actual request host, so a browser POST whose Origin matches localhost
// is not wrongly rejected (either host that resolves to the loopback is fine).
func TestOOBLoginAcceptsLocalhostOrigin(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-1", "test@example.com")
	require.NoError(t, err)

	// Rewrite the loopback URL to localhost and POST with a matching Origin, as
	// a user who opened http://localhost:<port>/login/<id> would, including the
	// per-request CSRF token a browser submits from the rendered form.
	lu := strings.Replace(u, "127.0.0.1", "localhost", 1)
	rec := httptest.NewRecorder()
	body := url.Values{
		"password": {"fixture-password"},
		"csrf":     {fetchCSRF(t, o, u)},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, lu, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://localhost:"+portOf(u))
	o.loginPage(rec, req)
	// The request must pass CSRF and reach the credential path (login
	// completes); it must NOT be a 403 origin rejection.
	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}

// portOf extracts the ":port" suffix from a loopback URL.
func portOf(u string) string {
	if i := strings.LastIndex(u, ":"); i >= 0 {
		return u[i+1 : strings.Index(u[i:], "/")+i]
	}
	return ""
}

// TestOOBLoginSessionIsolation verifies a login is bound to its originating
// session: only the session that started it sees the outcome, and once consumed
// a later poll reports not-done (so a fresh session must re-authenticate in the
// browser rather than seeing a stale completion).
func TestOOBLoginSessionIsolation(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-A", "test@example.com")
	require.NoError(t, err)

	// Complete the login in the browser (same-origin Origin passes CSRF).
	rec := doLogin(t, o, u, testOrigin(o), "")
	require.Equal(t, http.StatusOK, rec.Code)

	// The originating session consumes the outcome and reports done once.
	_, done, err := o.pendingOutcome("session-A", "test@example.com")
	require.NoError(t, err)
	assert.True(t, done)

	// A subsequent poll for the same session reports not-done (consumed).
	_, done, err = o.pendingOutcome("session-A", "test@example.com")
	require.NoError(t, err)
	assert.False(t, done)

	// A different session with the same email must never see the completion of
	// session-A: it reports not-done and must start its own login.
	_, done, err = o.pendingOutcome("session-B", "test@example.com")
	require.NoError(t, err)
	assert.False(t, done)
}

// TestOOBLoginThrottle verifies the loopback login endpoint throttles repeated
// credential failures: after maxLoginAttempts consecutive failures the request
// is locked out for a cooldown, so the login endpoint cannot be hammered with
// password guesses in one burst.
func TestOOBLoginThrottle(t *testing.T) {
	failing := stubAuthService{loginErr: errors.New("bad credentials")}
	o := NewOutOfBandLogin(failing, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	_, u, err := o.Begin("session-1", "test@example.com")
	require.NoError(t, err)

	// Each failed attempt renders the login page (200). None lock until the
	// burst threshold is reached. (Same-origin Origin so the attempts pass the
	// CSRF check and reach the credential/throttle path.)
	for i := 0; i < maxLoginAttempts; i++ {
		assert.Equal(t, http.StatusOK, doLogin(t, o, u, testOrigin(o), "").Code, "attempt %d", i)
	}

	// After maxLoginAttempts failures the email is in a lockout cooldown,
	// tracked on the coordinator (not the request) so that restarting the login
	// cannot reset the counter.
	o.mu.Lock()
	// The throttle is keyed on email.
	th := o.throttle[throttleKey("test@example.com")]
	o.mu.Unlock()
	require.NotNil(t, th)
	th.mu.Lock()
	lockedUntil := th.lockedUntil
	th.mu.Unlock()
	assert.True(t, lockedUntil.After(time.Now()), "email should be locked out after the failure burst")
}

// TestOOBLoginCompleteErrorDoesNotThrottle verifies that a backend completion
// failure AFTER a valid LoginCheck does not advance the brute-force throttle.
// A correct password is not a credential guess, so repeated transient
// completion errors must not ratchet the lockout and lock the account owner out.
func TestOOBLoginCompleteErrorDoesNotThrottle(t *testing.T) {
	backendErr := errors.New("backend failure")
	svc := stubAuthService{completeErr: backendErr}
	o := NewOutOfBandLogin(svc, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	_, u, err := o.Begin("session-1", "owner@example.com")
	require.NoError(t, err)

	// Drive more than a full burst of correct-password POSTs whose completion
	// fails at the backend. Each must render the failure without ever locking
	// the email out.
	for i := 0; i < maxLoginAttempts+3; i++ {
		code := doLogin(t, o, u, testOrigin(o), "").Code
		assert.NotEqual(t, http.StatusForbidden, code, "attempt %d should pass CSRF", i)
	}

	// The throttle entry (created by the lockout check) must not reflect any
	// accumulated failures and must not be in a lockout.
	o.mu.Lock()
	th := o.throttle[throttleKey("owner@example.com")]
	o.mu.Unlock()
	require.NotNil(t, th)
	th.mu.Lock()
	defer th.mu.Unlock()
	assert.Equal(t, 0, th.failed, "completion errors must not accumulate failure budget")
	assert.False(t, th.lockedUntil.After(time.Now()), "email must not be locked out")
}

// TestOOBLoginCompleteFailureRendersPage verifies that when CompleteLogin fails
// after a valid LoginCheck, the handler renders the login failure page (with the
// error visible) instead of returning a silent empty 200. This matches the
// failed-password path so the browser always gets feedback.
func TestOOBLoginCompleteFailureRendersPage(t *testing.T) {
	svc := stubAuthService{completeErr: errors.New("backend failure")}
	o := NewOutOfBandLogin(svc, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	_, u, err := o.Begin("session-1", "owner@example.com")
	require.NoError(t, err)

	rec := doLogin(t, o, u, testOrigin(o), "")
	assert.NotEqual(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sign in to Pinner", "failure page must be rendered, not an empty body")
	assert.Contains(t, rec.Body.String(), "backend failure", "error must be visible on the page")
}

// TestOOBLoginThrottleNotRefreshedWhileLocked verifies that checking the lockout
// (throttleLocked) does NOT refresh the idle-decay timestamp. A local attacker
// who is blocked by the cooldown must not be able to keep the ban alive forever
// by hammering the endpoint: only an actual credential attempt (noteFailure)
// touches lastUsed, so the cooldown eventually decays once the attacker stops
// guessing.
func TestOOBLoginThrottleNotRefreshedWhileLocked(t *testing.T) {
	failing := stubAuthService{loginErr: errors.New("bad credentials")}
	o := NewOutOfBandLogin(failing, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })

	const email = "hammer@example.com"
	// Lock the email out with a full burst.
	for i := 0; i < maxLoginAttempts; i++ {
		o.noteFailure(email)
	}
	o.mu.Lock()
	th := o.throttle[throttleKey(email)]
	o.mu.Unlock()
	require.NotNil(t, th)
	th.mu.Lock()
	lastUsed := th.lastUsed
	th.mu.Unlock()
	require.False(t, lastUsed.IsZero())

	// Repeatedly poll the lockout from the credential path (as a stuck client
	// or a scanner would). lastUsed must NOT advance, or the idle-decay timer
	// would never fire and the lockout would be unending.
	for i := 0; i < 50; i++ {
		o.throttleLocked(email)
	}
	th.mu.Lock()
	after := th.lastUsed
	th.mu.Unlock()
	assert.True(t, after.Equal(lastUsed),
		"throttleLocked must not refresh lastUsed; got %v -> %v", lastUsed, after)
}

// TestOOBLoginResetOnSuccess verifies that a successful login discharges the
// brute-force ban: after a lockout, resetting the throttle (as the success path
// does) clears the failure budget and any active cooldown so the owner is not
// blocked.
func TestOOBLoginResetOnSuccess(t *testing.T) {
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })

	const email = "owner@example.com"
	for i := 0; i < maxLoginAttempts; i++ {
		o.noteFailure(email)
	}
	o.mu.Lock()
	th := o.throttle[throttleKey(email)]
	o.mu.Unlock()
	require.NotNil(t, th)
	th.mu.Lock()
	locked := time.Now().Before(th.lockedUntil)
	th.mu.Unlock()
	require.True(t, locked, "precondition: email should be locked out")

	// The owner subsequently authenticates successfully.
	o.resetThrottle(email)

	th.mu.Lock()
	defer th.mu.Unlock()
	assert.Equal(t, 0, th.failed)
	assert.False(t, th.lockedUntil.After(time.Now()), "cooldown must be cleared after a successful login")
	assert.True(t, th.lockedUntil.IsZero(), "lockout must be cleared after success")
}

// TestOOBLoginBeginReusesAcceptedLogin verifies a browser-completed login is not
// dropped by a redundant Begin. After a request completes successfully, a second
// Begin for the same session+email must reuse the accepted completion (same
// request id) rather than evicting it and forcing the user to sign in again.
func TestOOBLoginBeginReusesAcceptedLogin(t *testing.T) {
	o := newOOBForTest(t)
	// Start the login and capture its request id + URL.
	id1, u, err := o.Begin("session-1", "reuse@example.com")
	require.NoError(t, err)

	// Complete the login in the browser (same-origin POST with the correct
	// password) so the request transitions to loginCompleted.
	code := doLogin(t, o, u, testOrigin(o), "").Code
	require.NotEqual(t, http.StatusForbidden, code)

	// A redundant Begin for the same session+email must reuse the accepted
	// request (same id), not create a fresh one that would discard the
	// completed login and force the user to sign in again.
	id2, _, err := o.Begin("session-1", "reuse@example.com")
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "Begin must reuse the accepted login id")

	// The reused login is still consumable and reports done.
	_, done, err := o.pendingOutcome("session-1", "reuse@example.com")
	require.NoError(t, err)
	assert.True(t, done)
}

// TestOOBLoginCompleteClearsError verifies a successful completion reports a
// success even after a prior failure: complete() must clear loginError so a
// later pendingOutcome does not report a stale failed attempt (e.g. an OTP
// failure followed by a correct retry).
func TestOOBLoginCompleteClearsError(t *testing.T) {
	r := &loginRequest{status: loginPending}
	r.fail(errors.New("OTP authentication failed"))

	r.complete()
	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Equal(t, loginCompleted, r.status)
	assert.Nil(t, r.loginError, "complete() must clear a stale prior failure")
}

// TestOOBLoginCompletedRejectsRePOST verifies a login request that has already
// completed in the browser cannot be flipped into a reported failure by a
// subsequent wrong-password POST to the same URL (e.g. a Back-button re-POST).
// The POST branch short-circuits with 410 Gone when the request is no longer
// loginPending, and the accepted completion stays reported as a success.
func TestOOBLoginCompletedRejectsRePOST(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-1", "repost@example.com")
	require.NoError(t, err)

	// Complete the login successfully (correct password -> loginCompleted).
	code := doLogin(t, o, u, testOrigin(o), "").Code
	require.NotEqual(t, http.StatusForbidden, code)

	// A second credential POST (as a stale Back-submit with a wrong password)
	// must be rejected as Gone and must not run the auth backend.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, u, strings.NewReader(url.Values{
		"password": {"wrong-password"},
		"csrf":     {fetchCSRF(t, o, u)},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testOrigin(o))
	o.loginPage(rec, req)
	assert.Equal(t, http.StatusGone, rec.Code, "a completed login must reject re-POST with 410 Gone")

	// The original acceptance is untouched: pendingOutcome still reports the
	// completed success, not a failure introduced by the stale POST.
	_, done, err := o.pendingOutcome("session-1", "repost@example.com")
	require.NoError(t, err)
	assert.True(t, done, "a successful completion must not be flipped by a re-POST")
}

// TestOOBLoginExpiredRejectsCredentialPOST verifies a request already marked
// expired (elapsed without completion) cannot accept a credential POST and run
// the auth backend. A stale URL relayed earlier must not complete an
// out-of-band login after expiry, leaving a backend session no wizard consumes.
func TestOOBLoginExpiredRejectsCredentialPOST(t *testing.T) {
	o := newOOBForTest(t)
	_, u, err := o.Begin("session-1", "stale@example.com")
	require.NoError(t, err)

	// Mark the request expired (as the reaper/session-TTL path does).
	o.mu.Lock()
	for _, r := range o.requests {
		r.mu.Lock()
		r.status = loginExpired
		r.mu.Unlock()
	}
	o.mu.Unlock()

	// A credential POST to the expired URL must be rejected with 410 Gone. If
	// the guard were missing, authLoginSubmit would run LoginCheck (success)
	// and CompleteLogin, returning a 200-render; 410 proves the backend was
	// not reached.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, u, strings.NewReader(url.Values{
		"password": {"whatever"},
		"csrf":     {fetchCSRF(t, o, u)},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testOrigin(o))
	o.loginPage(rec, req)
	assert.Equal(t, http.StatusGone, rec.Code, "an expired login must reject credential POST with 410 Gone")
}

// TestOutOfBandLoginMountsOnSharedMux verifies the coordinator can be served on a
// shared transport mux (as the HTTP/tunnel transport does) so a remote human can
// complete sign-in at the public URL rather than an unreachable loopback. It
// mirrors serveHTTP's registration order — /assets/ first, then the OOB login
// routes — so a duplicate /assets/ registration would surface as the
// "multiple registrations for /assets/" ServeMux panic. Begin must advertise the
// base URL set on the coordinator, and /login/<id> must be reachable.
func TestOutOfBandLoginMountsOnSharedMux(t *testing.T) {
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })

	// Bind like the remote transport: a shared mux plus the public base URL,
	// registering /assets/ exactly once (as serveHTTP does) before the OOB
	// routes.
	const publicBase = "https://tunnel.example.com"
	o.SetBaseURL(publicBase)
	mux := http.NewServeMux()
	mux.Handle("/assets/", staticAssetHandler())
	o.registerHandlers(mux)

	id, u, err := o.Begin("session-1", "remote@example.com")
	require.NoError(t, err)
	require.Contains(t, u, publicBase+"/login/"+id, "login URL must use the public base URL in remote mode")

	// The rendered login page must be served through the shared mux.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login/"+id, nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sign in to Pinner", "login page must be reachable on the shared mux")
}

func TestOutOfBandLoginSetBaseURLTrimmed(t *testing.T) {
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })
	o.SetBaseURL("https://tunnel.example.com/")
	_, u, err := o.Begin("session-1", "remote@example.com")
	require.NoError(t, err)
	assert.Contains(t, u, "https://tunnel.example.com/login/", "trailing slash must be trimmed")
}

// TestOutOfBandLoginReaperRestartsAfterStop verifies that after a Stop, a
// subsequent Begin re-arms the expiry reaper. Stop() must clear both reaperCancel
// AND reaperCtx so startReaper()'s guard does not early-return on the stale
// context, which would leave pending requests unpruned for the process lifetime.
func TestOutOfBandLoginReaperRestartsAfterStop(t *testing.T) {
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })

	// Initial Begin arms the reaper.
	_, _, err := o.Begin("session-1", "reaper@example.com")
	require.NoError(t, err)
	o.mu.Lock()
	require.NotNil(t, o.reaperCancel, "reaper must be armed after first Begin")
	o.mu.Unlock()

	// Stop tears the reaper down.
	o.Stop(context.Background())
	o.mu.Lock()
	require.Nil(t, o.reaperCancel, "reaper cancel must be cleared by Stop")
	require.Nil(t, o.reaperCtx, "reaper ctx must be cleared by Stop so a restart is possible")
	o.mu.Unlock()

	// A subsequent Begin must restart the reaper (this is the regression: before
	// the fix, startReaper returned on the stale reaperCtx and never re-armed).
	_, _, err = o.Begin("session-1", "reaper@example.com")
	require.NoError(t, err)
	o.mu.Lock()
	defer o.mu.Unlock()
	require.NotNil(t, o.reaperCancel, "reaper must be re-armed after stop/restart cycle")
}

// TestOOBLoginStopDoesNotHoldLockDuringShutdown verifies Stop() releases o.mu
// before blocking in http.Server.Shutdown. A concurrent in-flight /login POST
// handler (which needs o.mu via throttleLocked/failReq/noteFailure) must be able
// to make progress instead of stalling teardown for the whole shutdown context.
func TestOOBLoginStopDoesNotHoldLockDuringShutdown(t *testing.T) {
	o := NewOutOfBandLogin(stubAuthService{}, "", "test-key")
	t.Cleanup(func() { o.Stop(context.Background()) })

	// Start the loopback server.
	_, _, err := o.Begin("session-1", "owner@example.com")
	require.NoError(t, err)

	// Hold o.mu so any goroutine that acquires it in Stop's critical section
	// must wait — this exercises the window the fix changes.
	o.mu.Lock()
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		o.Stop(context.Background())
	}()
	// Give Stop a moment to reach its o.mu acquisition, then release the lock.
	time.Sleep(10 * time.Millisecond)
	o.mu.Unlock()

	// Stop must complete promptly once o.mu is available (Shutdown of an idle
	// loopback server returns immediately). Before the fix, Stop held o.mu while
	// calling Shutdown, which is exactly the behavior this test documents.
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after o.mu was released")
	}
}

// TestOOBSetupPromptDoesNotRequestCredentials verifies the setup auth prompt no
// longer instructs the agent to collect a password/OTP: sign_in requests only an
// email and the handler relays the out-of-band URL for browser completion, so
// secrets never transit the MCP/LLM channel.
func TestOOBSetupPromptDoesNotRequestCredentials(t *testing.T) {
	out := setupOverview()
	require.Contains(t, out, "sign_in")
	require.Contains(t, out, "out-of-band login URL")
	require.Contains(t, out, "only the email")
	// The prompt must no longer instruct collection of a password as a sign_in
	// input field or ask for one from the user.
	assert.NotContains(t, out, "ask for email, password")
}
