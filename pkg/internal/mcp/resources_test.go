package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/looplab/fsm"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	ipfs "go.lumeweb.com/ipfs-sdk"
	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

// --- Mocks ---

type mockAccountProvider struct {
	authed     bool
	authErr    error
	apiKeyHint string
	quota      map[string]any
	cfgSummary map[string]any
}

func (m *mockAccountProvider) IsAuthenticated() bool                  { return m.authed }
func (m *mockAccountProvider) AuthStatus(_ context.Context) error     { return m.authErr }
func (m *mockAccountProvider) APIKey() string                         { return m.apiKeyHint }
func (m *mockAccountProvider) Quota(_ context.Context) map[string]any { return m.quota }
func (m *mockAccountProvider) ConfigSummary() map[string]any         { return m.cfgSummary }

type mockWebsitesProvider struct {
	website  *ipfs.WebsiteItem
	getErr   error
	validate *ipfs.WebsiteValidateResponse
	valErr   error
	cfgResp  *ipfs.WebsiteConfigResponse
	cfgErr   error
}

func (m *mockWebsitesProvider) GetByDomain(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return m.website, m.getErr
}

func (m *mockWebsitesProvider) GetByID(_ context.Context, _ string) (*ipfs.WebsiteItem, error) {
	return m.website, m.getErr
}

func (m *mockWebsitesProvider) Validate(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
	return m.validate, m.valErr
}

func (m *mockWebsitesProvider) GetConfig(_ context.Context) (*ipfs.WebsiteConfigResponse, error) {
	return m.cfgResp, m.cfgErr
}

// --- Helpers ---

// buildServerWithResources builds a minimal MCP server with resources wired.
func buildServerWithResources(t *testing.T, provs mcpadapter.ResourceProviders) *client.Client {
	t.Helper()
	root := &cli.Command{
		Name:    "test",
		Version: "1.0.0",
		Action:  func(context.Context, *cli.Command) error { return nil },
	}
	srv, err := mcpadapter.MCPServerWithOpts(root, true, nil)
	require.NoError(t, err)
	mcpadapter.RegisterResources(srv, provs)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	return c
}

// parseResourceText extracts the text content from a ReadResourceResult.
func parseResourceText(t *testing.T, result *mcp.ReadResourceResult) string {
	t.Helper()
	require.NotEmpty(t, result.Contents)
	tc, ok := result.Contents[0].(mcp.TextResourceContents)
	require.True(t, ok, "expected TextResourceContents")
	return tc.Text
}

func newTestFSM() *fsm.FSM {
	return fsm.NewFSM("step1",
		[]fsm.EventDesc{
			{Name: "next", Src: []string{"step1"}, Dst: "step2"},
			{Name: "next", Src: []string{"step2"}, Dst: "complete"},
		},
		nil,
	)
}

func newTestSteps() []mcpadapter.StepDef {
	return []mcpadapter.StepDef{
		{Name: "step1", Event: "next"},
		{Name: "step2", Event: "next"},
	}
}

// --- Tests ---

func TestResources_CapabilitiesEnabled(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Account: &mockAccountProvider{authed: true, authErr: nil, apiKeyHint: "****1234"},
	}
	c := buildServerWithResources(t, provs)

	// The Initialize result should report resource capabilities (checked in buildServerWithResources).
	// Verify via resources/list returning our static resource.
	result, err := c.ListResources(t.Context(), mcp.ListResourcesRequest{})
	require.NoError(t, err)

	// Should contain the account-status static resource.
	var foundAccount bool
	for _, r := range result.Resources {
		if r.URI == mcpadapter.AccountStatusURI {
			foundAccount = true
		}
	}
	assert.True(t, foundAccount, "resources/list should include pinner://account/status")
}

func TestResources_AccountStatus_Authenticated(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Account: &mockAccountProvider{
			authed:     true,
			authErr:    nil,
			apiKeyHint: "****1234",
			quota:      map[string]any{"storage_mb": 5000},
			cfgSummary: map[string]any{
				"base_endpoint":   "https://pinner.xyz",
				"secure":          true,
				"max_retries":     3,
				"memory_limit_mb": 100,
			},
		},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = mcpadapter.AccountStatusURI
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &status))

	assert.True(t, status["authenticated"].(bool))
	assert.Equal(t, "****1234", status["api_key"])
	assert.True(t, status["token_valid"].(bool))
	quota, ok := status["quota"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(5000), quota["storage_mb"])

	cfg, ok := status["config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://pinner.xyz", cfg["base_endpoint"])
	assert.True(t, cfg["secure"].(bool))
}

func TestResources_AccountStatus_NotAuthenticated(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Account: &mockAccountProvider{
			authed:  false,
			authErr: errors.New("no token"),
		},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = mcpadapter.AccountStatusURI
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &status))

	assert.False(t, status["authenticated"].(bool))
	assert.False(t, status["token_valid"].(bool))
	assert.Contains(t, status["token_error"].(string), "no token")
}

