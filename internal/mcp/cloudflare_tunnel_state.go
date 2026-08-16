package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tunnelStateFileName is the JSON file the tunnel installer / service install
// wizard persist a provisioned tunnel to. The cloudflared runtime reads it at
// Start time to build the credentials file and config.yml (which reference the
// actual bound local origin port).
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

// tunnelStatePath returns the per-user path to the tunnel state file, under
// the same config directory used for the MCP service env file. It is a package
// variable so tests can redirect it to a temp dir.
var tunnelStatePath = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "pinner", tunnelStateFileName), nil
}

// LoadCloudflareTunnelState loads the provisioned tunnel state, returning
// os.ErrNotExist if none has been provisioned.
func LoadCloudflareTunnelState() (*CloudflareTunnelState, error) {
	path, err := tunnelStatePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTunnelState(b)
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
func SaveCloudflareTunnelState(s *CloudflareTunnelState) error {
	if s == nil {
		return fmt.Errorf("nil tunnel state")
	}
	path, err := tunnelStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create pinner config dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write tunnel state %q: %w", path, err)
	}
	return nil
}

// credentialsJSON returns the cloudflared "credentials file" JSON for a
// locally-managed tunnel. This is what cloudflared expects at
// credentials-file in config.yml.
func (s *CloudflareTunnelState) credentialsJSON() []byte {
	// Field names are fixed by cloudflared; do not rename.
	return []byte(fmt.Sprintf(
		`{"AccountTag":%q,"TunnelID":%q,"TunnelName":%q,"TunnelSecret":%q}`,
		s.AccountID, s.TunnelID, s.TunnelName, s.Secret,
	))
}

// configYAML returns a cloudflared config.yml for a locally-managed tunnel that
// routes hostname to the given local origin (host:port). The origin port is the
// MCP server's actual bound port, so it is always correct regardless of which
// ephemeral port the OS assigned. The hostname is normalized to its bare form
// (scheme stripped) because cloudflared's ingress hosts are bare hostnames, not
// URLs. Every interpolated value is emitted through %q so a hostile hostname or
// other value cannot break out of the YAML scalar and inject ingress rules.
func (s *CloudflareTunnelState) configYAML(origin string) ([]byte, error) {
	credsPath, err := s.credentialsFilePath()
	if err != nil {
		return nil, err
	}
	host := bareHostname(s.Hostname)
	if !validIngressHostname(host) {
		return nil, fmt.Errorf("invalid tunnel hostname %q: must be a bare DNS hostname (letters, digits, dots, hyphens)", host)
	}
	// indentation is significant in YAML; keep the template literal exact.
	return []byte(fmt.Sprintf(
		"tunnel: %q\ncredentials-file: %q\n\n"+
			"ingress:\n"+
			"  - hostname: %q\n    service: %q\n"+
			"  - service: http_status:404\n",
		s.TunnelID, credsPath, host, origin,
	)), nil
}

// validIngressHostname reports whether h is a safe bare hostname for the
// cloudflared ingress table. It rejects anything containing whitespace,
// control characters, or characters outside the DNS hostname set so a
// user-supplied --domain cannot smuggle YAML directives or extra ingress keys.
func validIngressHostname(h string) bool {
	if h == "" {
		return false
	}
	for _, r := range h {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// credentialsFilePath returns where the credentials file is written for the
// current provider. It propagates the os.UserConfigDir error rather than
// silently building a path at the filesystem root when resolution fails.
func (s *CloudflareTunnelState) credentialsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "pinner", s.TunnelID+".json"), nil
}

// bareHostname strips a leading http(s):// scheme so hostname comparisons and
// cloudflared ingress hosts are always bare (e.g. "mcp.example.com"), never
// scheme-qualified URLs. It also strips a trailing path/hash fragment if a
// caller passed a full URL.
func bareHostname(h string) string {
	h = strings.TrimSpace(h)
	for _, p := range []string{"https://", "http://"} {
		if len(h) >= len(p) && strings.EqualFold(h[:len(p)], p) {
			h = h[len(p):]
			break
		}
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	return h
}
