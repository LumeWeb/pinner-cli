package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func TestNewRegisterCommand(t *testing.T) {
	cmd := newRegisterCommand()

	assert.Equal(t, "register", cmd.Name)
	assert.Equal(t, "Setup", cmd.Category)
	assert.NotEmpty(t, cmd.Usage)
	assert.NotEmpty(t, cmd.Description)
	assert.NotNil(t, cmd.Action)

	flagNames := getFlagNames(cmd)
	nameSet := make(map[string]bool)
	for _, n := range flagNames {
		nameSet[n] = true
	}

	expectedFlags := []string{FlagEmail, FlagFirstName, FlagLastName, FlagPassword}
	for _, f := range expectedFlags {
		assert.True(t, nameSet[f], "register command should have flag --%s", f)
	}
}

func TestRegisterAllFlagsProvided(t *testing.T) {
	authService := NewMockAuthService(t)
	output := newTestOutput()

	authService.EXPECT().Register(mock.Anything, "user@example.com", "John", "Doe", "secret123").Return(&auth.RegisterResult{}, nil)

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestRegisterConfigManagerError(t *testing.T) {
	output := newTestOutput()

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	cfgMgrFactory := func() (config.Manager, error) {
		return nil, errors.New("config error")
	}
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService {
		return nil
	}

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config manager")
}

func TestRegisterAuthServiceError(t *testing.T) {
	authService := NewMockAuthService(t)
	output := newTestOutput()

	authService.EXPECT().Register(mock.Anything, "user@example.com", "John", "Doe", "secret123").
		Return(nil, errors.New("registration failed"))

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registration failed")
}

func TestRegisterMissingEmailPrompts(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService { return nil }

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read email")
}

func TestRegisterMissingFirstNamePrompts(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService { return nil }

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read first name")
}

func TestRegisterMissingLastNamePrompts(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService { return nil }

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read last name")
}

func TestRegisterMissingPasswordPrompts(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "user@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
		},
	}

	output := newTestOutput()
	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService { return nil }

	err := register(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read password")
}