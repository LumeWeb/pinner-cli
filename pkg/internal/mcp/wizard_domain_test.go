package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"

	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
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
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewDomainSession(store, deps)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	assert.Equal(t, "domain_auth_check", sess.FSM.Current())

	resp := mcpadapter.BuildStepResponseForTest(sess)
	require.False(t, resp.Complete)
	require.Equal(t, "domain_auth_check", resp.CurrentStep)
	require.NotNil(t, resp.NextStepSchema)

	// Step 1: auth_check — empty input is fine.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_website", sess.FSM.Current())

	// Step 2: website.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_name", sess.FSM.Current())
	w := sess.State().(mcpadapter.DomainWizardState)
	assert.Equal(t, "7", w.WebsiteID())
	assert.Equal(t, "example.com", w.WebsiteDomain())

	// Step 3: domain name.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
	assert.Equal(t, "mydomain.com", w.Domain())

	// Step 4: namespace.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_bind", sess.FSM.Current())
	assert.Equal(t, "icann", w.Namespace())

	// Step 5: bind.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":true}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_delegation_setup", sess.FSM.Current())
	assert.NotNil(t, w.Result())
	assert.Equal(t, "mydomain.com", w.Result().Domain)

	// Step 6: delegation setup — informational.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_verify", sess.FSM.Current())

	// Step 7: verify.
	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "domain_complete", sess.FSM.Current())

	// Session should report complete.
	resp = mcpadapter.BuildStepResponseForTest(sess)
	assert.True(t, resp.Complete)
}

func TestDomainWizard_AuthCheckFails(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, false) // no auth token
	websitesSvc := &mockWebsitesSvc{}
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewDomainSession(store, deps)
	require.NoError(t, err)

	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
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
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewDomainSession(store, deps)
	require.NoError(t, err)
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`)))

	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"999"}`))
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
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewDomainSession(store, deps)
	require.NoError(t, err)
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`)))
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`)))
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`)))

	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"invalid"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
	assert.Equal(t, "domain_namespace", sess.FSM.Current())
}

func TestDomainWizard_BindWithoutConfirm(t *testing.T) {
	t.Parallel()

	cfgMgr := newConfigMgr(t, true)
	websitesSvc := &mockWebsitesSvc{
		listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com", Status: "active", Created: time.Now()}}, nil
		},
	}
	store := mcpadapter.NewSessionStore()

	deps := mcpadapter.DomainWizardDeps{
		DomainFactory:   testDomainFactory,
		CfgMgr:          cfgMgr,
		WebsitesService: websitesSvc,
	}

	sess, err := mcpadapter.NewDomainSession(store, deps)
	require.NoError(t, err)
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{}`)))
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"website_id":"7"}`)))
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"domain":"mydomain.com"}`)))
	require.NoError(t, mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"namespace":"icann"}`)))

	err = mcpadapter.AdvanceSession(context.Background(), sess, json.RawMessage(`{"confirm":false}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
	assert.Equal(t, "domain_bind", sess.FSM.Current())
}
