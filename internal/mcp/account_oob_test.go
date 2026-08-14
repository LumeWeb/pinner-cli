package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// testAccountAuthService implements the mcp AuthService subset for password OOB
// tests. It records UpdatePassword/RequestPasswordReset calls and lets tests
// force Status/UpdatePassword outcomes.
type testAccountAuthService struct {
	statusErr       error
	updateErr       error
	updateCurrent   string
	updateNew       string
	resetCalls      []string
	updateCalls     int
}

func (s *testAccountAuthService) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	return nil, nil
}
func (s *testAccountAuthService) CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error) {
	return &auth.LoginCompleteResult{}, nil
}
func (s *testAccountAuthService) LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error) {
	return &auth.LoginCompleteResult{}, nil
}
func (s *testAccountAuthService) Status(ctx context.Context) (*auth.StatusResult, error) {
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	return &auth.StatusResult{}, nil
}
func (s *testAccountAuthService) UpdatePassword(ctx context.Context, currentPassword, newPassword string) error {
	s.updateCalls++
	s.updateCurrent = currentPassword
	s.updateNew = newPassword
	return s.updateErr
}
func (s *testAccountAuthService) RequestPasswordReset(ctx context.Context, email string) error {
	s.resetCalls = append(s.resetCalls, email)
	return nil
}

// buildAccountPwServer returns a wired OOBAccountPasswordChange + mux on which
// /account/password/ routes are served.
func buildAccountPwServer(svc AuthService) (*OOBAccountPasswordChange, *http.ServeMux) {
	c := NewOOBAccountPasswordChange(svc, time.Minute)
	c.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	c.registerHandlers(mux)
	return c, mux
}

// fetchCSRF renders the GET page and extracts the hidden csrf input value.
func fetchAccountPwCSRF(t *testing.T, mux *http.ServeMux, url string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "name=\"csrf\"")
	start := strings.Index(body, `name="csrf" value="`)
	require.Greater(t, start, -1)
	rest := body[start+len(`name="csrf" value="`):]
	end := strings.Index(rest, `"`)
	require.Greater(t, end, -1)
	return rest[:end]
}

// accountPwPOST is a helper that POSTs a url-encoded form to the account
// password route with the loopback Origin set so the core's same-origin check
// admits it (mirroring create_oob_test.go).
func accountPwPOST(mux *http.ServeMux, token, form string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/account/password/"+token, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://127.0.0.1:9999")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func tokenFromURL(t *testing.T, url string) string {
	t.Helper()
	idx := strings.LastIndex(url, "/")
	require.Greater(t, idx, -1)
	return url[idx+1:]
}

// TestAccountPasswordChangeRegistersPage verifies Register mints a /account/password/<token>
// URL whose GET renders the password form.
func TestAccountPasswordChangeRegistersPage(t *testing.T) {
	c, mux := buildAccountPwServer(&testAccountAuthService{})
	url := c.Register()
	require.NotEmpty(t, url)
	require.Contains(t, url, "/account/password/")

	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Change your password")
	assert.Contains(t, body, "current_password")
	assert.Contains(t, body, "new_password")
	assert.Contains(t, body, "confirm_password")
}

// TestAccountPasswordChangeConsumeSuccess verifies a valid POST calls UpdatePassword
// with the current+new passwords and renders the success page.
func TestAccountPasswordChangeConsumeSuccess(t *testing.T) {
	svc := &testAccountAuthService{}
	c, mux := buildAccountPwServer(svc)
	url := c.Register()
	csrf := fetchAccountPwCSRF(t, mux, url)

	form := "csrf=" + urlEncode(csrf) + "&current_password=" + urlEncode("oldpass") + "&new_password=" + urlEncode("newpass") + "&confirm_password=" + urlEncode("newpass")
	w := accountPwPOST(mux, tokenFromURL(t, url), form)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Password changed")
	assert.Equal(t, 1, svc.updateCalls)
	assert.Equal(t, "oldpass", svc.updateCurrent)
	assert.Equal(t, "newpass", svc.updateNew)
	assert.Equal(t, 0, c.core.count(), "token consumed after success")
}

// TestAccountPasswordChangeConsumeMismatch verifies a confirm mismatch does not
// consume the token and renders an error.
func TestAccountPasswordChangeConsumeMismatch(t *testing.T) {
	svc := &testAccountAuthService{}
	c, mux := buildAccountPwServer(svc)
	url := c.Register()
	csrf := fetchAccountPwCSRF(t, mux, url)

	form := "csrf=" + urlEncode(csrf) + "&current_password=" + urlEncode("oldpass") + "&new_password=" + urlEncode("newpass") + "&confirm_password=" + urlEncode("different")
	w := accountPwPOST(mux, tokenFromURL(t, url), form)

	assert.Contains(t, w.Body.String(), "do not match")
	assert.Equal(t, 0, svc.updateCalls, "no UpdatePassword on mismatch")
	assert.Equal(t, 1, c.core.count(), "token NOT consumed on validation failure")
}

