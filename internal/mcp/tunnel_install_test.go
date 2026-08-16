package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/cloudflare"
)

// fakeCFClient is an mcp-side fake of cloudflare.Client for wizard tests.
type fakeCFClient struct {
	verifyErr   error
	accounts    []cloudflare.Account
	accountsErr error
	tunnel      cloudflare.TunnelRecord
	tunnelErr   error
	token       string
	tokenErr    error
	dnsErr      error

	lastCreateName string
	lastRouteHost  string
	lastRouteID    string

	// deletedTunnels records IDs passed to DeleteTunnel for orphan-cleanup
	// assertions.
	deletedTunnels []string
}

func (f *fakeCFClient) VerifyToken(context.Context) error { return f.verifyErr }
func (f *fakeCFClient) ListAccounts(context.Context) ([]cloudflare.Account, error) {
	return f.accounts, f.accountsErr
}
func (f *fakeCFClient) FindZone(_ context.Context, _ cloudflare.Account, _ string) (cloudflare.Zone, error) {
	return cloudflare.Zone{ID: "zone-1", Name: "mcp.example.com"}, nil
}
func (f *fakeCFClient) CreateTunnel(_ context.Context, _ cloudflare.Account, name string) (cloudflare.TunnelRecord, error) {
	f.lastCreateName = name
	return f.tunnel, f.tunnelErr
}
func (f *fakeCFClient) GetTunnelToken(_ context.Context, _ cloudflare.Account, _ string) (string, error) {
	return f.token, f.tokenErr
}
func (f *fakeCFClient) CreateDNSRoute(_ context.Context, _ cloudflare.Account, _ cloudflare.Zone, host, id string) error {
	f.lastRouteHost = host
	f.lastRouteID = id
	return f.dnsErr
}
func (f *fakeCFClient) DeleteTunnel(_ context.Context, _ cloudflare.Account, id string) error {
	f.deletedTunnels = append(f.deletedTunnels, id)
	return nil
}

// TestProvisionCloudflareTunnel exercises the provisioning core against a fake
// and asserts the persisted tunnel state holds the tunnel-scoped credential.
func TestProvisionCloudflareTunnel(t *testing.T) {
	// Point the state file at a temp dir so the test never touches the real
	// user config. Capture the path once so Save and Load resolve the same file.
	tmpDir := t.TempDir()
	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) {
		return filepath.Join(tmpDir, "tunnel-state.json"), nil
	}
	defer func() { tunnelStatePath = orig }()

	f := &fakeCFClient{
		accounts: []cloudflare.Account{{ID: "acct-1", Name: "acme"}},
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: "c2VjcmV0", Token: ""},
		token:    "scoped-jwt",
	}
	ctx := context.Background()
	state, err := provisionCloudflareTunnel(ctx, f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1", Name: "mcp.example.com"},
		"pin", "mcp.example.com")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, "tun-1", state.TunnelID)
	require.Equal(t, "c2VjcmV0", state.Secret)
	require.Equal(t, "scoped-jwt", state.Token)
	require.Equal(t, "mcp.example.com", state.Hostname)
	require.Equal(t, "pin", f.lastCreateName)
	require.Equal(t, "tun-1", f.lastRouteID)

	// The state must have been persisted to disk.
	loaded, err := LoadCloudflareTunnelState()
	require.NoError(t, err)
	require.Equal(t, state.TunnelID, loaded.TunnelID)
}

// TestProvisionCloudflareTunnelDNSFailure verifies a DNS-route failure is
// surfaced (no silent partial state).
func TestProvisionCloudflareTunnelDNSFailure(t *testing.T) {
	tmpDir := t.TempDir()
	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) {
		return filepath.Join(tmpDir, "tunnel-state.json"), nil
	}
	defer func() { tunnelStatePath = orig }()

	f := &fakeCFClient{
		accounts: []cloudflare.Account{{ID: "acct-1"}},
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: "c2VjcmV0"},
		dnsErr:   errors.New("dns refused"),
	}
	_, err := provisionCloudflareTunnel(context.Background(), f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1"}, "pin", "mcp.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dns refused")
	// The partially-created tunnel must be cleaned up so it is not orphaned.
	require.Equal(t, []string{"tun-1"}, f.deletedTunnels)
}

// TestCloudflaredDownloadURL verifies the platform download-URL mapping.
func TestCloudflaredDownloadURL(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"linux", "amd64", "cloudflared-linux-amd64"},
		{"linux", "arm64", "cloudflared-linux-arm64"},
		{"darwin", "arm64", "cloudflared-darwin-arm64.tgz"},
		{"windows", "amd64", "cloudflared-windows-amd64.exe"},
	}
	for _, c := range cases {
		u, err := cloudflaredDownloadURL(c.goos, c.goarch)
		require.NoError(t, err)
		require.Contains(t, u, c.want, "%s/%s", c.goos, c.goarch)
	}
	_, err := cloudflaredDownloadURL("plan9", "amd64")
	require.Error(t, err)
}

// TestCredentialsAndConfigYAML verifies the generated cloudflared credentials
// file and config.yml shapes.
func TestCredentialsAndConfigYAML(t *testing.T) {
	s := &CloudflareTunnelState{
		AccountID:  "acct-1",
		TunnelID:   "tun-1",
		TunnelName: "pin",
		Secret:     "c2VjcmV0",
		Hostname:   "mcp.example.com",
	}
	creds := string(s.credentialsJSON())
	require.Contains(t, creds, `"AccountTag":"acct-1"`)
	require.Contains(t, creds, `"TunnelID":"tun-1"`)
	require.Contains(t, creds, `"TunnelSecret":"c2VjcmV0"`)

	cfg := string(s.configYAML("http://127.0.0.1:8893"))
	require.Contains(t, cfg, "tunnel: tun-1")
	require.Contains(t, cfg, "credentials-file:")
	require.Contains(t, cfg, "hostname: mcp.example.com")
	require.Contains(t, cfg, "service: http://127.0.0.1:8893")
	require.Contains(t, cfg, "http_status:404")
}
