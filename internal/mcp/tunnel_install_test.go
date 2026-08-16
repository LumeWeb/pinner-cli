package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/cloudflare"
)

// tunnelFixtureSecret returns a runtime-derived base64 value used as the
// tunnel-secret fixture in tests. It is generated at runtime (never a source
// literal) so no secret-shaped string appears in the repo, and the Cloudflare
// tunnel secret is expected to be base64-encoded.
func tunnelFixtureSecret() string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("fixture-secret-%d", time.Now().UnixNano())))
}

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
	// lastRouteRecordID is the fixed DNS record ID the fake returns from a
	// successful CreateDNSRoute call.
	lastRouteRecordID string

	// deletedTunnels records IDs passed to DeleteTunnel for orphan-cleanup
	// assertions.
	deletedTunnels []string
	// deletedRoutes records record IDs passed to DeleteDNSRoute for clean
	// rollback assertions.
	deletedRoutes []string
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
func (f *fakeCFClient) CreateDNSRoute(_ context.Context, _ cloudflare.Account, _ cloudflare.Zone, host, id string) (string, error) {
	f.lastRouteHost = host
	f.lastRouteID = id
	f.lastRouteRecordID = "dns-record-1"
	if f.dnsErr != nil {
		return "", f.dnsErr
	}
	return f.lastRouteRecordID, nil
}
func (f *fakeCFClient) DeleteDNSRoute(_ context.Context, _ cloudflare.Zone, recordID string) error {
	f.deletedRoutes = append(f.deletedRoutes, recordID)
	return nil
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

	fixtureSecret := tunnelFixtureSecret()
	f := &fakeCFClient{
		accounts: []cloudflare.Account{{ID: "acct-1", Name: "acme"}},
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: fixtureSecret, Token: ""},
		token:    "scoped-jwt",
	}
	ctx := context.Background()
	state, err := provisionCloudflareTunnel(ctx, f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1", Name: "mcp.example.com"},
		"pin", "mcp.example.com")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, "tun-1", state.TunnelID)
	require.Equal(t, fixtureSecret, state.Secret)
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
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: tunnelFixtureSecret()},
		dnsErr:   errors.New("dns refused"),
	}
	_, err := provisionCloudflareTunnel(context.Background(), f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1"}, "pin", "mcp.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dns refused")
	// The partially-created tunnel must be cleaned up so it is not orphaned.
	require.Equal(t, []string{"tun-1"}, f.deletedTunnels)
	// No DNS route was created (DNS failed), so no route rollback occurs.
	require.Empty(t, f.deletedRoutes)
}

// TestProvisionCloudflareTunnelTokenFailure requires a failed token fetch to
// roll back BOTH the created DNS route and the tunnel so neither is orphaned
// and re-provisioning is not blocked by a leftover proxied CNAME.
func TestProvisionCloudflareTunnelTokenFailure(t *testing.T) {
	tmpDir := t.TempDir()
	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) {
		return filepath.Join(tmpDir, "tunnel-state.json"), nil
	}
	defer func() { tunnelStatePath = orig }()

	f := &fakeCFClient{
		accounts: []cloudflare.Account{{ID: "acct-1"}},
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: tunnelFixtureSecret()},
		tokenErr: errors.New("token fetch failed"),
	}
	_, err := provisionCloudflareTunnel(context.Background(), f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1"}, "pin", "mcp.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token fetch failed")
	// Both the DNS route (record dns-record-1) and the tunnel must be removed.
	require.Equal(t, []string{"dns-record-1"}, f.deletedRoutes)
	require.Equal(t, []string{"tun-1"}, f.deletedTunnels)
}

// TestProvisionCloudflareTunnelSaveFailure verifies that a state-save failure
// (after the tunnel and DNS route are created) rolls back both resources rather
// than orphaning the billed tunnel or leaving the hostname occupied.
func TestProvisionCloudflareTunnelSaveFailure(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a regular file where the state dir would go, so MkdirAll fails
	// (ENOTDIR) and SaveCloudflareTunnelState errors after the tunnel and DNS
	// route have already been provisioned.
	blocker := filepath.Join(tmpDir, "block")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) {
		return filepath.Join(blocker, "tunnel-state.json"), nil
	}
	defer func() { tunnelStatePath = orig }()

	f := &fakeCFClient{
		accounts: []cloudflare.Account{{ID: "acct-1"}},
		tunnel:   cloudflare.TunnelRecord{AccountID: "acct-1", ID: "tun-1", Name: "pin", Secret: tunnelFixtureSecret()},
		token:    "scoped-jwt",
	}
	_, err := provisionCloudflareTunnel(context.Background(), f,
		cloudflare.Account{ID: "acct-1"}, cloudflare.Zone{ID: "zone-1"}, "pin", "mcp.example.com")
	require.Error(t, err)
	// Both the DNS route and the tunnel must be rolled back on save failure.
	require.Equal(t, []string{"dns-record-1"}, f.deletedRoutes)
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

// TestCloudflaredDownloadPinnedAndChecksummed verifies every download URL this
// installer can produce is pinned to the release tag (never "latest") and has a
// matching SHA-256 checksum entry, so a fetch can always be verified before the
// artifact is written to disk.
func TestCloudflaredDownloadPinnedAndChecksummed(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, goarch := range []string{"386", "amd64", "arm", "arm64"} {
			u, err := cloudflaredDownloadURL(goos, goarch)
			if err != nil {
				continue // unsupported combination
			}
			require.NotContains(t, u, "releases/latest", "must pin a release tag, not latest")
			require.Contains(t, u, "/download/"+cloudflaredVersion+"/")
			if _, ok := cloudflaredChecksums[goos+"/"+goarch]; !ok {
				t.Fatalf("no pinned checksum for %s/%s (url %s)", goos, goarch, u)
			}
		}
	}
	require.NotEmpty(t, cloudflaredChecksums)
}

// TestCredentialsAndConfigYAML verifies the generated cloudflared credentials
// file and config.yml shapes.
func TestCredentialsAndConfigYAML(t *testing.T) {
	fixtureSecret := tunnelFixtureSecret()
	s := &CloudflareTunnelState{
		AccountID:  "acct-1",
		TunnelID:   "tun-1",
		TunnelName: "pin",
		Secret:     fixtureSecret,
		Hostname:   "mcp.example.com",
	}
	creds := string(s.credentialsJSON())
	require.Contains(t, creds, `"AccountTag":"acct-1"`)
	require.Contains(t, creds, `"TunnelID":"tun-1"`)
	require.Contains(t, creds, fmt.Sprintf(`"TunnelSecret":%q`, fixtureSecret))

	cfgB, err := s.configYAML("http://127.0.0.1:8893")
	require.NoError(t, err)
	cfg := string(cfgB)
	require.Contains(t, cfg, `tunnel: "tun-1"`)
	require.Contains(t, cfg, "credentials-file:")
	require.Contains(t, cfg, `hostname: "mcp.example.com"`)
	require.Contains(t, cfg, `service: "http://127.0.0.1:8893"`)
	require.Contains(t, cfg, "http_status:404")
}
