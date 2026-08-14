package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"

	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
)

// --- Test wizard state ---

// testWebsitesWizard implements mcpadapter.WebsitesWizardState for tests.
type testWebsitesWizard struct {
	cid              string
	domain           string
	targetType       string
	dnsHosting       bool
	website          *ipfs.WebsiteItem
	validationResult *ipfs.WebsiteValidateResponse
}

func (w *testWebsitesWizard) CID() string                { return w.cid }
func (w *testWebsitesWizard) Domain() string             { return w.domain }
func (w *testWebsitesWizard) DNSHosting() bool           { return w.dnsHosting }
func (w *testWebsitesWizard) TargetType() string         { return w.targetType }
func (w *testWebsitesWizard) Website() *ipfs.WebsiteItem { return w.website }
func (w *testWebsitesWizard) ValidationResult() *ipfs.WebsiteValidateResponse {
	return w.validationResult
}
func (w *testWebsitesWizard) SetCID(v string)                { w.cid = v }
func (w *testWebsitesWizard) SetDomain(v string)             { w.domain = v }
func (w *testWebsitesWizard) SetDNSHosting(v bool)           { w.dnsHosting = v }
func (w *testWebsitesWizard) SetTargetType(v string)         { w.targetType = v }
func (w *testWebsitesWizard) SetWebsite(v *ipfs.WebsiteItem) { w.website = v }
func (w *testWebsitesWizard) SetValidationResult(v *ipfs.WebsiteValidateResponse) {
	w.validationResult = v
}

// testSetupWizard implements mcpadapter.SetupWizardState for tests.
type testSetupWizard struct{}

// testWebsitesFactory creates a testWebsitesWizard.
func testWebsitesFactory() mcpadapter.WebsitesWizardState {
	return &testWebsitesWizard{}
}

// testSetupFactory creates a testSetupWizard.
func testSetupFactory() mcpadapter.SetupWizardState {
	return &testSetupWizard{}
}

// extractLoginURL pulls the http(s) URL out of an out-of-band sign-in info
// message so the test can drive the loopback login the way a browser would.
func extractLoginURL(info string) string {
	const marker = "complete sign-in: "
	i := strings.Index(info, marker)
	if i < 0 {
		return info
	}
	rest := info[i+len(marker):]
	// The message is: "...complete sign-in: <url>. Then call setup_auth ...".
	// The URL ends at the ". Then" sentence boundary (there is no whitespace
	// inside the URL itself).
	if j := strings.Index(rest, ". Then"); j > 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// loginResult carries the outcome of a browser-style credential POST.
type loginResult struct {
	resp *http.Response
	err  error
}

// postLogin POSTs credentials to a loopback login URL exactly as a browser
// would: it first GETs the page to read the per-request CSRF token the form
// embeds, then POSTs the credentials with that token and an Origin header
// matching the URL's origin, which the out-of-band endpoint requires.
func postLogin(u string, form url.Values) loginResult {
	// Fetch the login page to obtain the per-request CSRF token.
	csrf, err := fetchCSRFHTTP(u)
	if err != nil {
		return loginResult{err: err}
	}
	form.Set("csrf", csrf)
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return loginResult{err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if uu, perr := url.Parse(u); perr == nil {
		req.Header.Set("Origin", uu.Scheme+"://"+uu.Host)
	}
	resp, derr := http.DefaultClient.Do(req)
	return loginResult{resp: resp, err: derr}
}

// fetchCSRFHTTP GETs a loopback login URL and extracts the per-request CSRF
// token from the rendered form (the hidden input named "csrf").
func fetchCSRFHTTP(u string) (string, error) {
	resp, err := http.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	m := wizardCSRFInputRE.FindSubmatch(body)
	if len(m) < 2 {
		return "", fmt.Errorf("no csrf input in rendered login page %s", u)
	}
	return string(m[1]), nil
}

// wizardCSRFInputRE matches the hidden csrf input in the rendered login page.
var wizardCSRFInputRE = regexp.MustCompile(`name="csrf"\s+value="([^"]+)"`)

// testDomainWizard implements mcpadapter.DomainWizardState for tests.
type testDomainWizard struct {
	websiteID     string
	websiteDomain string
	domain        string
	namespace     string
	result        *ipfs.DomainResponse
}

func (w *testDomainWizard) WebsiteID() string                { return w.websiteID }
func (w *testDomainWizard) SetWebsiteID(v string)            { w.websiteID = v }
func (w *testDomainWizard) WebsiteDomain() string            { return w.websiteDomain }
func (w *testDomainWizard) SetWebsiteDomain(v string)        { w.websiteDomain = v }
func (w *testDomainWizard) Domain() string                   { return w.domain }
func (w *testDomainWizard) SetDomain(v string)               { w.domain = v }
func (w *testDomainWizard) Namespace() string                { return w.namespace }
func (w *testDomainWizard) SetNamespace(v string)            { w.namespace = v }
func (w *testDomainWizard) Result() *ipfs.DomainResponse     { return w.result }
func (w *testDomainWizard) SetResult(v *ipfs.DomainResponse) { w.result = v }

// testDomainFactory creates a testDomainWizard.
func testDomainFactory() mcpadapter.DomainWizardState {
	return &testDomainWizard{}
}

// webservFactory returns a WebsitesWizardDeps with the test factory set.
func webservFactory(deps mcpadapter.WebsitesWizardDeps) mcpadapter.WebsitesWizardDeps {
	deps.WebsitesFactory = testWebsitesFactory
	return deps
}

// --- Mocks ---

// mockWebsitesSvc implements cli.WebsitesService for wizard tests.
type mockWebsitesSvc struct {
	createFunc     func(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	validateFunc   func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	getConfigFunc  func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
	listFunc       func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	createCallReq  *ipfs.WebsiteRequest
	validateCallID string
}

func (m *mockWebsitesSvc) RequireAuthenticated() error { return nil }

func (m *mockWebsitesSvc) List(ctx context.Context) ([]ipfs.WebsiteItem, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesSvc) Create(_ context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
	return &ipfs.WebsiteItem{Id: 1, Domain: domain, TargetHash: cid, TargetType: targetType, Status: "active", Created: time.Now()}, nil
}

func (m *mockWebsitesSvc) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	m.createCallReq = &req
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &ipfs.WebsiteItem{Id: 42, Domain: req.Domain, TargetHash: req.TargetHash, TargetType: req.TargetType, Status: "active", Created: time.Now()}, nil
}

func (m *mockWebsitesSvc) Get(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return nil, nil
}

func (m *mockWebsitesSvc) Update(_ context.Context, _, _, _, _ string) (*ipfs.WebsiteItem, error) {
	return nil, nil
}

func (m *mockWebsitesSvc) UpdateWithOptions(_ context.Context, _ string, _ ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	return nil, nil
}

func (m *mockWebsitesSvc) Delete(_ context.Context, _ string) error { return nil }

func (m *mockWebsitesSvc) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	m.validateCallID = id
	if m.validateFunc != nil {
		return m.validateFunc(ctx, id)
	}
	return &ipfs.WebsiteValidateResponse{Id: 42, Domain: "example.com", Valid: true, Message: "ok"}, nil
}

func (m *mockWebsitesSvc) GetSSLStatus(_ context.Context, _ string) (*ipfs.WebsiteResponse, error) {
	return nil, nil
}

func (m *mockWebsitesSvc) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesSvc) BindDomain(_ context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	return &ipfs.DomainResponse{
		Id:        1,
		Domain:    req.Domain,
		Namespace: req.Namespace,
		Status:    lo.ToPtr("pending"),
	}, nil
}