// TestAccountPasswordChangeRejectsBadCSRF verifies a POST with a wrong CSRF
// token is forbidden and UpdatePassword is never called.
func TestAccountPasswordChangeRejectsBadCSRF(t *testing.T) {
	svc := &testAccountAuthService{}
	c, mux := buildAccountPwServer(svc)
	url := c.Register()

	form := "csrf=wrong&current_password=old&new_password=new&confirm_password=new"
	w := accountPwPOST(mux, tokenFromURL(t, url), form)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, svc.updateCalls, "no UpdatePassword on bad CSRF")
	assert.Equal(t, 1, c.core.count(), "token NOT consumed on bad CSRF")
}

// TestAccountPasswordChangeUpdateError verifies a failing UpdatePassword renders
// the error page and leaves a failed outcome (never done).
func TestAccountPasswordChangeUpdateError(t *testing.T) {
	svc := &testAccountAuthService{updateErr: context.DeadlineExceeded}
	c, mux := buildAccountPwServer(svc)
	url := c.Register()
	csrf := fetchAccountPwCSRF(t, mux, url)

	form := "csrf=" + urlEncode(csrf) + "&current_password=old&new_password=new&confirm_password=new"
	w := accountPwPOST(mux, tokenFromURL(t, url), form)

	if !assert.Equal(t, http.StatusOK, w.Code) {
		return
	}
	assert.Contains(t, w.Body.String(), "failed")
	done, failed, _, _ := c.tokenDone(tokenFromURL(t, url))
	assert.False(t, done, "failed change is never done")
	assert.True(t, failed, "failed change is reported failed")
}

// TestAccountPasswordUpdateRejectsUnauthenticated verifies the tool steers to
// auth_sso when Status fails (not signed in).
func TestAccountPasswordUpdateRejectsUnauthenticated(t *testing.T) {
	svc := &testAccountAuthService{statusErr: context.DeadlineExceeded}
	c, _ := buildAccountPwServer(svc)
	desc := NewAccountPasswordUpdateDescriptor(c, svc, nil, nil)
	res, err := desc.Handler(context.Background(), ToolRequest{Name: "account_password_update", Arguments: map[string]any{}})
	require.NoError(t, err)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, StatusNeedsHuman, sc["status"])
	assert.Equal(t, ReasonSSOApproval, sc["reason"])
	assert.Equal(t, "auth_sso", sc["resume_tool"])
}

// TestAccountPasswordUpdateReturnsHandoff verifies an authenticated account gets
// a needs_human hand-off with the change-page URL.
func TestAccountPasswordUpdateReturnsHandoff(t *testing.T) {
	svc := &testAccountAuthService{}
	c, _ := buildAccountPwServer(svc)
	desc := NewAccountPasswordUpdateDescriptor(c, svc, nil, nil)
	res, err := desc.Handler(context.Background(), ToolRequest{Name: "account_password_update", Arguments: map[string]any{}})
	require.NoError(t, err)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, StatusNeedsHuman, sc["status"])
	assert.Equal(t, ReasonSSOApproval, sc["reason"])
	u, _ := sc["action_url"].(string)
	require.NotEmpty(t, u)
	assert.Contains(t, u, "/account/password/")
}

// TestAccountPasswordUpdateNotConfigured verifies a nil coordinator returns a
// structured not-configured hand-off.
func TestAccountPasswordUpdateNotConfigured(t *testing.T) {
	desc := NewAccountPasswordUpdateDescriptor(nil, nil, nil, nil)
	res, err := desc.Handler(context.Background(), ToolRequest{Name: "account_password_update", Arguments: map[string]any{}})
	require.NoError(t, err)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, StatusNeedsHuman, sc["status"])
	assert.Equal(t, ReasonInteractiveOnly, sc["reason"])
}

// TestAccountPasswordResetSendsAndHandsOff verifies account_password_reset calls
// RequestPasswordReset and returns a needs_human hand-off.
func TestAccountPasswordResetSendsAndHandsOff(t *testing.T) {
	svc := &testAccountAuthService{}
	desc := NewAccountPasswordResetDescriptor(svc, "https://account.pinner.xyz")
	res, err := desc.Handler(context.Background(), ToolRequest{Name: "account_password_reset", Arguments: map[string]any{"email": "user@example.com"}})
	require.NoError(t, err)
	require.Equal(t, []string{"user@example.com"}, svc.resetCalls)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, StatusNeedsHuman, sc["status"])
	assert.Equal(t, "https://account.pinner.xyz", sc["action_url"])
}

// TestAccountPasswordResetMissingEmail verifies an empty email errors.
func TestAccountPasswordResetMissingEmail(t *testing.T) {
	svc := &testAccountAuthService{}
	desc := NewAccountPasswordResetDescriptor(svc, "")
	res, err := desc.Handler(context.Background(), ToolRequest{Name: "account_password_reset", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func urlEncode(s string) string {
	r := strings.NewReplacer(
		"+", "%2B",
		"&", "%26",
	)
	return r.Replace(s)
}
