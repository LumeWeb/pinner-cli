package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// TestWebRegisterURL verifies the registration page URL is derived from the
// configured account endpoint on the account-subdomain web app.
func TestWebRegisterURL(t *testing.T) {
	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }

	url, err := webRegisterURL(cfgMgrFactory)
	require.NoError(t, err)
	require.Equal(t, "https://account.pinner.xyz/register", url)

	// A trailing slash on the endpoint must not produce a double slash.
	cfgMgrFactoryWithSlash := func() (config.Manager, error) {
		m := newTestConfigMgr(t)
		m.Config().BaseEndpoint = "pinner.xyz/"
		return m, nil
	}
	url, err = webRegisterURL(cfgMgrFactoryWithSlash)
	require.NoError(t, err)
	require.False(t, strings.HasSuffix(url, "//register"), "URL must not have a double slash: %s", url)
}

// TestRegisterOpenProceeds verifies that with --open set the register flow
// still completes registration (the browser open is a best-effort side effect
// that must not derail the command, even when no opener is available).
func TestRegisterOpenProceeds(t *testing.T) {
	authService := NewMockAuthService(t)

	authService.EXPECT().Register(mock.Anything, "open@example.com", "John", "Doe", "secret123").
		Return(&auth.RegisterResult{}, nil)

	cmd := newMockCommand().
		withString(FlagEmail, "open@example.com").
		withString(FlagFirstName, "John").
		withString(FlagLastName, "Doe").
		withString(FlagPassword, "secret123").
		withBool("open", true)

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), cmd, newTestOutput(), cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

// TestRegisterNoOpenStillRegisters guards that --open is entirely optional:
// with the flag absent (the default), register behaves exactly as before.
func TestRegisterNoOpenStillRegisters(t *testing.T) {
	authService := NewMockAuthService(t)

	authService.EXPECT().Register(mock.Anything, "plain@example.com", "John", "Doe", "secret123").
		Return(&auth.RegisterResult{}, nil)

	cmd := newMockCommand().
		withString(FlagEmail, "plain@example.com").
		withString(FlagFirstName, "John").
		withString(FlagLastName, "Doe").
		withString(FlagPassword, "secret123")

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), cmd, newTestOutput(), cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}
