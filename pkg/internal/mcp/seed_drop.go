package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
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
	mu       sync.Mutex
	drops    map[string]*seedDropItem
	ttl      time.Duration
	now      func() time.Time
	loopback loopbackServer
}

type seedDropItem struct {
	profile   string
	mnemonic  string
	expiresAt time.Time
}

// DefaultSeedDropTTL is how long a seed drop URL stays valid before it expires.
const DefaultSeedDropTTL = 30 * time.Minute

// NewSeedDrop creates a SeedDrop coordinator.
func NewSeedDrop(ttl time.Duration) *SeedDrop {
	if ttl <= 0 {
		ttl = DefaultSeedDropTTL
	}
	return &SeedDrop{
		drops: make(map[string]*seedDropItem),
		ttl:   ttl,
		now:   time.Now,
	}
}

// SetBaseURL sets the externally reachable base URL used to build seed URLs
// (the same value pointed at the OOB login coordinator).
func (s *SeedDrop) SetBaseURL(baseURL string) {
	s.loopback.SetBaseURL(baseURL)
}

// Register mints a one-time, expiring, single-use URL carrying the given
// recovery mnemonic for a profile. It returns the full URL the human opens.
// It ensures the loopback listener is running so the URL is always reachable,
// whether in HTTP mode (handlers on the shared mux, base URL set) or stdio
// mode (loopback listener).
func (s *SeedDrop) Register(profile, mnemonic string) string {
	if err := s.loopback.ensureLoopback(s.registerHandlers); err != nil {
		// If we cannot spin up a listener there is no URL to hand over; return
		// empty so the caller keeps the plaintext-path fallback.
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token := randomID()
	s.drops[token] = &seedDropItem{
		profile:   profile,
		mnemonic:  mnemonic,
		expiresAt: s.now().Add(s.ttl),
	}
	return s.loopback.urlLocked("seed", token)
}

// Stop shuts down the loopback listener, if any.
func (s *SeedDrop) Stop(ctx context.Context) {
	s.loopback.Stop(ctx)
}

// registerHandlers mounts the GET-only seed retrieval route on the shared mux,
// mounted outside the bearer-token middleware (the human must open it in a
// browser). The unguessable token in the path is the access control.
func (s *SeedDrop) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/seed/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, "/seed/")
		s.serve(w, token)
	})
}

// serve fulfills a seed drop request exactly once, then invalidates it. It is
// rate/size-bounded and expires drops whose TTL has elapsed so the map doesn't
// grow unbounded over a long-running process.
func (s *SeedDrop) serve(w http.ResponseWriter, token string) {
	s.mu.Lock()
	item, ok := s.drops[token]
	if !ok {
		s.mu.Unlock()
		http.NotFound(w, nil)
		return
	}
	if s.now().After(item.expiresAt) {
		delete(s.drops, token)
		s.mu.Unlock()
		http.NotFound(w, nil)
		return
	}
	// Single use: consume the drop before rendering so a concurrent or
	// repeated GET cannot read it twice.
	delete(s.drops, token)
	profile := item.profile
	seed := item.mnemonic
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Vault recovery seed for profile " + profile + " (shown once):\n\n" + seed + "\n\nThis is your only path back into the vault. Store it securely. This page is no longer available after this view.\n"))
}

// count returns the number of currently registered, unexpired drops. Used in
// tests and possible instrumentation.
func (s *SeedDrop) count() int {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for token, it := range s.drops {
		if now.After(it.expiresAt) {
			delete(s.drops, token)
			continue
		}
		n++
	}
	return n
}

// vaultCreateAgentOutput is the JSON shape `vault create --agent` prints (see
// pkg/cli vaultCreateApprovalResponse). Defined locally to avoid importing the
// CLI package, which the MCP package must not depend on.
type vaultCreateAgentOutput struct {
	Profile  string `json:"profile"`
	SeedPath string `json:"seed_path"`
	NextStep string `json:"next_step"`
}

// attachSeedDrop post-processes the stdout of a successfully invoked CLI
// command. When the command is `vault create --agent`, a SeedDrop is wired,
// and the output carries a seed_path, it reads the recovery mnemonic from that
// host file and mints a one-time browser URL, returning a result that includes
// both the path (unchanged, for host-side scripts) and the seed_url (for the
// human) without the mnemonic itself ever crossing the MCP channel. In any
// other case it returns the raw output unchanged.
func attachSeedDrop(stdout string, requestName string, seedDrop *SeedDrop) (string, map[string]any) {
	if seedDrop == nil {
		return stdout, nil
	}
	// Only vault create produces a seed-bearing response.
	if requestName != "pinner_vault_create" {
		return stdout, nil
	}
	var out vaultCreateAgentOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil || out.SeedPath == "" {
		return stdout, nil
	}
	// Read the mnemonic from the host 0600 seed file. This is host-side access
	// by the MCP server itself (which owns the filesystem), not the agent.
	data, err := os.ReadFile(out.SeedPath)
	if err != nil {
		// If the file is unreadable we still pass the original output through;
		// the plaintext path remains the fallback.
		return stdout, nil
	}
	mnemonic := strings.TrimSpace(string(data))
	if mnemonic == "" {
		return stdout, nil
	}
	url := seedDrop.Register(out.Profile, mnemonic)
	extra := map[string]any{
		"seed_url": url,
		"profile":  out.Profile,
		"hint":     "Open seed_url in a browser to view the recovery seed once. The mnemonic is not placed on the MCP channel.",
	}
	return stdout, extra
}
