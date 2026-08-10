package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestNewAdminTokenProvider(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		AuthToken: "test-token",
	}).Maybe()

	provider := NewAdminTokenProvider(cfgMgr)
	require.NotNil(t, provider)
	assert.Equal(t, "test-token", provider.cfgMgr.Config().AuthToken)
}

func TestAdminTokenProvider_Invalidate(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()

	provider := &AdminTokenProvider{
		cfgMgr:     cfgMgr,
		baseToken:  "old-token",
		loginToken: "old-login",
	}

	provider.Invalidate()
	assert.Equal(t, "", provider.loginToken)
	assert.Equal(t, "", provider.baseToken)
}

func TestAdminTokenProvider_GetLoginToken_NonAPIKeyJWT(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()

	provider := &AdminTokenProvider{
		cfgMgr:      cfgMgr,
		apiEndpoint: "http://localhost:8080",
	}

	token, err := provider.GetLoginToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "token", token)
}

func TestAdminTokenProvider_GetLoginToken_Cached(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "same-token"}).Maybe()

	provider := &AdminTokenProvider{
		cfgMgr:     cfgMgr,
		baseToken:  "same-token",
		loginToken: "cached-login",
	}

	token, err := provider.GetLoginToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "cached-login", token)
}

func TestAdminTokenProvider_GetLoginToken_EmptyToken(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()

	provider := &AdminTokenProvider{
		cfgMgr: cfgMgr,
	}

	token, err := provider.GetLoginToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", token)
}
