package wizard_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"

	wizard "go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// --- Domain wizard tests ---

func TestDomainWizard_FullSession(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	assert.Equal(t, "domain_auth_check", sess.FSM.Current())

	resp := wizard.BuildStepResponseForTest(sess)
	require.False(t, resp.Complete)
	require.Equal(t, "domain_auth_check", resp.CurrentStep)
	require.NotNil(t, resp.NextStepSchema)

	// Step 1: auth_check: empty input is fine.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_website", sess.FSM.Current())

	// Step 2: website.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_name", sess.FSM.Current())
	w := sess.State().(wizard.DomainWizardState)
	assert.Equal(t, "7", w.WebsiteID())
	assert.Equal(t, "example.com", w.WebsiteDomain())

	// Step 3: domain name.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
	assert.Equal(t, "mydomain.com", w.Domain())

	// Step 4: namespace.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_bind", sess.FSM.Current())
	assert.Equal(t, "icann", w.Namespace())

	// Step 5: bind.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_delegation_setup", sess.FSM.Current())
	assert.NotNil(t, w.Result())
	assert.Equal(t, "mydomain.com", w.Result().Domain)

	// Step 6: delegation setup: informational.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_verify", sess.FSM.Current())

	// Step 7: verify.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_complete", sess.FSM.Current())

	// Session should report complete.
	resp = wizard.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

func TestDomainWizard_AuthCheckFails(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, false) // no auth token
	websitesSvc := &mockWebsitesSvc{}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication required")
	// Session stays in auth_check so it can be retried after auth.
	assert.Equal(t, "domain_auth_check", sess.FSM.Current())
}

func TestDomainWizard_WebsiteNotFound(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"999"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, "domain_website", sess.FSM.Current())
}

func TestDomainWizard_InvalidNamespace(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`))
	require.NoError(t, err)

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
}

func TestDomainWizard_ClaimPlatformSubdomain(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)

	// auth -> website
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)

	// domain_name: claim a platform subdomain with an explicit label. The
	// platform root is supplied as the domain; when platform_domain is omitted
	// it defaults to the supplied domain.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"ipfs.pin.xyz","label":"myblog"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
	w := sess.State().(wizard.DomainWizardState)
	assert.Equal(t, "ipfs.pin.xyz", w.Domain())
	assert.Equal(t, "myblog", w.Label())
	assert.True(t, w.PlatformDomain() != "", "platform domain should default to the supplied root")
	assert.Equal(t, "ipfs.pin.xyz", w.PlatformDomain())

	// namespace
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`))
	require.NoError(t, err)

	// bind
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_delegation_setup", sess.FSM.Current())

	// The claim fields must be passed through on the DomainRequest.
	require.NotNil(t, websitesSvc.bindCallReq)
	req := websitesSvc.bindCallReq
	assert.Equal(t, "myblog", lo.FromPtr(req.Label))
	assert.Nil(t, req.Generate, "generate should be nil for an explicit label claim")
	require.NotNil(t, req.PlatformDomain)
	assert.Equal(t, "ipfs.pin.xyz", *req.PlatformDomain)
	assert.Equal(t, "ipfs.pin.xyz", req.Domain)
}

func TestDomainWizard_ClaimPlatformSubdomainGenerate(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)

	// Auto-generate a subdomain label under the platform root.
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"ipfs.pin.xyz","generate":true,"platform_namespace":"default"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
	w := sess.State().(wizard.DomainWizardState)
	assert.True(t, w.Generate())
	assert.Equal(t, "default", w.PlatformNamespace())

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)

	require.NotNil(t, websitesSvc.bindCallReq)
	req := websitesSvc.bindCallReq
	require.NotNil(t, req.Generate)
	assert.True(t, *req.Generate)
	require.NotNil(t, req.PlatformDomain)
	assert.Equal(t, "ipfs.pin.xyz", *req.PlatformDomain)
	require.NotNil(t, req.PlatformNamespace)
	assert.Equal(t, "default", *req.PlatformNamespace)
	assert.Nil(t, req.Label, "no explicit label for a generate claim")
}

func TestDomainWizard_BindWithoutConfirm(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := session.NewSessionStore()

	deps := wizard.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := wizard.NewDomainSession(store, deps)
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`))
	require.NoError(t, err)
	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`))
	require.NoError(t, err)

	_, err = session.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":false}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
	assert.Equal(t, "domain_bind", sess.FSM.Current())
}