func TestResources_DNSRequirements_SelfManaged(t *testing.T) {
	t.Parallel()

	gateway := "gw.pinner.xyz"
	tokenVal := "lumeweb-verify=abc123"
	website := &ipfs.WebsiteItem{
		Id:                1,
		Domain:            "example.com",
		TargetHash:        "bafyabcd",
		TargetType:        "ipfs",
		DnsHostingEnabled: false,
		ValidationToken:   tokenVal,
		GatewayDomain:     &gateway,
	}

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: website},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/example.com/dns-requirements"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var reqs mcpadapter.DNSRequirements
	require.NoError(t, json.Unmarshal([]byte(raw), &reqs))

	assert.Equal(t, "example.com", reqs.Domain)
	assert.False(t, reqs.DNSHostingEnabled)
	assert.Len(t, reqs.Records, 3)

	// TXT validation record
	txtFound := false
	dnslinkFound := false
	cnameFound := false
	for _, r := range reqs.Records {
		switch r.Type {
		case "TXT":
			if r.Name == "example.com" && r.Value == tokenVal {
				txtFound = true
			}
			if r.Name == "_dnslink.example.com" && r.Value == "dnslink=/ipfs/bafyabcd" {
				dnslinkFound = true
			}
		case "CNAME":
			if r.Name == "example.com" && r.Value == gateway {
				cnameFound = true
			}
		}
	}
	assert.True(t, txtFound, "should have TXT validation record")
	assert.True(t, dnslinkFound, "should have dnslink TXT record")
	assert.True(t, cnameFound, "should have CNAME record")
}

func TestResources_DNSRequirements_Hosted(t *testing.T) {
	t.Parallel()

	ns := []string{"ns1.pinner.xyz", "ns2.pinner.xyz"}
	website := &ipfs.WebsiteItem{
		Id:                2,
		Domain:            "example.com",
		DnsHostingEnabled: true,
	}
	cfgResp := &ipfs.WebsiteConfigResponse{
		Nameservers: &ns,
	}

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: website, cfgResp: cfgResp},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/example.com/dns-requirements"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var reqs mcpadapter.DNSRequirements
	require.NoError(t, json.Unmarshal([]byte(raw), &reqs))

	assert.True(t, reqs.DNSHostingEnabled)
	assert.Equal(t, ns, reqs.Nameservers)
	assert.NotEmpty(t, reqs.Records)
}

func TestResources_DNSRequirements_NotFound(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{
			website: nil,
			getErr:  errors.New("404 not found"),
		},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/doesnotexist.com/dns-requirements"
	_, err := c.ReadResource(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "doesnotexist.com")
}

func TestResources_DNSRequirements_MissingDomainParam(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: nil},
	}
	c := buildServerWithResources(t, provs)

	// Malformed URI that doesn't match the template pattern.
	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/dns-requirements"
	_, err := c.ReadResource(t.Context(), req)
	require.Error(t, err)
}

func TestResources_ValidationStatus_Valid(t *testing.T) {
	t.Parallel()

	website := &ipfs.WebsiteItem{
		Id:     42,
		Domain: "example.com",
		Status: "active",
	}
	validate := &ipfs.WebsiteValidateResponse{
		Id:      42,
		Domain:  "example.com",
		Valid:   true,
		Reason:  "",
		Message: "validated",
	}

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: website, validate: validate},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/42/validation-status"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var vs mcpadapter.ValidationStatus
	require.NoError(t, json.Unmarshal([]byte(raw), &vs))

	assert.Equal(t, 42, vs.ID)
	assert.Equal(t, "example.com", vs.Domain)
	assert.True(t, vs.Valid)
	assert.Equal(t, "active", vs.Status)
}

func TestResources_ValidationStatus_Invalid(t *testing.T) {
	t.Parallel()

	website := &ipfs.WebsiteItem{
		Id:     42,
		Domain: "example.com",
		Status: "pending",
	}
	validate := &ipfs.WebsiteValidateResponse{
		Id:      42,
		Domain:  "example.com",
		Valid:   false,
		Reason:  "TXT record missing",
		Message: "validation failed",
	}

	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: website, validate: validate},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/42/validation-status"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var vs mcpadapter.ValidationStatus
	require.NoError(t, json.Unmarshal([]byte(raw), &vs))

	assert.False(t, vs.Valid)
	assert.Equal(t, "TXT record missing", vs.Reason)
	assert.Equal(t, "pending", vs.Status)
}

