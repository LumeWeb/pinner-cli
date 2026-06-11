package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/pkg/config"
)

func TestSetupCommandContext(t *testing.T) {
	orig := configManagerFactory
	defer func() { configManagerFactory = orig }()

	configManagerFactory = func() (config.Manager, error) { return newTestConfigMgr(t), nil }

	cmd := &cli.Command{}
	cfgMgr, output, err := setupCommandContext(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfgMgr)
	require.NotNil(t, output)
}

func TestSetupCommandContextError(t *testing.T) {
	orig := configManagerFactory
	defer func() { configManagerFactory = orig }()

	configManagerFactory = func() (config.Manager, error) {
		return nil, errors.New("config error")
	}

	cmd := &cli.Command{}
	cfgMgr, output, err := setupCommandContext(cmd)
	require.Error(t, err)
	assert.Nil(t, cfgMgr)
	assert.Nil(t, output)
}

func TestSetupOutput(t *testing.T) {
	orig := configManagerFactory
	defer func() { configManagerFactory = orig }()

	configManagerFactory = func() (config.Manager, error) { return newTestConfigMgr(t), nil }

	cmd := &cli.Command{}
	output := setupOutput(cmd)
	require.NotNil(t, output)
}

func TestRequireUpdateFieldsSet(t *testing.T) {
	cmd := newMockCommand().withIsSet("name", true)
	err := requireUpdateFields(cmd, "name", "email")
	require.NoError(t, err)
}

func TestRequireUpdateFieldsNotSet(t *testing.T) {
	cmd := newMockCommand()
	err := requireUpdateFields(cmd, "name", "email")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one field must be provided")
	assert.Contains(t, err.Error(), "name")
	assert.Contains(t, err.Error(), "email")
}

func TestRequireSetInt(t *testing.T) {
	cmd := newMockCommand().withIsSet("limit", true).withInt("limit", 10)
	v, err := requireSetInt(cmd, "limit")
	require.NoError(t, err)
	assert.Equal(t, 10, v)
}

func TestRequireSetIntNotSet(t *testing.T) {
	cmd := newMockCommand()
	_, err := requireSetInt(cmd, "limit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit is required")
}

func TestRequireSetIntZero(t *testing.T) {
	cmd := newMockCommand().withIsSet("limit", true).withInt("limit", 0)
	_, err := requireSetInt(cmd, "limit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit must be greater than zero")
}