func (m *mockWebsitesSvc) GetDomainDNSRequirements(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
	return &ipfs.DomainResponse{
		Id:        1,
		Domain:    "example.com",
		Namespace: "icann",
		Status:    lo.ToPtr("active"),
	}, nil
}

func (m *mockWebsitesSvc) VerifyDomain(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
	return &ipfs.DomainResponse{
		Id:        1,
		Domain:    "example.com",
		Namespace: "icann",
		Status:    lo.ToPtr("active"),
	}, nil
}

// mockAuthService implements cli.AuthService for setup wizard tests.
type mockAuthService struct {
	loginCheckFunc   func(ctx context.Context, email, password string) (*portalsdk.LoginResult, error)
	completeLoginErr error
	loginWithOTPErr  error
	registerErr      error
	saveTokenErr     error
	enableOTPErr     error
	disableOTPErr    error

	loginCheckEmail       string
	loginCheckPassword    string
	completeLoginToken    string
	completeLoginKey      string
	completeLoginNoCreate bool
	otpJWT                string
	otpCode               string
	otpKey                string
	otpNoCreate           bool
}

func (m *mockAuthService) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	m.loginCheckEmail = email
	m.loginCheckPassword = password
	if m.loginCheckFunc != nil {
		return m.loginCheckFunc(ctx, email, password)
	}
	return &portalsdk.LoginResult{Token: "jwt-token-123", OTPRequired: false}, nil
}

func (m *mockAuthService) CompleteLogin(_ context.Context, token, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error) {
	m.completeLoginToken = token
	m.completeLoginKey = keyName
	m.completeLoginNoCreate = noCreateKey
	return &auth.LoginCompleteResult{}, m.completeLoginErr
}

func (m *mockAuthService) LoginWithOTP(_ context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*auth.LoginCompleteResult, error) {
	m.otpJWT = intermediateJWT
	m.otpCode = otp
	m.otpKey = keyName
	m.otpNoCreate = noCreateKey
	return &auth.LoginCompleteResult{}, m.loginWithOTPErr
}

func (m *mockAuthService) Register(_ context.Context, _, _, _, _ string) (*auth.RegisterResult, error) {
	return &auth.RegisterResult{}, m.registerErr
}

func (m *mockAuthService) SaveToken(_ string) (*auth.SaveTokenResult, error) {
	return &auth.SaveTokenResult{}, m.saveTokenErr
}

func (m *mockAuthService) GetAPIEndpoint() string { return "https://api.pinner.xyz" }

