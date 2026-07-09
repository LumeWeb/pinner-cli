package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestConfigKeyToEnvVar(t *testing.T) {
	testCases := []struct {
		name     string
		key      string
		expected string
	}{
		{"auth_token", "auth_token", "PINNER_AUTH_TOKEN"},
		{"base_endpoint", "base_endpoint", "PINNER_BASE_ENDPOINT"},
		{"max_retries", "max_retries", "PINNER_MAX_RETRIES"},
		{"memory_limit", "memory_limit", "PINNER_MEMORY_LIMIT"},
		{"secure", "secure", "PINNER_SECURE"},
		{"gateway_endpoint", "gateway_endpoint", "PINNER_GATEWAY_ENDPOINT"},
		{"hyphenated key", "some-key", "PINNER_SOME_KEY"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := configKeyToEnvVar(tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNewConfigCommand(t *testing.T) {
	cmd := newConfigCommand()

	assert.Equal(t, "config", cmd.Name)
	assert.Equal(t, "System", cmd.Category)
	assert.NotEmpty(t, cmd.Usage)
	assert.NotNil(t, cmd.Action)
	assert.Len(t, cmd.Commands, 2)
	assert.Equal(t, "get", cmd.Commands[0].Name)
	assert.Equal(t, "set", cmd.Commands[1].Name)
}

func TestShowAllConfigError(t *testing.T) {
	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) {
		return nil, errors.New("config error")
	}

	err := showAllConfig(output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestShowAllConfigWithBoolValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().GetAllDescriptions().Return(map[string]string{"secure": "Use HTTPS"})
	cfgMgr.EXPECT().All().Return(map[string]any{"secure": true})

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }

	err := showAllConfig(output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestShowAllConfigWithIntValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().GetAllDescriptions().Return(map[string]string{"max_retries": "Max retries"})
	cfgMgr.EXPECT().All().Return(map[string]any{"max_retries": 3})

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }

	err := showAllConfig(output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestShowAllConfigWithEmptyDescription(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().GetAllDescriptions().Return(map[string]string{"custom_key": ""})
	cfgMgr.EXPECT().All().Return(map[string]any{"custom_key": "value"})

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }

	err := showAllConfig(output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestShowAllConfigWithNotSetValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().GetAllDescriptions().Return(map[string]string{"auth_token": "Auth token"})
	cfgMgr.EXPECT().All().Return(map[string]any{})

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }

	err := showAllConfig(output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestGetConfigError(t *testing.T) {
	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) {
		return nil, errors.New("config error")
	}

	cmd := newMockCommand().withArgs("auth_token")
	err := getConfig(cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestSetConfigError(t *testing.T) {
	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) {
		return nil, errors.New("config error")
	}

	cmd := newMockCommand().withArgs("base_endpoint", "test.com")
	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestGetConfigMissingKey(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand()

	err := getConfig(cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")
}

func TestGetConfigSuccess(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Get("base_endpoint").Return("pinner.xyz", true, nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("base_endpoint")

	err := getConfig(cmd, output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestGetConfigGetFails(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Get("unknown_key").Return(nil, false, errors.New("not found"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("unknown_key")

	err := getConfig(cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get config key")
}

func TestSetConfigMissingKeyOrValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("key")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key and value are required")
}

func TestSetConfigNewKey(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("custom_key").Return(false)
	cfgMgr.EXPECT().Set(mock.Anything, "custom_key", "custom_value").Return(nil)
	cfgMgr.EXPECT().Persist().Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("custom_key", "custom_value")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestSetConfigBoolValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("secure").Return(true)
	cfgMgr.EXPECT().Get("secure").Return(true, true, nil)
	cfgMgr.EXPECT().Set(mock.Anything, "secure", false).Return(nil)
	cfgMgr.EXPECT().Persist().Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("secure", "false")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestSetConfigInvalidBoolValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("secure").Return(true)
	cfgMgr.EXPECT().Get("secure").Return(true, true, nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("secure", "notabool")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be true or false")
}

func TestSetConfigInvalidIntValue(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("max_retries").Return(true)
	cfgMgr.EXPECT().Get("max_retries").Return(3, true, nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("max_retries", "notanint")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer")
}

func TestSetConfigDryRun(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("max_retries").Return(true)
	cfgMgr.EXPECT().Get("max_retries").Return(3, true, nil)
	cfgMgr.EXPECT().GetDescription("max_retries").Return("Max retries")

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("max_retries", "5").withBool(FlagDryRun, true)

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.NoError(t, err)
}

func TestSetConfigPersistFails(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Exists("custom_key").Return(false)
	cfgMgr.EXPECT().Set(mock.Anything, "custom_key", "value").Return(nil)
	cfgMgr.EXPECT().Persist().Return(errors.New("disk full"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	cmd := newMockCommand().withArgs("custom_key", "value")

	err := setConfig(context.Background(), cmd, output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save config")
}
