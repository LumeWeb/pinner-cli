package cloudflare

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewRejectsEmptyToken ensures the client factory fails fast on an empty
// API token rather than returning a client that will 403 later.
func TestNewRejectsEmptyToken(t *testing.T) {
	_, err := New("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "API token is empty")
}

// TestZoneHostnameMatches covers the FindZone candidate predicate: a tunnel
// hostname maps to either an exact zone name or a subdomain nested beneath it,
// ignoring case and an optional https:// scheme.
func TestZoneHostnameMatches(t *testing.T) {
	cases := []struct {
		zoneName string
		domain   string
		want     bool
	}{
		{"example.com", "example.com", true},             // exact apex
		{"example.com", "mcp.example.com", true},         // subdomain
		{"example.com", "a.b.example.com", true},         // nested subdomain
		{"example.com", "https://mcp.example.com", true}, // scheme tolerated
		{"example.com", "EXAMPLE.com", true},             // case-insensitive
		{"example.com", "notexample.com", false},         // suffix lookalike
		{"example.com", "other.org", false},              // unrelated
	}
	for _, c := range cases {
		require.Equal(t, c.want, zoneHostnameMatches(c.zoneName, c.domain), "zone=%q domain=%q", c.zoneName, c.domain)
	}
}

// fakeClient is an in-memory Client for tests. It records calls and returns
// canned results, so install/service wizard tests never touch the network.
type fakeClient struct {
	verifyErr    error
	accounts     []Account
	accountsErr  error
	createTunnel TunnelRecord
	createErr    error
	token        string
	tokenErr     error
	dnsErr       error

	lastAccount Account
	lastZone    Zone
	lastName    string
	lastTunnel  string
	lastHost    string

	// deletedTunnels records tunnel IDs passed to DeleteTunnel, so tests can
	// assert mid-provision orphan cleanup.
	deletedTunnels []string
	// deletedRoutes records DNS record IDs passed to DeleteDNSRoute so tests
	// can assert the hostname route is rolled back on failure.
	deletedRoutes []string
}

func (f *fakeClient) VerifyToken(context.Context) error { return f.verifyErr }

func (f *fakeClient) ListAccounts(context.Context) ([]Account, error) {
	return f.accounts, f.accountsErr
}

func (f *fakeClient) FindZone(_ context.Context, a Account, _ string) (Zone, error) {
	f.lastAccount = a
	if len(f.accounts) > 0 {
		return Zone{ID: "zone-1", Name: f.accounts[0].Name}, nil
	}
	return Zone{ID: "zone-1", Name: "example.com"}, nil
}

func (f *fakeClient) CreateTunnel(_ context.Context, a Account, name string) (TunnelRecord, error) {
	f.lastAccount = a
	f.lastName = name
	return f.createTunnel, f.createErr
}

func (f *fakeClient) GetTunnelToken(_ context.Context, a Account, tunnelID string) (string, error) {
	f.lastAccount = a
	f.lastTunnel = tunnelID
	return f.token, f.tokenErr
}

func (f *fakeClient) CreateDNSRoute(_ context.Context, a Account, z Zone, host, tunnelID string) (string, error) {
	f.lastAccount = a
	f.lastZone = z
	f.lastHost = host
	f.lastTunnel = tunnelID
	if f.dnsErr != nil {
		return "", f.dnsErr
	}
	return "dns-record-1", nil
}

func (f *fakeClient) DeleteDNSRoute(_ context.Context, z Zone, recordID string) error {
	f.lastZone = z
	f.deletedRoutes = append(f.deletedRoutes, recordID)
	return nil
}

func (f *fakeClient) DeleteTunnel(_ context.Context, a Account, tunnelID string) error {
	f.lastAccount = a
	f.lastTunnel = tunnelID
	f.deletedTunnels = append(f.deletedTunnels, tunnelID)
	return nil
}

// TestFakeClientImplementsInterface is a compile-time assertion that the fake
// satisfies the Client contract, and exercises its call recording.
func TestFakeClientImplementsInterface(t *testing.T) {
	var _ Client = (*fakeClient)(nil)
	f := &fakeClient{accounts: []Account{{ID: "acct-1", Name: "acme"}}}
	ctx := context.Background()
	require.NoError(t, f.VerifyToken(ctx))
	accs, err := f.ListAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, accs, 1)
	rec, err := f.CreateTunnel(ctx, Account{ID: "acct-1"}, "pin")
	require.NoError(t, err)
	require.Empty(t, rec.ID) // canned default is zero value
}

// TestNewTunnelSecret ensures the generated secret is non-empty and decodes to
// at least 32 bytes (the tunnel create API requirement).
func TestNewTunnelSecret(t *testing.T) {
	s, err := NewTunnelSecret()
	require.NoError(t, err)
	require.NotEmpty(t, s)
	raw, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)
	require.Len(t, raw, 32)
}
