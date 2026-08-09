package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func newUnauthWebsitesService(t *testing.T) *websitesService {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	return &websitesService{
		ipfsServiceBase: ipfsServiceBase{cfgMgr: cfgMgr, authToken: ""},
	}
}

func newAuthedNilWebsitesService(t *testing.T) *websitesService {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()
	return &websitesService{
		ipfsServiceBase: ipfsServiceBase{cfgMgr: cfgMgr, authToken: "token"},
		service:         nil,
	}
}

func TestWebsitesService_List_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.List(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_List_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilWebsitesService(t)
	_, err := svc.List(context.Background())
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestWebsitesService_Create_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.Create(context.Background(), "example.com", "QmHash", "cid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_CreateWithOptions_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.CreateWithOptions(context.Background(), ipfs.WebsiteRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_Get_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.Get(context.Background(), "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_Update_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.Update(context.Background(), "example.com", "QmHash", "cid", "ipns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_UpdateWithOptions_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.UpdateWithOptions(context.Background(), "example.com", ipfs.WebsiteUpdateRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_Delete_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	err := svc.Delete(context.Background(), "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_Validate_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.Validate(context.Background(), "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_GetSSLStatus_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.GetSSLStatus(context.Background(), "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_GetConfig_Unauthenticated(t *testing.T) {
	svc := newUnauthWebsitesService(t)
	_, err := svc.GetConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestWebsitesService_GetConfig_ServiceUnavailable(t *testing.T) {
	svc := newAuthedNilWebsitesService(t)
	_, err := svc.GetConfig(context.Background())
	require.Error(t, err)
	assert.Equal(t, ErrServiceUnavailable, err)
}

func TestWebsitesService_WithWebsitesAuthToken(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()

	svc := &websitesService{
		ipfsServiceBase: ipfsServiceBase{cfgMgr: cfgMgr},
	}
	WithWebsitesAuthToken("override-token")(svc)
	assert.Equal(t, "override-token", svc.getAuthToken())
}

// TestWebsitesService_AuthTokenLiveFromConfig verifies the service reads the
// auth token live from the config manager when NO WithWebsitesAuthToken override
// is pinned. This is what keeps a long-lived MCP server live-reload aware: a
// `pinner login` that rewrites the on-disk token is reflected by the running
// server's websites/DNS/IPNS services without a restart. Pinning the startup
// token as an override (the root.go bug this guards against) would freeze it and
// defeat live reload.
func TestWebsitesService_AuthTokenLiveFromConfig(t *testing.T) {
	// Mutable config holder so we can simulate a live-reloaded token.
	cfg := &config.Config{AuthToken: "tok-a"}
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().RunAndReturn(func() *config.Config { return cfg }).Maybe()

	// Build via the real NewWebsitesService so the constructor's token-handling
	// is exercised (it must NOT freeze config's token into ipfsServiceBase.authToken).
	// Inject an offline client via WithWebsitesClient to avoid any network.
	client, err := ipfs.NewClient("http://127.0.0.1:9", "tok-a")
	require.NoError(t, err)
	output := NewOutputFormatter(false, false, false, false)
	iface := NewWebsitesService(cfgMgr, output, "http://127.0.0.1:9", WithWebsitesClient(client))
	svc, ok := iface.(*websitesService)
	require.True(t, ok, "expected *websitesService, got %T", iface)

	// No WithWebsitesAuthToken override, and the constructor must not have frozen
	// the config token, so getAuthToken() falls through to config live.
	assert.Equal(t, "tok-a", svc.getAuthToken())

	// Simulate `pinner login` updating the on-disk token, which the watcher
	// live-reloads into the manager; the service must see the new value.
	cfg.AuthToken = "tok-b"
	assert.Equal(t, "tok-b", svc.getAuthToken(), "service token must live-reload from config")
}

// TestWebsitesService_SetAuthTokenReWiresClient verifies that pushing a new token
// into a long-lived service via SetAuthToken (as the MCP server's config
// subscription does on live-reload) hot-updates the retained *ipfs.Client rather
// than leaving it frozen at bootstrap.
func TestWebsitesService_SetAuthTokenReWiresClient(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().RunAndReturn(func() *config.Config { return &config.Config{AuthToken: "tok-a"} }).Maybe()
	output := NewOutputFormatter(false, false, false, false)

	client, err := ipfs.NewClient("http://127.0.0.1:9", "tok-a")
	require.NoError(t, err)
	iface := NewWebsitesService(cfgMgr, output, "http://127.0.0.1:9", WithWebsitesClient(client))
	svc, ok := iface.(*websitesService)
	require.True(t, ok, "expected *websitesService, got %T", iface)
	assert.Same(t, client, svc.client, "injected client must be retained")
	assert.Equal(t, "tok-a", svc.client.BearerToken())

	// Simulate the config subscription firing on a `pinner login`.
	svc.SetAuthToken("tok-c")
	assert.Equal(t, "tok-c", svc.client.BearerToken(), "retained client token must hot-update")
}
