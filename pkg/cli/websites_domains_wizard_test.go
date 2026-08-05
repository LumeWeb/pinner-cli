package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestDomainAddWizard_Run(t *testing.T) {
	t.Run("full wizard with icann namespace", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("staging.example.com")
		mockUI.SetNamespaceChoice(DomainNamespaceICANNChoice)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		result := &ipfs.DomainResponse{
			Id:        42,
			Domain:    "staging.example.com",
			Namespace: "icann",
			Status:    &status,
		}
		delegStatus := "active"
		delegResult := &ipfs.DomainResponse{
			Id:        42,
			Domain:    "staging.example.com",
			Namespace: "icann",
			Status:    &delegStatus,
		}
		verifyStatus := "active"
		verifyResult := &ipfs.DomainResponse{
			Id:        42,
			Domain:    "staging.example.com",
			Namespace: "icann",
			Status:    &verifyStatus,
		}

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return result, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return delegResult, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return verifyResult, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.Equal(t, 7, res.StepsTotal)
		require.Equal(t, "1", w.WebsiteID())
		require.Equal(t, "example.com", w.WebsiteDomain())
		require.Equal(t, "staging.example.com", w.Domain())
		require.Equal(t, "icann", w.Namespace())
		require.NotNil(t, w.Result())
		require.Equal(t, 42, w.Result().Id)
	})

	t.Run("full wizard with hns namespace", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("mydomain")
		mockUI.SetNamespaceChoice(DomainNamespaceHNSChoice)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{
					Id:        7,
					Domain:    req.Domain,
					Namespace: req.Namespace,
					Status:    &status,
				}, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{
					Id:        7,
					Domain:    "mydomain",
					Namespace: "hns",
					Status:    &status,
				}, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{
					Id:        7,
					Domain:    "mydomain",
					Namespace: "hns",
					Status:    &status,
				}, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.Equal(t, "hns", w.Namespace())
		require.Equal(t, "mydomain", w.Domain())
	})

	t.Run("skip auth step when already authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("staging.example.com")
		mockUI.SetNamespaceChoice(DomainNamespaceICANNChoice)

		cfg := &config.Config{
			AuthToken: "existing-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, _ ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "x.com", Namespace: "icann", Status: &status}, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "x.com", Namespace: "icann", Status: &status}, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "x.com", Namespace: "icann", Status: &status}, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.True(t, res.StepsSkipped > 0)
		require.False(t, mockUI.AuthCheckExecuted)
	})

	t.Run("auth step fails when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()

		cfg := &config.Config{AuthToken: "", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "authentication required")
	})

	t.Run("website selection step fails when no websites", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()

		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			listFunc: func(_ context.Context) ([]ipfs.WebsiteItem, error) {
				return []ipfs.WebsiteItem{}, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "no websites found")
	})

	t.Run("bind domain step error propagates", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("staging.example.com")
		mockUI.SetNamespaceChoice(DomainNamespaceICANNChoice)

		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, _ ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return nil, errors.New("bind failed")
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "bind failed")
	})

	t.Run("delegation setup handles nil delegation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("mydomain")
		mockUI.SetNamespaceChoice(DomainNamespaceHNSChoice)

		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, _ ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "mydomain", Namespace: "hns", Status: &status}, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return nil, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "mydomain", Namespace: "hns", Status: &status}, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.True(t, mockUI.DelegationSetupExecuted)
	})

	t.Run("verify step executes and reports valid status", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("staging.example.com")
		mockUI.SetNamespaceChoice(DomainNamespaceICANNChoice)

		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, _ ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "s.com", Namespace: "icann", Status: &status}, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "s.com", Namespace: "icann", Status: &status}, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "s.com", Namespace: "icann", Status: &status}, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.True(t, mockUI.VerifyExecuted)
		require.Equal(t, 1, mockUI.VerifyAttempts)
	})

	t.Run("verify step tolerates nil result from VerifyDomain", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockDomainsUI()
		mockUI.SetWebsiteSelectIndex(0)
		mockUI.SetDomainInput("staging.example.com")
		mockUI.SetNamespaceChoice(DomainNamespaceICANNChoice)

		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		status := "active"
		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			BindDomainFn: func(_ context.Context, _ string, _ ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "s.com", Namespace: "icann", Status: &status}, nil
			},
			GetDomainDNSRequirementsFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return &ipfs.DomainResponse{Id: 1, Domain: "s.com", Namespace: "icann", Status: &status}, nil
			},
			VerifyDomainFn: func(_ context.Context, _, _ string) (*ipfs.DomainResponse, error) {
				return nil, nil
			},
		}

		w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		// Must complete without panicking on the nil verify result, and the
		// previously bound domain must not be clobbered (executeVerify must
		// only SetResult when verification returns non-nil).
		res, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, res.Completed)
		require.True(t, mockUI.VerifyExecuted)
		require.NotNil(t, w.Result())
		require.Equal(t, "s.com", w.Result().Domain)
	})

	t.Run("step-specific UI calls tracked", func(t *testing.T) {
		mock := NewMockDomainsUI()

		_ = mock.ShowWelcome()
		_ = mock.ExecuteAuthCheckStep(context.Background(), nil)
		_ = mock.ExecuteWebsiteStep(context.Background(), nil)
		_ = mock.ExecuteDomainStep(context.Background(), nil)
		_ = mock.ExecuteNamespaceStep(context.Background(), nil)
		_ = mock.ExecuteBindDomainStep(context.Background(), nil)
		_ = mock.ExecuteDelegationSetupStep(context.Background(), nil)
		_ = mock.ExecuteVerifyStep(context.Background(), nil)

		calls := mock.GetCalls()
		require.Equal(t, "ShowWelcome", calls[0])
		require.Equal(t, "ExecuteAuthCheckStep", calls[1])
		require.Equal(t, "ExecuteWebsiteStep", calls[2])
		require.Equal(t, "ExecuteDomainStep", calls[3])
		require.Equal(t, "ExecuteNamespaceStep", calls[4])
		require.Equal(t, "ExecuteBindDomainStep", calls[5])
		require.Equal(t, "ExecuteDelegationSetupStep", calls[6])
		require.Equal(t, "ExecuteVerifyStep", calls[7])
	})
}

