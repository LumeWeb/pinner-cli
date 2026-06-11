package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestWebsitesWizard_Run(t *testing.T) {
	t.Run("full wizard with pinner-managed DNS", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash123")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModePinnerManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.Equal(t, 8, result.StepsTotal)
		require.Equal(t, "QmTestHash123", w.CID())
		require.Equal(t, "example.com", w.Domain())
		require.Equal(t, "ipfs", w.TargetType())
		require.True(t, w.DNSHosting())
		require.NotNil(t, w.Website())
		require.Equal(t, "example.com", w.Website().Domain)
	})

	t.Run("full wizard with self-managed DNS", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmAnotherHash")
		mockUI.SetDomainInput("mysite.io")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.Equal(t, "QmAnotherHash", w.CID())
		require.Equal(t, "mysite.io", w.Domain())
		require.False(t, w.DNSHosting())
		require.NotNil(t, w.Website())
	})

	t.Run("full wizard with IPNS target type", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("k51qzi5uqu5djuc6y3wj6zk7hj7mj7o0xqx3r9rp9w8i3xrhil3ci1e1hz8w6m")
		mockUI.SetTargetChoice(TargetTypeIPNS)
		mockUI.SetDomainInput("myipns.site")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.Equal(t, "ipns", w.TargetType())
		require.True(t, mockUI.TargetTypeExecuted)
		require.NotNil(t, w.Website())
	})

	t.Run("default target type is ipfs", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.Equal(t, "ipfs", w.TargetType())
		require.True(t, mockUI.TargetTypeExecuted)
	})

	t.Run("skip auth step when already authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "existing-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.True(t, result.StepsSkipped > 0)
		require.False(t, mockUI.AuthCheckExecuted)
	})

	t.Run("auth step fails when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()

		cfg := &config.Config{
			AuthToken: "",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "authentication required")
	})

	t.Run("content source step fails when user needs to upload", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceExit)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "content upload required")
	})

	t.Run("DNS setup runs for self-managed mode", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.False(t, w.DNSHosting())
		require.GreaterOrEqual(t, result.StepsCompleted, 7)
	})

	t.Run("create website step error propagates", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			createFunc: func(_ context.Context, _, _, _ string) (*ipfs.WebsiteItem, error) {
				return nil, errors.New("create failed")
			},
		}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())

		require.Error(t, err)
		require.Contains(t, err.Error(), "create failed")
	})

	t.Run("validation step executes and sets result", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			validateFunc: func(_ context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
				return &ipfs.WebsiteValidateResponse{
					Id:      1,
					Domain:  "example.com",
					Valid:   true,
					Message: "Website is valid",
				}, nil
			},
		}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.True(t, mockUI.ValidateExecuted)
		require.NotNil(t, w.ValidationResult())
		require.True(t, w.ValidationResult().Valid)
	})

	t.Run("validation step succeeds when validation fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			validateFunc: func(_ context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
				return &ipfs.WebsiteValidateResponse{
					Id:      1,
					Domain:  "example.com",
					Valid:   false,
					Message: "DNS record not found",
				}, nil
			},
		}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.NotNil(t, w.ValidationResult())
		require.False(t, w.ValidationResult().Valid)
	})

	t.Run("validation step does not retry on service error for self-managed", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{
			validateFunc: func(_ context.Context, _ string) (*ipfs.WebsiteValidateResponse, error) {
				return nil, errors.New("validation service error")
			},
		}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		result, err := w.Run(context.Background())

		require.NoError(t, err)
		require.True(t, result.Completed)
		require.Equal(t, 0, result.StepsRetried)
		require.Equal(t, 1, mockUI.ValidateAttempts)
		require.Nil(t, w.ValidationResult())
		require.False(t, w.ValidateRetry())
	})
}

func TestWebsitesWizard_Accessors(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{
		AuthToken: "test-token",
		Secure:    true,
	}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockWebsitesUI()
	output := newTestOutput()
	mockWebsitesSvc := &mockWebsitesServiceForCLI{}

	w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, output)

	require.Equal(t, cfgMgr, w.ConfigManager())
	require.Equal(t, output, w.Output())
	require.Equal(t, mockWebsitesSvc, w.WebsitesService())
}

func TestWebsitesWizard_Setters(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{
		AuthToken: "test-token",
		Secure:    true,
	}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockWebsitesUI()
	mockWebsitesSvc := &mockWebsitesServiceForCLI{}

	w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

	require.Equal(t, "", w.CID())
	require.Equal(t, "", w.Domain())
	require.Equal(t, "", w.TargetType())
	require.False(t, w.DNSHosting())
	require.Nil(t, w.Website())

	w.SetCID("QmTestHash")
	w.SetDomain("example.com")
	w.SetTargetType("ipns")
	w.SetDNSHosting(true)
	w.SetWebsite(&ipfs.WebsiteItem{
		Id:         1,
		Domain:     "example.com",
		TargetHash: "QmTestHash",
	})

	require.Equal(t, "QmTestHash", w.CID())
	require.Equal(t, "example.com", w.Domain())
	require.Equal(t, "ipns", w.TargetType())
	require.True(t, w.DNSHosting())
	require.NotNil(t, w.Website())
	require.Equal(t, "example.com", w.Website().Domain)
}

