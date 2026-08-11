package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SeedDrop hands a vault recovery seed to a human over a secure, one-time,
// expiring URL without it ever transiting the MCP/LLM channel.
//
// vault create --agent writes the seed to a 0600 file on the host and returns
// that path (not the seed) so it stays out of logs and agent context. The MCP
// layer additionally mints a SeedDrop: the human opens
// <baseURL>/seed/<unguessable-token> in a browser, sees the seed exactly once,
// and then the URL is invalidated. This closes the "agent created a vault it
// can't finish / human can't find the seed" gap while preserving the security
// invariant that the plaintext master mnemonic never crosses the MCP layer.
//
// SeedDrop works over BOTH transports: over HTTP/tunnel it mounts /seed/ on the
// shared transport mux via registerHandlers (baseURL set); over stdio there is
// no transport server, so Start() spins up a loopback listener on a random
// port (the same pattern OutOfBandLogin uses) and Register falls back to the
// loopback URL. Either way the seed never transits the MCP/LLM channel, which
// is the whole point.
type SeedDrop struct {
	core handoffEndpoint
}

// seedPayload is the per-token context for a seed drop: the profile it belongs
// to and the recovery mnemonic to show exactly once.
type seedPayload struct {
	profile  string
	mnemonic string
}

// DefaultSeedDropTTL is how long a seed drop URL stays valid before it expires.
const DefaultSeedDropTTL = 30 * time.Minute

// NewSeedDrop creates a SeedDrop coordinator.
func NewSeedDrop(ttl time.Duration) *SeedDrop {
	if ttl <= 0 {
		ttl = DefaultSeedDropTTL
	}
	s := &SeedDrop{}
	s.core = *newHandoff("seed", s, ttl)
	return s
}

// SetBaseURL sets the externally reachable base URL used to build seed URLs
// (the same value pointed at the OOB login coordinator).
func (s *SeedDrop) SetBaseURL(baseURL string) {
	s.core.SetBaseURL(baseURL)
}

// WithLogger sets the zap logger the seed-drop coordinator uses for lifecycle
// events.
func (s *SeedDrop) WithLogger(l *zap.Logger) *SeedDrop {
	s.core.WithLogger(l)
	return s
}

// Register mints a one-time, expiring, single-use URL carrying the given
// recovery mnemonic for a profile. It returns the full URL the human opens. It
// is a read-direction hand-off: the seed is shown once on GET, then the token
// is consumed.
func (s *SeedDrop) Register(profile, mnemonic string) string {
	return s.core.mint(&seedPayload{profile: profile, mnemonic: mnemonic})
}

// Stop shuts down the loopback listener, if any.
func (s *SeedDrop) Stop(ctx context.Context) {
	s.core.Stop(ctx)
}

// registerHandlers mounts the GET-only seed retrieval route on the shared mux.
func (s *SeedDrop) registerHandlers(mux *http.ServeMux) {
	s.core.registerHandlers(mux)
}

// consumeOnGET reports that a GET consumes the seed drop (single-use display).
func (s *SeedDrop) consumeOnGET() bool { return true }

// renderGET implements handoffHandler: show the seed exactly once.
func (s *SeedDrop) renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) {
	payload, _ := item.payload.(*seedPayload)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Vault recovery seed for profile " + payload.profile + " (shown once):\n\n" + payload.mnemonic + "\n\nThis is your only path back into the vault. Store it securely. This page is no longer available after this view.\n"))
}

// consumePOST is unused for the GET-only seed drop; satisfy the interface.
func (s *SeedDrop) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) bool {
	w.Header().Set("Allow", "GET")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return true
}

// count returns the number of currently registered, unexpired drops.
func (s *SeedDrop) count() int {
	return s.core.count()
}

// setNow overrides the clock used for expiry (test seam).
func (s *SeedDrop) setNow(f func() time.Time) {
	s.core.setNow(f)
}

// attachSeedDrop post-processes the stdout of a successfully invoked CLI
// command that carries a vault-recovery-seed path. When the tool declares
// seed-drop behavior (Behavior.SeedDrop non-nil) and a SeedDrop coordinator is
// wired, it reads the mnemonic from the host seed file and mints a one-time
// browser URL, returning a result that includes both the path (unchanged) and
// the seed_url (for the human) without the mnemonic itself ever crossing the
// MCP channel. The spec's ProfileField/SeedPathField name the JSON output
// fields to read, so the behavior is data-driven rather than a hardcoded tool
// name. In any other case it returns the raw output unchanged.
func attachSeedDrop(stdout string, spec *SeedDropSpec, seedDrop *SeedDrop) (string, map[string]any) {
	if spec == nil || seedDrop == nil {
		return stdout, nil
	}
	// Decode the agent JSON output generically and read the fields named by
	// the spec, so the attach logic is decoupled from any specific tool.
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		return stdout, nil
	}
	profile, _ := out[spec.ProfileField].(string)
	seedPath, _ := out[spec.SeedPathField].(string)
	if profile == "" || seedPath == "" {
		return stdout, nil
	}
	// Read the mnemonic from the host 0600 seed file. This is host-side access
	// by the MCP server itself (which owns the filesystem), not the agent.
	data, err := os.ReadFile(seedPath)
	if err != nil {
		// If the file is unreadable we still pass the original output through;
		// the plaintext path remains the fallback.
		return stdout, nil
	}
	mnemonic := strings.TrimSpace(string(data))
	if mnemonic == "" {
		return stdout, nil
	}
	url := seedDrop.Register(profile, mnemonic)
	extra := map[string]any{
		"seed_url": url,
		"profile":  profile,
		"hint":     "Open seed_url in a browser to view the recovery seed once. The mnemonic is not placed on the MCP channel.",
	}
	return stdout, extra
}