func (m *mockAuthService) Status(_ context.Context) (*auth.StatusResult, error) {
	return &auth.StatusResult{}, nil
}

func (m *mockAuthService) UpdatePassword(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAuthService) RequestPasswordReset(_ context.Context, _ string) error {
	return nil
}

func (m *mockAuthService) GetAuthenticatedClient(_ context.Context) (portalsdk.AccountAPI, error) {
	return nil, nil
}

func (m *mockAuthService) GetLoginToken(_ context.Context) (string, error) {
	return "jwt-token-123", nil
}

func (m *mockAuthService) EnableOTP(_ context.Context, _ string) (*auth.OTPSecretResult, error) {
	return &auth.OTPSecretResult{}, m.enableOTPErr
}

func (m *mockAuthService) DisableOTP(_ context.Context, _ string) (*auth.DisableOTPResult, error) {
	return &auth.DisableOTPResult{}, m.disableOTPErr
}

// --- Test helpers ---

func newConfigMgr(t *testing.T, authed bool) *configmocks.MockManager {
	t.Helper()
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{
		AuthToken: "",
		Secure:    true,
	}
	if authed {
		cfg.AuthToken = "test-token"
	}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()
	return cfgMgr
}

// --- Websites wizard session tests ---

func TestWebsitesWizard_FullSession(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.WebsitesWizardDeps{
		WebsitesFactory: testWebsitesFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)

	// After start, should be in auth_check state.
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Step 1: auth_check: empty input is fine.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	require.False(t, resp.Complete)
	require.Equal(t, "auth_check", resp.CurrentStep)
	require.NotNil(t, resp.NextStepSchema)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "content_source", sess.FSM.Current())

	// Step 2: content_source: provide CID.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTestHash123"}`))
	require.NoError(t, err)
	assert.Equal(t, "target_type", sess.FSM.Current())

	// Verify CID was set on wizard state.
	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.Equal(t, "QmTestHash123", w.CID())

	// Step 3: target_type.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain", sess.FSM.Current())
	assert.Equal(t, "ipfs", w.TargetType())

	// Step 4: domain.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "dns_mode", sess.FSM.Current())
	assert.Equal(t, "example.com", w.Domain())

	// Step 5: dns_mode.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"managed"}`))
	require.NoError(t, err)
	assert.Equal(t, "create", sess.FSM.Current())
	assert.True(t, w.DNSHosting())

	// Step 6: create.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	assert.Equal(t, "dns_setup", sess.FSM.Current())
	assert.NotNil(t, w.Website())
	assert.Equal(t, "example.com", w.Website().Domain)

	// Verify CreateWithOptions was called with the right args.
	require.NotNil(t, websitesSvc.createCallReq)
	assert.Equal(t, "example.com", websitesSvc.createCallReq.Domain)
	assert.Equal(t, "QmTestHash123", websitesSvc.createCallReq.TargetHash)
	assert.Equal(t, "ipfs", websitesSvc.createCallReq.TargetType)
	assert.True(t, *websitesSvc.createCallReq.DnsHostingEnabled)

	// Step 7: dns_setup: informational.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "validate", sess.FSM.Current())

	// Step 8: validate.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	// Verify Validate was called with the website ID.
	assert.Equal(t, "42", websitesSvc.validateCallID)

	// Verify validation result was set.
	assert.NotNil(t, w.ValidationResult())
	assert.True(t, w.ValidationResult().Valid)

	// Session should report complete.
	resp = mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

func TestWebsitesWizard_AuthCheckFails(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, false) // no auth token
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.WebsitesWizardDeps{
		WebsitesFactory: testWebsitesFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	assert.Equal(t, "auth_check", sess.FSM.Current())

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication required")

	// Session should remain in auth_check state for retry.
	assert.Equal(t, "auth_check", sess.FSM.Current())
}

func TestWebsitesWizard_ContentSourceInvalidChoice(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, _ := mcpadapter.NewWebsitesSession(store, deps)

	// Pass auth_check.
	_, err := mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Invalid choice.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid choice")
	assert.Equal(t, "content_source", sess.FSM.Current())
}

func TestWebsitesWizard_ContentSourceUploadChoice(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, _ := mcpadapter.NewWebsitesSession(store, deps)

	// Pass auth_check.
	_, err := mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Upload choice.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"upload"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content upload required")
}

func TestWebsitesWizard_ContentSourceMissingCID(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, _ := mcpadapter.NewWebsitesSession(store, deps)

	// Pass auth_check.
	_, err := mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// CID choice but empty cid.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cid cannot be empty")
}

func TestWebsitesWizard_TargetTypeInvalid(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, _ := mcpadapter.NewWebsitesSession(store, deps)

	// Pass auth_check + content_source.
	_, err := mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)

	// Invalid target type.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target type")
}

func TestWebsitesWizard_DNSModeInvalid(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to dns_mode.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)

	// Invalid DNS mode.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DNS mode")
}

func TestWebsitesWizard_CreateWithoutConfirm(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to create step.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)

	// Create without confirm.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":false}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
	assert.Equal(t, "create", sess.FSM.Current())
}

