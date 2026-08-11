package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestNewConfirmEmailCommand(t *testing.T) {
	cmd := newConfirmEmailCommand()

	assert.Equal(t, "confirm-email", cmd.Name)
	assert.Equal(t, "Setup", cmd.Category)
	assert.NotEmpty(t, cmd.Usage)
	assert.NotEmpty(t, cmd.Description)
	assert.NotNil(t, cmd.Action)

	flagNames := getFlagNames(cmd)
	nameSet := make(map[string]bool)
	for _, n := range flagNames {
		nameSet[n] = true
	}

	expectedFlags := []string{FlagEmail, FlagToken}
	for _, f := range expectedFlags {
		assert.True(t, nameSet[f], "confirm-email command should have flag --%s", f)
	}
}

func TestConfirmEmailConfigManagerError(t *testing.T) {
	output := newTestOutput()

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagToken, Value: "abc123"},
		},
	}

	cfgMgrFactory := func() (config.Manager, error) {
		return nil, errors.New("config error")
	}

	err := confirmEmail(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config manager")
}

func TestConfirmEmailFlagAliases(t *testing.T) {
	cmd := newConfirmEmailCommand()

	emailFlag := findFlag(cmd, FlagEmail)
	require.NotNil(t, emailFlag)
	assert.Contains(t, emailFlag.Names(), "e", "email flag should have -e alias")

	tokenFlag := findFlag(cmd, FlagToken)
	require.NotNil(t, tokenFlag)
	assert.Contains(t, tokenFlag.Names(), "t", "token flag should have -t alias")
}

func TestConfirmEmail_MockCommand_ConfigError(t *testing.T) {
	output := newTestOutput()

	cmd := newMockCommand().
		withString(FlagEmail, "user@example.com").
		withString(FlagToken, "abc123")

	err := confirmEmail(context.Background(), cmd, output, failingConfigMgrFactory())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create config manager")
}

func TestConfirmEmail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/account/verify-email", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	output := newTestOutput()
	output.SetWriter(&buf)

	cmd := newMockCommand().
		withString(FlagEmail, "user@example.com").
		withString(FlagToken, "abc123")

	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		BaseEndpoint: server.URL,
		Secure:       false,
	}).Maybe()

	cfgMgrFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}

	err := confirmEmail(context.Background(), cmd, output, cfgMgrFactory)
	require.NoError(t, err)

	result := buf.String()
	assert.Contains(t, result, "Email verified successfully!")
	assert.Contains(t, result, "pinner auth --email user@example.com")
}

func findFlag(cmd *cli.Command, name string) cli.Flag {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return f
			}
		}
	}
	return nil
}
