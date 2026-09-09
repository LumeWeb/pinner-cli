//go:build !no_tunnel

package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tunneler "go.lumeweb.com/tunneler"
	cloudflare "go.lumeweb.com/tunneler/cloudflare"
)

// tunnelStateFileName is the JSON file the tunnel installer / service install
// wizard persist a provisioned tunnel to. The embedded cloudflared runtime
// reads it at Start time to build the in-process named tunnel's credentials.
const tunnelStateFileName = "tunnel-state.json"

// CloudflareTunnelState is the persisted, tunnel-scoped credential set for a
// provisioned Cloudflare named tunnel. It is exactly what a cloudflared
// "credentials file" needs (AccountTag/TunnelID/TunnelSecret) plus the public
// hostname and the scoped run token. It is the "scoped api key for the tunnel
// itself": holding it authorizes running exactly this one tunnel.
type CloudflareTunnelState struct {
	Provider   TunnelProvider `json:"provider"`
	AccountID  string         `json:"account_id"` // credentials AccountTag
	TunnelID   string         `json:"tunnel_id"`
	TunnelName string         `json:"tunnel_name"`
	// Secret and Token are credentials (the tunnel credentials secret and the
	// scoped run token). They are populated ONLY at runtime from the Cloudflare
	// API response / tunnel provisioning, never from source and never from
	// literals. Code paths that construct a state must not hard-code these
	// values; tests must likewise use fixtures that are clearly not real
	// credentials.
	Secret   string `json:"secret"`
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
	// ZoneID and DNSRecordID capture the proxied DNS route created for the
	// hostname so a later failure (e.g. an env-file write) can roll the route
	// back alongside the tunnel instead of orphaning the CNAME.
	ZoneID      string `json:"zone_id"`
	DNSRecordID string `json:"dns_record_id"`
}

// TunnelStatePath returns the per-user path to the tunnel state file, under
// the same config directory used for the MCP service env file. It is a package
// variable so tests can redirect it to a temp dir. The default preserves the
// pinner layout (<UserConfigDir>/pinner/tunnel-state.json): the library
// genericized the parent directory, so the shim pins it back, and it also
// installs this function into the library so delegated Load/Save (and the
// cloudflared runtime) read the same location as this package's callers.
var TunnelStatePath = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "pinner", tunnelStateFileName), nil
}

// init wires the library's cloudflare package onto this package's
// TunnelStatePath var: every library-side tunnel-state lookup (including the
// embedded cloudflared daemon's state loading) redirects through it, so
// tests/callers reassigning tunnel.TunnelStatePath keep full effect.
func init() {
	cloudflare.TunnelStatePath = func() (string, error) {
		return TunnelStatePath()
	}
}

// toLibrary converts the pinner tunnel state into the library's state shape.
// The fields are identical in name and JSON tags; only the Provider type
// differs (pinner TunnelProvider vs. tunneler.TunnelProvider), so it is
// converted by string.
func (s *CloudflareTunnelState) toLibrary() *cloudflare.CloudflareTunnelState {
	if s == nil {
		return nil
	}
	return &cloudflare.CloudflareTunnelState{
		Provider:    tunneler.TunnelProvider(string(s.Provider)),
		AccountID:   s.AccountID,
		TunnelID:    s.TunnelID,
		TunnelName:  s.TunnelName,
		Secret:      s.Secret,
		Token:       s.Token,
		Hostname:    s.Hostname,
		ZoneID:      s.ZoneID,
		DNSRecordID: s.DNSRecordID,
	}
}

// cloudflareStateFrom converts a library state back into its pinner shape.
func cloudflareStateFrom(s *cloudflare.CloudflareTunnelState) *CloudflareTunnelState {
	if s == nil {
		return nil
	}
	return &CloudflareTunnelState{
		Provider:    TunnelProvider(string(s.Provider)),
		AccountID:   s.AccountID,
		TunnelID:    s.TunnelID,
		TunnelName:  s.TunnelName,
		Secret:      s.Secret,
		Token:       s.Token,
		Hostname:    s.Hostname,
		ZoneID:      s.ZoneID,
		DNSRecordID: s.DNSRecordID,
	}
}

// LoadCloudflareTunnelState loads the provisioned tunnel state, returning
// os.ErrNotExist if none has been provisioned. It delegates the file IO to the
// extracted tunneler cloudflare package (whose path resolution redirects to
// this package's TunnelStatePath) and converts the result to the pinner shape.
func LoadCloudflareTunnelState() (*CloudflareTunnelState, error) {
	st, err := cloudflare.LoadCloudflareTunnelState()
	if err != nil {
		return nil, err
	}
	return cloudflareStateFrom(st), nil
}

// parseTunnelState unmarshals a tunnel state document, yielding a clear error
// on malformed input.
func parseTunnelState(b []byte) (*CloudflareTunnelState, error) {
	var s CloudflareTunnelState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse tunnel state: %w", err)
	}
	return &s, nil
}

// SaveCloudflareTunnelState persists the tunnel state as a private (0600)
// file. The secret/token are first-class secrets and must not be world-readable.
// It delegates the file IO to the extracted tunneler cloudflare package (whose
// path resolution redirects to this package's TunnelStatePath).
func SaveCloudflareTunnelState(s *CloudflareTunnelState) error {
	if s == nil {
		return fmt.Errorf("nil tunnel state")
	}
	return cloudflare.SaveCloudflareTunnelState(s.toLibrary())
}