func TestDomainAddWizard_Accessors(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{AuthToken: "test-token", Secure: true}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockDomainsUI()
	output := newTestOutput()
	mockWebsitesSvc := &mockWebsitesServiceForCLI{}

	w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, output)

	require.Equal(t, cfgMgr, w.ConfigManager())
	require.Equal(t, output, w.Output())
	require.Equal(t, mockWebsitesSvc, w.WebsitesService())

	require.Equal(t, "", w.WebsiteID())
	require.Equal(t, "", w.WebsiteDomain())
	require.Equal(t, "", w.Domain())
	require.Equal(t, "", w.Namespace())
	require.Nil(t, w.Result())
	require.False(t, w.VerifyRetry())
	require.Empty(t, w.Websites())

	w.SetWebsiteID("5")
	w.SetWebsiteDomain("example.com")
	w.SetDomain("staging.example.com")
	w.SetNamespace("icann")
	w.SetResult(&ipfs.DomainResponse{Id: 9, Domain: "staging.example.com", Namespace: "icann"})
	w.SetVerifyRetry(true)

	require.Equal(t, "5", w.WebsiteID())
	require.Equal(t, "example.com", w.WebsiteDomain())
	require.Equal(t, "staging.example.com", w.Domain())
	require.Equal(t, "icann", w.Namespace())
	require.NotNil(t, w.Result())
	require.Equal(t, 9, w.Result().Id)
	require.True(t, w.VerifyRetry())
}

func TestDomainAddWizard_UIError(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
	}{
		{name: "website error", errMsg: "website failed"},
		{name: "domain error", errMsg: "domain failed"},
		{name: "namespace error", errMsg: "namespace failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockDomainsUI()
			mockUI.SetReturnError(errors.New(tt.errMsg))

			cfg := &config.Config{AuthToken: "test-token", Secure: true}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			mockWebsitesSvc := &mockWebsitesServiceForCLI{}

			w := NewDomainAddWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

			_, err := w.Run(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}