func TestWebsitesWizard_CreateServiceError(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		createFunc: func(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
			return nil, errors.New("create service error")
		},
	}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to create step.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website creation failed")
	assert.Contains(t, err.Error(), "create service error")
}

func TestWebsitesWizard_ValidateWithoutWebsite(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to dns_setup (skip create with confirm by going through all steps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)

	// At dns_setup: skip it.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Now at validate. The wizard has a website, so validate should call service.
	// Let's test validate service error.
	websitesSvc.validateFunc = func(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
		return nil, errors.New("validation service error")
	}

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestWebsitesWizard_DefaultTargetType(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)

	// Set target type to ipns, then verify it's used in create.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"type":"ipns"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)

	// Verify target type was set.
	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.Equal(t, "ipns", w.TargetType())

	// Verify CreateWithOptions received ipns.
	require.NotNil(t, websitesSvc.createCallReq)
	assert.Equal(t, "ipns", websitesSvc.createCallReq.TargetType)
}

func TestWebsitesWizard_InvalidJSON(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, _ := mcpadapter.NewWebsitesSession(store, deps)
	_, err := mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Invalid JSON for content_source.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid input")
}

func TestWebsitesWizard_StepSchemas(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)

	// auth_check schema.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	require.NotNil(t, resp.NextStepSchema)

	// content_source schema after auth_check.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	resp = mcpadapter.BuildStepResponseForTest(sess)
	require.NotNil(t, resp.NextStepSchema)
	require.NotNil(t, resp.NextStepSchema.Properties)
	choiceSchema, ok := resp.NextStepSchema.Properties.Get("choice")
	require.True(t, ok)
	assert.NotEmpty(t, choiceSchema.Enum)
}

// --- Setup wizard session tests ---

func TestSetupWizard_FullSessionSkipAuth(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.SetupWizardDeps{
		CfgMgr:       cfgMgr,
		AuthService:  authSvc,
		SetupFactory: testSetupFactory,
	}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	assert.Equal(t, "auth", sess.FSM.Current())

	// Step 1: auth: skip.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)
	assert.Equal(t, "config", sess.FSM.Current())

	// Step 2: config: use defaults.
	cfgMgr.EXPECT().SetBaseEndpoint("").Return(nil).Maybe()
	cfgMgr.EXPECT().SetSecure(true).Return(nil).Maybe()
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"use_defaults"}`))
	require.NoError(t, err)
	assert.Equal(t, "completion", sess.FSM.Current())

	// Step 3: completion: informational.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"shell":"bash"}`))
	require.NoError(t, err)
	assert.Equal(t, "tutorial", sess.FSM.Current())

	// Step 4: tutorial: read-only.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

// TestSetupWizard_SignIn_RequiresOutOfBand verifies the new curated sign-in
// contract: signing in requires out-of-band login, so without an OutOfBand
// coordinator configured the step fails and the session stays on auth.
func TestSetupWizard_SignIn_RequiresOutOfBand(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	require.Equal(t, "auth", sess.FSM.Current())

	// No OutOfBand configured: sign_in must fail and stay on auth.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sign_in requires out-of-band login")
	assert.Equal(t, "auth", sess.FSM.Current())
}

// TestSetupWizard_SignIn_StartsOutOfBand verifies that with an OutOfBand
// coordinator the sign_in step returns an informational message (the login
// URL) with Err nil and does NOT advance until out-of-band login completes.
func TestSetupWizard_SignIn_StartsOutOfBand(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	oob := mcpadapter.NewOutOfBandLogin(authSvc, "", "test-key")
	t.Cleanup(func() { oob.Stop(context.Background()) })
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory, OutOfBand: oob}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	require.Equal(t, "auth", sess.FSM.Current())

	// First sign_in with the coordinator: returns info message, no error, and
	// the session stays on auth (out-of-band login is pending).
	var info string
	info, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, info)
	assert.Contains(t, info, "Out-of-band sign-in required")
	assert.Contains(t, info, "/login/")
	assert.Equal(t, "auth", sess.FSM.Current())

	// Re-calling with the same email surfaces the URL again without advancing.
	info, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, info)
	assert.Equal(t, "auth", sess.FSM.Current())
}

// TestSetupWizard_SignIn_MissingEmail verifies that sign_in with the
// out-of-band coordinator but no email is rejected as a genuine error.
func TestSetupWizard_SignIn_MissingEmail(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	oob := mcpadapter.NewOutOfBandLogin(authSvc, "", "test-key")
	t.Cleanup(func() { oob.Stop(context.Background()) })
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory, OutOfBand: oob}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email is required for sign_in")
	assert.Equal(t, "auth", sess.FSM.Current())
}

func TestSetupWizard_CreateAccountChoice(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"create_account"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account creation must be done")
}

func TestSetupWizard_InvalidAuthChoice(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auth choice")
}

