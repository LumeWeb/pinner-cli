package wizard

import (
	"testing"

	"github.com/stretchr/testify/require"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

// TestApplySetupConfigDefaults guards the use_defaults branch: it resets the
// endpoint and restores HTTPS enforcement on the persistent CLI config.
func TestApplySetupConfigDefaults(t *testing.T) {
	cfg := configmocks.NewMockManager(t)
	cfg.EXPECT().SetBaseEndpoint("").Return(nil).Once()
	cfg.EXPECT().SetSecure(true).Return(nil).Once()

	require.NoError(t, applySetupConfig(cfg, SetupConfigInput{Choice: ConfigChoiceDefaults}))
}

// TestApplySetupConfigSkip guards the skip branch: existing configuration is
// left untouched (no config writes).
func TestApplySetupConfigSkip(t *testing.T) {
	cfg := configmocks.NewMockManager(t)
	require.NoError(t, applySetupConfig(cfg, SetupConfigInput{Choice: ConfigChoiceSkip}))
}

// TestApplySetupConfigCustom guards the custom_endpoint branch: it persists the
// supplied endpoint and secure flag.
func TestApplySetupConfigCustom(t *testing.T) {
	cfg := configmocks.NewMockManager(t)
	cfg.EXPECT().SetBaseEndpoint("https://api.example.com").Return(nil).Once()
	cfg.EXPECT().SetSecure(true).Return(nil).Once()

	require.NoError(t, applySetupConfig(cfg, SetupConfigInput{
		Choice:   ConfigChoiceCustom,
		Endpoint: "https://api.example.com",
		Secure:   true,
	}))
}

// TestApplySetupConfigCustomMissingEndpoint guards that a custom_endpoint
// choice with no endpoint fails without touching config.
func TestApplySetupConfigCustomMissingEndpoint(t *testing.T) {
	cfg := configmocks.NewMockManager(t)
	require.Error(t, applySetupConfig(cfg, SetupConfigInput{Choice: ConfigChoiceCustom}))
}
