package catalogops

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// mockENSIPNSService is a minimal in-package ipns.Service fake for the ENS
// operations. All methods default to an "unimplemented" error so a test must
// opt into the behavior it exercises, surfacing unexpected calls.
type mockENSIPNSService struct {
	requireAuth func() error
	createKey   func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error)
	listKeys    func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error)
	publish     func(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error)
	deleteKey   func(ctx context.Context, id string) error
}

func (m *mockENSIPNSService) SetAuthToken(string) {}
func (m *mockENSIPNSService) RequireAuthenticated() error {
	if m.requireAuth != nil {
		return m.requireAuth()
	}
	return nil
}
func (m *mockENSIPNSService) ListKeys(ctx context.Context, _ ...ipfs.ListKeyOption) ([]ipfs.IPNSKeyResponse, error) {
	if m.listKeys == nil {
		return nil, errors.New("unexpected ListKeys")
	}
	return m.listKeys(ctx)
}
func (m *mockENSIPNSService) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if m.createKey == nil {
		return nil, errors.New("unexpected CreateKey")
	}
	return m.createKey(ctx, name, key)
}
func (m *mockENSIPNSService) GetKey(ctx context.Context, _ string) (*ipfs.IPNSKeyResponse, error) {
	return nil, errors.New("unexpected GetKey")
}
func (m *mockENSIPNSService) DeleteKey(ctx context.Context, id string) error {
	if m.deleteKey == nil {
		return errors.New("unexpected DeleteKey")
	}
	return m.deleteKey(ctx, id)
}
func (m *mockENSIPNSService) Publish(ctx context.Context, cid, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if m.publish == nil {
		return nil, errors.New("unexpected Publish")
	}
	return m.publish(ctx, cid, keyName, ttl)
}
func (m *mockENSIPNSService) Republish(ctx context.Context, _ string) (*ipfs.IPNSRepublishResponse, error) {
	return nil, errors.New("unexpected Republish")
}
func (m *mockENSIPNSService) Resolve(ctx context.Context, _ string) (*ipfs.IPNSResolveResponse, error) {
	return nil, errors.New("unexpected Resolve")
}

func newENSKey(id int, name, ipnsName string) *ipfs.IPNSKeyResponse {
	return &ipfs.IPNSKeyResponse{Id: id, Name: name, IpnsName: ipnsName, PeerId: "12D3KooW...", Created: time.Now()}
}

func TestPointENSSuccessNewKey(t *testing.T) {
	svc := &mockENSIPNSService{
		createKey: func(_ context.Context, name string, _ *string) (*ipfs.IPNSKeyResponse, error) {
			return newENSKey(1, name, "k51qzi5uqu5djx"), nil
		},
		publish: func(_ context.Context, cid, keyName string, _ *string) (*ipfs.IPNSPublishResponse, error) {
			return &ipfs.IPNSPublishResponse{Name: keyName, Value: cid, Sequence: 1, Published: time.Now(), Validity: time.Now().Add(time.Hour)}, nil
		},
	}

	res, err := PointENS(context.Background(), svc, "vitalik.eth", "bafybeigtest")
	require.NoError(t, err)
	require.Equal(t, "vitalik.eth", res.Name)
	require.Equal(t, "bafybeigtest", res.CID)
	require.Equal(t, "k51qzi5uqu5djx", res.IPNSName)
	require.Equal(t, "ipns://k51qzi5uqu5djx", res.Contenthash)
	require.True(t, res.Created)
	require.Equal(t, "https://vitalik.eth.limo", res.VerifyURL)
	require.NotEmpty(t, res.NextSteps)
	// Wallet guidance must surface the exact contenthash and verify URL within
	// the steps, without assuming a single wallet.
	require.Contains(t, fmt.Sprint(res.NextSteps), "https://vitalik.eth.limo")
	require.Contains(t, fmt.Sprint(res.NextSteps), "ipns://k51qzi5uqu5djx")
	require.Equal(t, "https://vitalik.eth.limo", res.VerifyURL)
}