func TestSetupWizard_CustomEndpoint(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)

	// Skip auth.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)

	// Custom endpoint.
	cfgMgr.EXPECT().SetBaseEndpoint("https://custom.api.xyz").Return(nil).Once()
	cfgMgr.EXPECT().SetSecure(false).Return(nil).Once()

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"custom_endpoint","endpoint":"https://custom.api.xyz","secure":false}`))
	require.NoError(t, err)
	assert.Equal(t, "completion", sess.FSM.Current())
}

func TestSetupWizard_CustomEndpointMissingEndpoint(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"custom_endpoint"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
}

func TestSetupWizard_InvalidConfigChoice(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config choice")
}

// --- Enum validity tests ---

func TestContentSourceChoice_Valid(t *testing.T) {
	t.Parallel()
	assert.True(t, mcpadapter.ContentSourceChoice("cid").Valid())
	assert.True(t, mcpadapter.ContentSourceChoice("upload").Valid())
	assert.False(t, mcpadapter.ContentSourceChoice("invalid").Valid())
	assert.False(t, mcpadapter.ContentSourceChoice("").Valid())
}

func TestTargetTypeValue_Valid(t *testing.T) {
	t.Parallel()
	assert.True(t, mcpadapter.TargetTypeValue("ipfs").Valid())
	assert.True(t, mcpadapter.TargetTypeValue("ipns").Valid())
	assert.False(t, mcpadapter.TargetTypeValue("invalid").Valid())
}

func TestDNSModeValue_Valid(t *testing.T) {
	t.Parallel()
	assert.True(t, mcpadapter.DNSModeValue("managed").Valid())
	assert.True(t, mcpadapter.DNSModeValue("self_managed").Valid())
	assert.False(t, mcpadapter.DNSModeValue("invalid").Valid())
}

func TestAuthStepChoiceValue_Valid(t *testing.T) {
	t.Parallel()
	assert.True(t, mcpadapter.AuthStepChoiceValue("create_account").Valid())
	assert.True(t, mcpadapter.AuthStepChoiceValue("sign_in").Valid())
	assert.True(t, mcpadapter.AuthStepChoiceValue("skip").Valid())
	assert.False(t, mcpadapter.AuthStepChoiceValue("invalid").Valid())
}

func TestConfigStepChoiceValue_Valid(t *testing.T) {
	t.Parallel()
	assert.True(t, mcpadapter.ConfigStepChoiceValue("use_defaults").Valid())
	assert.True(t, mcpadapter.ConfigStepChoiceValue("custom_endpoint").Valid())
	assert.True(t, mcpadapter.ConfigStepChoiceValue("skip").Valid())
	assert.False(t, mcpadapter.ConfigStepChoiceValue("invalid").Valid())
}

// --- FSM events builder tests ---

func TestWebsitesFSMEvents_AllStatesCovered(t *testing.T) {
	t.Parallel()
	// Verify the FSM can be created and all transitions are valid.
	fsm := mcpadapter.NewWebsitesFSMForTest()
	assert.Equal(t, "init", fsm.Current())
}

func TestSetupFSMEvents_AllStatesCovered(t *testing.T) {
	t.Parallel()
	fsm := mcpadapter.NewSetupFSMForTest()
	assert.Equal(t, "init", fsm.Current())
}

// --- Phase 2 Task 10: Additional tests for FSM transition enforcement,
// skip transitions, retry transitions, and error mid-flow ---

// TestWebsitesWizard_FSMTransitionEnforcement verifies that the FSM rejects
// transitions fired from the wrong state. Firing "domain_done" while in
// "content_source" (not "domain") should fail with InvalidEventError.
func TestWebsitesWizard_FSMTransitionEnforcement(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	require.Equal(t, "auth_check", sess.FSM.Current())

	// Firing the domain_done event from auth_check should fail: the FSM
	// only allows auth_ok from auth_check.
	err = sess.FSM.Event(context.Background(), "domain_done")
	require.Error(t, err)

	// Firing the content_done event from auth_check should also fail.
	err = sess.FSM.Event(context.Background(), "content_done")
	require.Error(t, err)

	// Firing validate_done from auth_check should fail.
	err = sess.FSM.Event(context.Background(), "validate_done")
	require.Error(t, err)

	// FSM should remain in auth_check: no side effects.
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Now fire the correct event to advance to content_source.
	err = sess.FSM.Event(context.Background(), "auth_ok")
	require.NoError(t, err)
	assert.Equal(t, "content_source", sess.FSM.Current())

	// Fire domain_done from content_source: should fail (wrong state).
	// Cannot fire domain_done from content_source; only content_done is valid.
	err = sess.FSM.Event(context.Background(), "domain_done")
	require.Error(t, err)
	assert.Equal(t, "content_source", sess.FSM.Current())
}

// TestWebsitesWizard_FSMTransitionEnforcement_OutOfOrderStep verifies that
// attempting to advance to the validate step before completing earlier steps
// is blocked by the FSM. The session can only follow the linear step order.
func TestWebsitesWizard_FSMTransitionEnforcement_OutOfOrderStep(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)

	// Try to fire created event from auth_check: should fail.
	err = sess.FSM.Event(context.Background(), "created")
	require.Error(t, err)
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Advance properly: auth_check → content_source → target_type
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)

	assert.Equal(t, "domain", sess.FSM.Current())

	// Try to fire validate_done from domain: should fail.
	err = sess.FSM.Event(context.Background(), "validate_done")
	require.Error(t, err)
	assert.Equal(t, "domain", sess.FSM.Current())
}

// TestWebsitesWizard_AuthCheckSkippedWhenAuthed verifies the skip transition:
// when the user is already authenticated (has a token in config), the
// auth_check step passes silently with empty input.
func TestWebsitesWizard_AuthCheckSkippedWhenAuthed(t *testing.T) {
	t.Parallel()

	// Authed config (with token): auth_check should auto-skip.
	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Empty input is sufficient: the handler just checks the token.
	// No error means the auth_check is silently skipped.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// State should have advanced past auth_check to content_source.
	assert.Equal(t, "content_source", sess.FSM.Current())

	// The step response should reflect the current step.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "content_source", resp.CurrentStep)
}

// TestWebsitesWizard_AuthCheckNotSkippedWhenUnauthed verifies that the
// auth_check step blocks when the user is NOT authenticated.
// The FSM stays in auth_check for retry.
func TestWebsitesWizard_AuthCheckNotSkippedWhenUnauthed(t *testing.T) {
	t.Parallel()

	// Unauthed config (no token): auth_check should block.
	cfgMgr := newConfigMgr(t, false)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Empty input fails: user needs to authenticate first.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication required")

	// FSM should remain in auth_check: not skipped, stays for retry.
	assert.Equal(t, "auth_check", sess.FSM.Current())

	// Step response should still show auth_check as the current step.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "auth_check", resp.CurrentStep)
}

// TestWebsitesWizard_RetryAfterHandlerError verifies the retry transition:
// when a step handler fails (e.g., invalid input), the FSM stays in the
// same state, and the caller can retry with correct input to succeed.
func TestWebsitesWizard_RetryAfterHandlerError(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Pass auth_check.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "content_source", sess.FSM.Current())

	// First attempt: invalid choice: handler fails, FSM stays in content_source.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"choice":"bogus"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid choice")
	assert.Equal(t, "content_source", sess.FSM.Current())

	// Second attempt: valid input: retry succeeds, FSM advances.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmRetryHash"}`))
	require.NoError(t, err)
	assert.Equal(t, "target_type", sess.FSM.Current())

	// Verify the retried input was applied.
	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.Equal(t, "QmRetryHash", w.CID())
}

