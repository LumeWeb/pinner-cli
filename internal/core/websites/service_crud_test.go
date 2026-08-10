package websites

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	sdkwebsitesmocks "go.lumeweb.com/ipfs-sdk/mocks/services"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func newUnauthWebsitesService(t *testing.T) *service {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	return &service{
		Base: ipfsbase.New(cfgMgr),
	}
}

func newAuthedNilWebsitesService(t *testing.T) *service {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "token"}).Maybe()
	return &service{
		Base: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("token")),
		ws:             nil,
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
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
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
	assert.Equal(t, coreerrors.ErrServiceUnavailable, err)
}

func TestWebsitesService_WithAuthToken(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()

	svc := &service{
		Base: ipfsbase.New(cfgMgr),
	}
	WithAuthToken("override-token")(svc)
	assert.Equal(t, "override-token", svc.GetAuthToken())
}

// TestWebsitesService_AuthTokenLiveFromConfig verifies the service reads the
// auth token live from the config manager when NO auth token override
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
	// is exercised (it must NOT freeze config's token into Base auth token).
	// Inject an offline client via WithWebsitesClient to avoid any network.
	client, err := ipfs.NewClient("http://127.0.0.1:9", "tok-a")
	require.NoError(t, err)
	iface := New(cfgMgr, "http://127.0.0.1:9", nil, WithClient(client))
	svc, ok := iface.(*service)
	require.True(t, ok, "expected *service, got %T", iface)

	// No auth token override, and the constructor must not have frozen
	// the config token, so getAuthToken() falls through to config live.
	assert.Equal(t, "tok-a", svc.GetAuthToken())

	// Simulate `pinner login` updating the on-disk token, which the watcher
	// live-reloads into the manager; the service must see the new value.
	cfg.AuthToken = "tok-b"
	assert.Equal(t, "tok-b", svc.GetAuthToken(), "service token must live-reload from config")
}

// TestWebsitesService_SetAuthTokenReWiresClient verifies that pushing a new token
// into a long-lived service via SetAuthToken (as the MCP server's config
// subscription does on live-reload) hot-updates the retained *ipfs.Client rather
// than leaving it frozen at bootstrap.
func TestWebsitesService_SetAuthTokenReWiresClient(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().RunAndReturn(func() *config.Config { return &config.Config{AuthToken: "tok-a"} }).Maybe()

	client, err := ipfs.NewClient("http://127.0.0.1:9", "tok-a")
	require.NoError(t, err)
	iface := New(cfgMgr, "http://127.0.0.1:9", nil, WithClient(client))
	svc, ok := iface.(*service)
	require.True(t, ok, "expected *service, got %T", iface)
	assert.Same(t, client, svc.client, "injected client must be retained")
	assert.Equal(t, "tok-a", svc.client.BearerToken())

	// Simulate the config subscription firing on a `pinner login`.
	svc.SetAuthToken("tok-c")
	assert.Equal(t, "tok-c", svc.client.BearerToken(), "retained client token must hot-update")
}

// TestWebsitesService_SetAuthTokenConcurrent guards the data race between the
// config-watcher goroutine (SetAuthToken swaps s.ws) and request goroutines
// reading s.ws. Run with -race to verify the mutex serializes them.
func TestWebsitesService_SetAuthTokenConcurrent(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().RunAndReturn(func() *config.Config { return &config.Config{AuthToken: "tok-a"} }).Maybe()

	// Inject a mock SDK service so List does real work off a fake.
	mockSvc := sdkwebsitesmocks.NewMockWebsitesService(t)
	mockSvc.EXPECT().List(mock.Anything).Return([]ipfs.WebsiteItem{}, nil).Maybe()

	client, err := ipfs.NewClient("http://127.0.0.1:9", "tok-a")
	require.NoError(t, err)
	iface := New(cfgMgr, "http://127.0.0.1:9", nil, WithClient(client))
	svc, ok := iface.(*service)
	require.True(t, ok, "expected *service, got %T", iface)
	svc.ws = mockSvc

	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Requests reading s.ws.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if _, err := svc.List(ctx); err != nil {
						// coreerrors.ErrServiceUnavailable is transient during a swap; ignore it.
					}
				}
			}
		}()
	}

	// Config-watcher writer mutating s.ws.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				svc.SetAuthToken(fmt.Sprintf("tok-%d-%d", n, j))
			}
		}(i)
	}

	// Let them race, then stop the readers.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