func TestPointENSKeyReuse(t *testing.T) {
	svc := &mockENSIPNSService{
		createKey: func(_ context.Context, _ string, _ *string) (*ipfs.IPNSKeyResponse, error) {
			return nil, fmt.Errorf("key already exists")
		},
		listKeys: func(_ context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return []ipfs.IPNSKeyResponse{*newENSKey(1, "vitalik.eth", "k51qzi5uexisting")}, nil
		},
		publish: func(_ context.Context, cid, keyName string, _ *string) (*ipfs.IPNSPublishResponse, error) {
			return &ipfs.IPNSPublishResponse{Name: keyName, Value: cid, Sequence: 2, Published: time.Now(), Validity: time.Now().Add(time.Hour)}, nil
		},
	}

	res, err := PointENS(context.Background(), svc, "vitalik.eth", "bafybeigtest")
	require.NoError(t, err)
	require.False(t, res.Created)
	require.Equal(t, "k51qzi5uexisting", res.IPNSName)
	require.Equal(t, "ipns://k51qzi5uexisting", res.Contenthash)
}

func TestPointENSNonETHVerifyURL(t *testing.T) {
	svc := &mockENSIPNSService{
		createKey: func(_ context.Context, name string, _ *string) (*ipfs.IPNSKeyResponse, error) {
			return newENSKey(1, name, "k51qzi5uqu5djx"), nil
		},
		publish: func(_ context.Context, cid, keyName string, _ *string) (*ipfs.IPNSPublishResponse, error) {
			return &ipfs.IPNSPublishResponse{Name: keyName, Value: cid, Sequence: 1, Published: time.Now(), Validity: time.Now().Add(time.Hour)}, nil
		},
	}

	res, err := PointENS(context.Background(), svc, "brave.crypto", "bafybeigtest")
	require.NoError(t, err)
	require.Equal(t, "https://k51qzi5uqu5djx.ipns.inbrowser.link", res.VerifyURL)
}

func TestPointENSValidationErrors(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		_, err := PointENS(context.Background(), &mockENSIPNSService{}, "", "cid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "name is required")
	})
	t.Run("missing cid", func(t *testing.T) {
		_, err := PointENS(context.Background(), &mockENSIPNSService{}, "vitalik.eth", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cid is required")
	})
	t.Run("not authenticated", func(t *testing.T) {
		svc := &mockENSIPNSService{requireAuth: func() error { return errors.New("not authenticated") }}
		_, err := PointENS(context.Background(), svc, "vitalik.eth", "cid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authenticated")
	})
}

func TestUnpointENSSuccess(t *testing.T) {
	svc := &mockENSIPNSService{
		listKeys: func(_ context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return []ipfs.IPNSKeyResponse{*newENSKey(1, "vitalik.eth", "k51qzi5uqu5djx")}, nil
		},
		deleteKey: func(_ context.Context, id string) error {
			require.Equal(t, "1", id)
			return nil
		},
	}

	res, err := UnpointENS(context.Background(), svc, "vitalik.eth")
	require.NoError(t, err)
	require.True(t, res.Deleted)
	require.Equal(t, "vitalik.eth", res.Name)
	require.Equal(t, "k51qzi5uqu5djx", res.IPNSName)
	require.NotEmpty(t, res.NextSteps)
}

func TestUnpointENSKeyNotFound(t *testing.T) {
	svc := &mockENSIPNSService{
		listKeys: func(_ context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return nil, nil
		},
	}
	_, err := UnpointENS(context.Background(), svc, "nexist.eth")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no IPNS key found")
}