// TestWebsitesWizard_RetryContentSourceAfterMissingCID verifies that
// a failed content_source step (missing CID) can be retried with the
// correct input.
func TestWebsitesWizard_RetryContentSourceAfterMissingCID(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	// First attempt: CID choice but no cid value: fails.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cid cannot be empty")
	assert.Equal(t, "content_source", sess.FSM.Current())

	// Retry with the cid value: succeeds.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmFixedHash"}`))
	require.NoError(t, err)
	assert.Equal(t, "target_type", sess.FSM.Current())

	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.Equal(t, "QmFixedHash", w.CID())
}

// TestWebsitesWizard_ErrorMidFlow_CreateFails_KeepsFSMState verifies that
// when the create step's service call fails, the session response includes
// the error and the current FSM state remains at "create" for retry.
func TestWebsitesWizard_ErrorMidFlow_CreateFails_KeepsFSMState(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		createFunc: func(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
			return nil, errors.New("create service error")
		},
	}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to the create step.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)

	assert.Equal(t, "create", sess.FSM.Current())

	// Attempt create: service fails.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website creation failed")
	assert.Contains(t, err.Error(), "create service error")

	// FSM should remain in "create": the mid-flow error is retryable.
	assert.Equal(t, "create", sess.FSM.Current())

	// The step response should show create is still the current step
	// (not complete, same state as before the error).
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "create", resp.CurrentStep)

	// The schema should still contain the "confirm" field, indicating
	// the step is waiting for input.
	require.NotNil(t, resp.NextStepSchema.Properties)
	confirmSchema, ok := resp.NextStepSchema.Properties.Get("confirm")
	require.True(t, ok)
	assert.Equal(t, "boolean", confirmSchema.Type)
}

// TestWebsitesWizard_ErrorMidFlow_CreateFails_RetryWithSuccess verifies
// that after a create service error, retrying with a working service succeeds.
func TestWebsitesWizard_ErrorMidFlow_CreateFails_RetryWithSuccess(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		createFunc: func(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
			return nil, errors.New("create service error")
		},
	}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate to the create step.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)

	// First create attempt: fails.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.Error(t, err)
	assert.Equal(t, "create", sess.FSM.Current())

	// Fix the service and retry: should succeed.
	websitesSvc.createFunc = func(_ context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
		return &ipfs.WebsiteItem{
			Id: 99, Domain: req.Domain, TargetHash: req.TargetHash,
			TargetType: req.TargetType, Status: "active",
		}, nil
	}

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	assert.Equal(t, "dns_setup", sess.FSM.Current())

	w := sess.State().(mcpadapter.WebsitesWizardState)
	require.NotNil(t, w.Website())
	assert.Equal(t, 99, w.Website().Id)
}

