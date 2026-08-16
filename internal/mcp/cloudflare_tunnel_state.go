package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Secret     string         `json:"secret"`
	Token      string         `json:"token"`
	Hostname   string         `json:"hostname"`
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
// ephemeral port the OS assigned.
func (s *CloudflareTunnelState) configYAML(origin string) []byte {
	// indentation is significant in YAML; keep the template literal exact.
	return []byte(fmt.Sprintf(
		"tunnel: %s\ncredentials-file: %s\n\n"+
			"ingress:\n"+
			"  - hostname: %s\n    service: %s\n"+
			"  - service: http_status:404\n",
		s.TunnelID, s.credentialsFilePath(), s.Hostname, origin,
	))
}

// credentialsFilePath returns where the credentials file is written for the
// current provider.
func (s *CloudflareTunnelState) credentialsFilePath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "pinner", s.TunnelID+".json")
}