// TestENSOperationsInvoke runs ens_point and ens_unpoint through the operation
// catalog's Invoke gate, exercising the same path the MCP dispatch uses. It
// verifies required-arg validation and the destructive confirm gate for
// ens_unpoint.
func TestENSOperationsInvoke(t *testing.T) {
	svc := &mockENSIPNSService{
		createKey: func(_ context.Context, name string, _ *string) (*ipfs.IPNSKeyResponse, error) {
			return newENSKey(1, name, "k51qzi5uqu5djx"), nil
		},
		listKeys: func(_ context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return []ipfs.IPNSKeyResponse{*newENSKey(1, "vitalik.eth", "k51qzi5uqu5djx")}, nil
		},
		publish: func(_ context.Context, cid, keyName string, _ *string) (*ipfs.IPNSPublishResponse, error) {
			return &ipfs.IPNSPublishResponse{Name: keyName, Value: cid, Sequence: 1, Published: time.Now(), Validity: time.Now().Add(time.Hour)}, nil
		},
		deleteKey: func(_ context.Context, _ string) error { return nil },
	}
	cat := catalog.NewCatalog()
	for _, op := range ENSOperations(ensDepsFor(t, svc)) {
		if err := cat.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}

	t.Run("ens_point requires name and cid", func(t *testing.T) {
		_, err := cat.Invoke(context.Background(), "ens_point", map[string]any{"name": "vitalik.eth"}, catalog.ActorModel)
		require.Error(t, err)
	})

	t.Run("ens_point success", func(t *testing.T) {
		res, err := cat.Invoke(context.Background(), "ens_point", map[string]any{"name": "vitalik.eth", "cid": "bafybeigtest"}, catalog.ActorModel)
		require.NoError(t, err)
		r, ok := res.(*ENSPointResult)
		require.True(t, ok)
		require.Equal(t, "ipns://k51qzi5uqu5djx", r.Contenthash)
	})

	t.Run("model actor always refused for destructive unpoint", func(t *testing.T) {
		// A model agent cannot confirm a destructive delete on its own; the
		// catalog gate returns ErrConfirmRequired regardless of confirm value
		// (ens_unpoint's confirm is AgentRequired, not AgentConfirm).
		_, err := cat.Invoke(context.Background(), "ens_unpoint", map[string]any{"name": "vitalik.eth", "confirm": true}, catalog.ActorModel)
		require.Error(t, err)
		require.Contains(t, err.Error(), "destructive")
	})

	t.Run("human unpoint without confirm is rejected", func(t *testing.T) {
		// A human actor bypasses the model gate but the handler still enforces
		// confirm; passing false must fail.
		_, err := cat.Invoke(context.Background(), "ens_unpoint", map[string]any{"name": "vitalik.eth", "confirm": false}, catalog.ActorHuman)
		require.Error(t, err)
		require.Contains(t, err.Error(), "confirmation is required")
	})

	t.Run("human unpoint with confirm omitted is rejected", func(t *testing.T) {
		// confirm defaults to false, so a non-model actor that omits it must
		// NOT delete the key: "defaults are filled before the handler runs",
		// so an omitted confirm resolves to false and the gate fails. This
		// guards the destructive-op confirmation contract for app/human actors.
		_, err := cat.Invoke(context.Background(), "ens_unpoint", map[string]any{"name": "vitalik.eth"}, catalog.ActorHuman)
		require.Error(t, err)
		require.Contains(t, err.Error(), "confirmation is required")
	})

	t.Run("human unpoint success after confirm", func(t *testing.T) {
		res, err := cat.Invoke(context.Background(), "ens_unpoint", map[string]any{"name": "vitalik.eth", "confirm": true}, catalog.ActorHuman)
		require.NoError(t, err)
		r, ok := res.(*ENSUnpointResult)
		require.True(t, ok)
		require.True(t, r.Deleted)
	})
}

func TestResolveVerifyURLShared(t *testing.T) {
	require.Equal(t, "https://vitalik.eth.limo", ResolveVerifyURL("vitalik.eth", "k51qzi"))
	require.Equal(t, "https://k51qzi.ipns.inbrowser.link", ResolveVerifyURL("brave.crypto", "k51qzi"))
	require.Equal(t, "https://sub.vitalik.eth.limo", ResolveVerifyURL("sub.vitalik.eth", "k51qzi"))
}

// ensDepsFor builds an ENSDeps whose IPNS service is the given fake. It routes
// through NewAuthenticated (the auth-token path) so service() returns the fake
// without a real config manager.
func ensDepsFor(t *testing.T, svc ipns.Service) ENSDeps {
	return ENSDeps{IPNS: IPNSDeps{
		CfgMgr:           func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ string, _ bool) (ipns.Service, error) {
			return svc, nil
		},
		// A config token makes service() route through NewAuthenticated (the
		// auth-token path) instead of falling through to a nil ServiceFactory.
		GetAuthToken: func() string { return "config-token" },
	}}
}