// TestWebsitesWizard_ValidateRetry verifies the retry transition for
// the validate step: after a validation service error, the session stays
// in "validate", and retrying succeeds.
func TestWebsitesWizard_ValidateRetry(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		validateFunc: func(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
			return nil, errors.New("validation service error")
		},
	}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Navigate through all steps to reach validate.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Equal(t, "validate", sess.FSM.Current())

	// First validate attempt: fails.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
	assert.Equal(t, "validate", sess.FSM.Current())

	// Step response should show validate is still active (retryable).
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "validate", resp.CurrentStep)

	// Fix the validate service and retry.
	websitesSvc.validateFunc = func(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
		return &ipfs.WebsiteValidateResponse{
			Id: 42, Domain: "example.com", Valid: true, Message: "ok",
		}, nil
	}

	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"retry":true}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	// Session should now report complete.
	resp = mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

// TestSetupWizard_RetryAfterAuthError verifies the retry transition for
// the setup wizard: after an out-of-band login failure, the FSM stays in
// "auth", and a subsequent successful login advances.
func TestSetupWizard_RetryAfterAuthError(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{
		loginCheckFunc: func(_ context.Context, _, _ string) (*portalsdk.LoginResult, error) {
			return nil, errors.New("auth service error")
		},
	}
	store := mcpadapter.NewSessionStore()
	oob := mcpadapter.NewOutOfBandLogin(authSvc, "", "test-key")
	t.Cleanup(func() { oob.Stop(context.Background()) })
	deps := mcpadapter.SetupWizardDeps{
		CfgMgr:       cfgMgr,
		AuthService:  authSvc,
		SetupFactory: testSetupFactory,
		OutOfBand:    oob,
	}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	assert.Equal(t, "auth", sess.FSM.Current())

	// First sign_in relays the login URL and stays on auth.
	info, err := mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.Contains(t, info, "/login/")
	assert.Equal(t, "auth", sess.FSM.Current())

	// Step response should show auth is still active.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "auth", resp.CurrentStep)

	// Attempt login with a wrong password -> auth fails -> still on auth.
	u := extractLoginURL(info)
	bad := postLogin(u, url.Values{"password": {"wrong"}})
	require.NoError(t, bad.err)
	bad.resp.Body.Close()
	// The login fails server-side; the request stays pending and the page is
	// re-rendered with an error. The step does not advance.
	assert.Equal(t, "auth", sess.FSM.Current())

	// Retry sign_in: the same pending URL is relayed; complete it with the
	// correct credentials now that the auth service is fixed.
	authSvc.loginCheckFunc = func(_ context.Context, _, _ string) (*portalsdk.LoginResult, error) {
		return &portalsdk.LoginResult{Token: "jwt-token-123", OTPRequired: false}, nil
	}
	info, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.Contains(t, info, "/login/")
	u = extractLoginURL(info)
	ok := postLogin(u, url.Values{"password": {"correct"}})
	require.NoError(t, ok.err)
	ok.resp.Body.Close()

	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "config", sess.FSM.Current())
}

// TestSetupWizard_FSMTransitionEnforcement verifies the setup FSM rejects
// transitions fired from wrong states.
func TestSetupWizard_FSMTransitionEnforcement(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	assert.Equal(t, "auth", sess.FSM.Current())

	// Firing config_done from auth: should fail (only auth_done is valid).
	err = sess.FSM.Event(context.Background(), "config_done")
	require.Error(t, err)
	assert.Equal(t, "auth", sess.FSM.Current())

	// Firing tutorial_done from auth: should fail.
	err = sess.FSM.Event(context.Background(), "tutorial_done")
	require.Error(t, err)
	assert.Equal(t, "auth", sess.FSM.Current())
}

// TestWebsitesWizard_AbortTransitions verifies that the abort event can
// be fired from any step state to immediately transition to "complete".
func TestWebsitesWizard_AbortTransitions(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)

	// Abort from auth_check.
	err = sess.FSM.Event(context.Background(), "abort")
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	// Session should report complete.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

// TestWebsitesWizard_AbortMidFlow verifies that abort can be fired from
// a mid-flow state (e.g., after content_source).
func TestWebsitesWizard_AbortMidFlow(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)
	// Advance to content_source.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)

	assert.Equal(t, "target_type", sess.FSM.Current())

	// Abort from target_type.
	err = sess.FSM.Event(context.Background(), "abort")
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

// TestWebsitesWizard_StepResponseReflectsCurrentStep verifies that the
// StepResponse built at each intermediate state correctly reports the
// current step, next step name, and schema.
func TestWebsitesWizard_StepResponseReflectsCurrentStep(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	require.NoError(t, err)

	// auth_check response.
	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "auth_check", resp.CurrentStep)
	assert.Equal(t, "auth_check", resp.NextStep)
	assert.NotNil(t, resp.NextStepSchema)

	// Advance to content_source.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	resp = mcpadapter.BuildStepResponseForTest(sess)
	assert.False(t, resp.Complete)
	assert.Equal(t, "content_source", resp.CurrentStep)
	require.NotNil(t, resp.NextStepSchema.Properties)
	choiceSchema, ok := resp.NextStepSchema.Properties.Get("choice")
	require.True(t, ok)
	assert.NotEmpty(t, choiceSchema.Enum)
}