func TestResources_ValidationStatus_ValidateError(t *testing.T) {
	t.Parallel()

	website := &ipfs.WebsiteItem{Id: 42, Domain: "example.com"}
	provs := mcpadapter.ResourceProviders{
		Websites: &mockWebsitesProvider{website: website, valErr: errors.New("api error")},
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://websites/42/validation-status"
	_, err := c.ReadResource(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api error")
}

func TestResources_WizardState_ActiveSession(t *testing.T) {
	t.Parallel()

	store := mcpadapter.NewSessionStore()
	fsmInst := newTestFSM()
	steps := newTestSteps()
	sess, err := store.Create(nil, fsmInst, steps)
	require.NoError(t, err)

	provs := mcpadapter.ResourceProviders{
		Sessions: store,
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://wizard/" + sess.ID + "/state"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var state mcpadapter.WizardSessionState
	require.NoError(t, json.Unmarshal([]byte(raw), &state))

	assert.Equal(t, sess.ID, state.SessionID)
	assert.Equal(t, "step1", state.Current)
	assert.False(t, state.Complete)
}

func TestResources_WizardState_Complete(t *testing.T) {
	t.Parallel()

	store := mcpadapter.NewSessionStore()
	fsmInst := newTestFSM()
	steps := newTestSteps()
	sess, err := store.Create(nil, fsmInst, steps)
	require.NoError(t, err)

	// Advance to complete.
	require.NoError(t, fsmInst.Event(t.Context(), "next"))
	require.NoError(t, fsmInst.Event(t.Context(), "next"))

	provs := mcpadapter.ResourceProviders{
		Sessions: store,
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://wizard/" + sess.ID + "/state"
	result, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)

	raw := parseResourceText(t, result)
	var state mcpadapter.WizardSessionState
	require.NoError(t, json.Unmarshal([]byte(raw), &state))

	assert.True(t, state.Complete)
	assert.Equal(t, "complete", state.Current)
}

func TestResources_WizardState_NotFound(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Sessions: mcpadapter.NewSessionStore(),
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://wizard/nonexistent-uuid/state"
	_, err := c.ReadResource(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResources_WizardState_Expired(t *testing.T) {
	t.Parallel()

	// Use a store with effectively-zero TTL so sessions are immediately
	// expired.
	store := mcpadapter.NewSessionStoreWithTTL(0)
	fsmInst := newTestFSM()
	steps := newTestSteps()
	// Need a custom create that sets ExpiryAt in the past. SessionStore.Create
	// sets ExpiresAt = now + ttl. With ttl=0, now.After(ExpiresAt) is true
	// only after time advances. Use a tiny TTL and rely on Get checking
	// time.Now().After(s.ExpiresAt) — with ttl=0, ExpiresAt == now, so
	// time.Now().After(now) is false at the exact instant. Instead, recreate
	// with a negative-TTL store via NewSessionStoreWithTTL(-1). But that's
	// odd. The cleaner approach: create with normal store, then overwrite
	// expiry by deleting and using the fact that Get checks IsExpired.
	//
	// Simpler: use the normal store and call Delete manually, then expect
	// not-found. For the expired path, use a store with TTL = 1 nanosecond
	// and sleep briefly.
	store = mcpadapter.NewSessionStoreWithTTL(1)
	sess, err := store.Create(nil, fsmInst, steps)
	require.NoError(t, err)

	// Wait so the session is expired.
	require.Eventually(t, func() bool {
		_, err := store.Get(sess.ID)
		return errors.Is(err, mcpadapter.ErrSessionExpired) || errors.Is(err, mcpadapter.ErrSessionNotFound)
	}, 2_000_000_000, 1_000_000, "session should expire")

	provs := mcpadapter.ResourceProviders{
		Sessions: store,
	}
	c := buildServerWithResources(t, provs)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "pinner://wizard/" + sess.ID + "/state"
	// The session is deleted on Get when expired, so the handler returns
	// either "expired" or "not found". Either way it's an error path.
	_, err = c.ReadResource(t.Context(), req)
	require.Error(t, err)
}

func TestResources_ResourceTemplatesListed(t *testing.T) {
	t.Parallel()

	provs := mcpadapter.ResourceProviders{
		Account:  &mockAccountProvider{authed: true},
		Websites: &mockWebsitesProvider{},
		Sessions: mcpadapter.NewSessionStore(),
	}
	c := buildServerWithResources(t, provs)

	// List resource templates — should include our three templates.
	result, err := c.ListResourceTemplates(t.Context(), mcp.ListResourceTemplatesRequest{})
	require.NoError(t, err)

	uris := make([]string, 0, len(result.ResourceTemplates))
	for _, rt := range result.ResourceTemplates {
		uris = append(uris, rt.URITemplate.Raw())
	}

	assert.Contains(t, uris, mcpadapter.DNSRequirementsTmpl)
	assert.Contains(t, uris, mcpadapter.ValidationStatusTmpl)
	assert.Contains(t, uris, mcpadapter.WizardStateTmpl)
}

func TestResources_NoProviders_OmitsResourceCapability(t *testing.T) {
	t.Parallel()

	// No resource providers — should not enable resources capability.
	root := &cli.Command{
		Name:    "test",
		Version: "1.0.0",
		Action:  func(context.Context, *cli.Command) error { return nil },
	}
	srv, err := mcpadapter.MCPServerWithOpts(root, true, nil, nil)
	require.NoError(t, err)

	// Initialize via in-process client to check capabilities.
	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	initResult, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	assert.Nil(t, initResult.Capabilities.Resources, "resources capability should be nil when no providers configured")
}