func TestWebsitesWizard_StepCalls(t *testing.T) {
	t.Run("verify UI call sequence", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		mockUI := NewMockWebsitesUI()
		mockUI.SetContentChoice(ContentChoiceCID)
		mockUI.SetCIDInput("QmTestHash")
		mockUI.SetDomainInput("example.com")
		mockUI.SetDNSChoice(DNSModeSelfManaged)

		cfg := &config.Config{
			AuthToken: "test-token",
			Secure:    true,
		}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}

		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

		_, err := w.Run(context.Background())
		require.NoError(t, err)

		require.True(t, mockUI.ContentSourceExecuted)
		require.True(t, mockUI.TargetTypeExecuted)
		require.True(t, mockUI.DomainExecuted)
		require.True(t, mockUI.DNSModeExecuted)
	})
}

func TestWebsitesWizard_UIError(t *testing.T) {
	tests := []struct {
		name        string
		errMsg      string
	}{
		{
			name:   "welcome error",
			errMsg: "welcome failed",
		},
		{
			name:   "content source error",
			errMsg: "content source failed",
		},
		{
			name:   "target type error",
			errMsg: "target type failed",
		},
		{
			name:   "domain error",
			errMsg: "domain failed",
		},
		{
			name:   "dns mode error",
			errMsg: "dns mode failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockWebsitesUI()
			mockUI.SetReturnError(errors.New(tt.errMsg))

			cfg := &config.Config{
				AuthToken: "test-token",
				Secure:    true,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			mockWebsitesSvc := &mockWebsitesServiceForCLI{}

			w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mockUI, newTestOutput())

			_, err := w.Run(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestMockWebsitesUI(t *testing.T) {
	t.Run("call tracking", func(t *testing.T) {
		mock := NewMockWebsitesUI()

		require.False(t, mock.WasCalled("ExecuteAuthCheckStep"))
		require.Equal(t, 0, mock.CallCount("ExecuteAuthCheckStep"))

		cfgMgr := configmocks.NewMockManager(t)
		cfg := &config.Config{AuthToken: "test-token", Secure: true}
		cfgMgr.EXPECT().Config().Return(cfg).Maybe()

		mockWebsitesSvc := &mockWebsitesServiceForCLI{}
		w := NewWebsitesWizard(mockWebsitesSvc, cfgMgr, mock, newTestOutput())

		_ = mock.ExecuteAuthCheckStep(context.Background(), w)

		require.True(t, mock.WasCalled("ExecuteAuthCheckStep"))
		require.Equal(t, 1, mock.CallCount("ExecuteAuthCheckStep"))
	})

	t.Run("clear calls", func(t *testing.T) {
		mock := NewMockWebsitesUI()

		_ = mock.ShowWelcome()
		_ = mock.ExecuteContentSourceStep(context.Background(), nil)

		require.Len(t, mock.GetCalls(), 2)

		mock.ClearCalls()

		require.Empty(t, mock.GetCalls())
	})

	t.Run("step-specific calls tracked in unified Calls", func(t *testing.T) {
		mock := NewMockWebsitesUI()

		_ = mock.ShowWelcome()
		_ = mock.ExecuteAuthCheckStep(context.Background(), nil)
		_ = mock.ExecuteContentSourceStep(context.Background(), nil)
		_ = mock.ExecuteTargetTypeStep(context.Background(), nil)
		_ = mock.ExecuteDomainStep(context.Background(), nil)
		_ = mock.ExecuteDNSModeStep(context.Background(), nil)
		_ = mock.ExecuteCreateWebsiteStep(context.Background(), nil)
		_ = mock.ExecuteValidateStep(context.Background(), nil)

		calls := mock.GetCalls()
		require.Equal(t, "ShowWelcome", calls[0])
		require.Equal(t, "ExecuteAuthCheckStep", calls[1])
		require.Equal(t, "ExecuteContentSourceStep", calls[2])
		require.Equal(t, "ExecuteTargetTypeStep", calls[3])
		require.Equal(t, "ExecuteDomainStep", calls[4])
		require.Equal(t, "ExecuteDNSModeStep", calls[5])
		require.Equal(t, "ExecuteCreateWebsiteStep", calls[6])
		require.Equal(t, "ExecuteValidateStep", calls[7])
	})
}