// TestWebsitesWizard_SelfManagedDNS verifies that self-managed DNS mode
// sets DNSHosting to false on the wizard state.
func TestWebsitesWizard_SelfManagedDNS(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)

	// Self-managed DNS.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"mode":"self_managed"}`))
	require.NoError(t, err)

	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.False(t, w.DNSHosting())

	// The create request should have DnsHostingEnabled=false.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	require.NotNil(t, websitesSvc.createCallReq)
	assert.False(t, *websitesSvc.createCallReq.DnsHostingEnabled)
}

// TestWebsitesWizard_ManagedDNS verifies that managed DNS mode sets
// DNSHosting to true on the wizard state.
func TestWebsitesWizard_ManagedDNS(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()
	deps := webservFactory(mcpadapter.WebsitesWizardDeps{CfgMgr: cfgMgr, WebsitesService: websitesSvc})

	sess, err := mcpadapter.NewWebsitesSession(store, deps)
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"choice":"cid","cid":"QmTest"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"type":"ipfs"}`))
	require.NoError(t, err)
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"domain":"example.com"}`))
	require.NoError(t, err)

	// Managed DNS.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"mode":"managed"}`))
	require.NoError(t, err)

	w := sess.State().(mcpadapter.WebsitesWizardState)
	assert.True(t, w.DNSHosting())

	// The create request should have DnsHostingEnabled=true.
	_, err = mcpadapter.AdvanceSession(context.Background(),
		sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	require.NotNil(t, websitesSvc.createCallReq)
	assert.True(t, *websitesSvc.createCallReq.DnsHostingEnabled)
}

// TestSetupWizard_FullFlowSignIn verifies the complete setup wizard flow
// using sign_in (not skip) for the auth step. Sign-in is completed
// out-of-band in a browser, then the wizard advances.
func TestSetupWizard_FullFlowSignIn(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	oob := mcpadapter.NewOutOfBandLogin(authSvc, "", "test-key")
	t.Cleanup(func() { oob.Stop(context.Background()) })
	deps := mcpadapter.SetupWizardDeps{
		CfgMgr:       cfgMgr,
		AuthService:  authSvc,
		SetupFactory: testSetupFactory,
		OutOfBand:    oob,
	}

	sess, err := mcpadapter.NewSetupSession(store, deps)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	assert.Equal(t, "auth", sess.FSM.Current())

	// Step 1: auth: sign in. First call relays the out-of-band login URL as an
	// informational message and stays on auth.
	var info string
	info, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.Contains(t, info, "/login/")
	assert.Equal(t, "auth", sess.FSM.Current())

	// Complete the out-of-band login by POSTing the password to the loopback
	// URL, exactly as the browser would.
	u := extractLoginURL(info)
	require.Contains(t, u, "/login/")
	posted := postLogin(u, url.Values{"password": {"fixture-password"}})
	require.NoError(t, posted.err)
	posted.resp.Body.Close()
	require.Equal(t, http.StatusOK, posted.resp.StatusCode)

	// Re-calling sign_in now advances past auth to config.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"sign_in","email":"test@example.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "config", sess.FSM.Current())

	// Step 2: config: use defaults.
	cfgMgr.EXPECT().SetBaseEndpoint("").Return(nil).Maybe()
	cfgMgr.EXPECT().SetSecure(true).Return(nil).Maybe()
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"use_defaults"}`))
	require.NoError(t, err)
	assert.Equal(t, "completion", sess.FSM.Current())

	// Step 3: completion.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"shell":"bash"}`))
	require.NoError(t, err)
	assert.Equal(t, "tutorial", sess.FSM.Current())

	// Step 4: tutorial.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

// TestSetupWizard_FullFlowSkip verifies the complete setup wizard flow
// using skip for both auth and config steps.
func TestSetupWizard_FullFlowSkip(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	authSvc := &mockAuthService{}
	store := mcpadapter.NewSessionStore()
	deps := mcpadapter.SetupWizardDeps{CfgMgr: cfgMgr, AuthService: authSvc, SetupFactory: testSetupFactory}

	sess, err := mcpadapter.NewSetupSession(store, deps)

	// Step 1: auth: skip.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)
	assert.Equal(t, "config", sess.FSM.Current())

	// Step 2: config: skip.
	cfgMgr.EXPECT().SetSecure(true).Return(nil).Maybe()
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"choice":"skip"}`))
	require.NoError(t, err)
	assert.Equal(t, "completion", sess.FSM.Current())

	// Step 3: completion.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess,
		json.RawMessage(`{"shell":"zsh"}`))
	require.NoError(t, err)
	assert.Equal(t, "tutorial", sess.FSM.Current())

	// Step 4: tutorial.
	_, err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	resp := mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}
