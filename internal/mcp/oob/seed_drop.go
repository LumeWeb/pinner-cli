package oob

import (
	"context"
	"net/http"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
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
// shared transport mux via RegisterHandlers (baseURL set); over stdio there is
// no transport server, so Start() spins up a loopback listener on a random
// port (the same pattern OutOfBandLogin uses) and Register falls back to the
// loopback URL. Either way the seed never transits the MCP/LLM channel, which
// is the whole point.
type SeedDrop struct {
	core handoff.Endpoint
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
	s.core = *handoff.New("seed", s, ttl)
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
// recovery mnemonic for a profile. It returns the full URL the human opens.
// It is a confirm-direction hand-off: GET renders the seed plus a
// CSRF-guarded confirmation form; only the explicit human confirmation POST
// consumes the token and destroys the at-rest recovery copy.
func (s *SeedDrop) Register(profile, mnemonic string) string {
	return s.core.Mint(&seedPayload{profile: profile, mnemonic: mnemonic})
}

// Stop shuts down the loopback listener, if any.
func (s *SeedDrop) Stop(ctx context.Context) {
	s.core.Stop(ctx)
}

// registerHandlers mounts the GET-only seed retrieval route on the shared mux.
func (s *SeedDrop) RegisterHandlers(mux *http.ServeMux) {
	s.core.RegisterHandlers(mux)
}

// consumeOnGET reports that a GET does NOT consume the seed drop. GET only
// renders the seed and a confirmation form; a failed transport, prefetch, or
// link-expander must not consume the token or destroy the at-rest recovery
// copy, or the human would be stranded with a 410 and a dead vault. Only the
// explicit confirmation POST consumes it.
func (s *SeedDrop) ConsumeOnGET() bool { return false }

// renderGET implements handoffHandler: show the seed plus a confirmation form.
//
// Delivery is NOT confirmed at the HTTP layer — ResponseWriter.Write buffers
// into a bufio.Writer and returns success even if the client never received
// the bytes (a closed connection, a prefetch, a link-expander). So GET neither
// consumes the token nor destroys the at-rest seed. Instead the page renders
// the seed (a bearer secret, no new exposure) and asks the human to click "I
// have stored it" — the confirmation POST is what marks the seed retrieved and
// deletes the only at-rest recovery copy. If the transport fails or the human
// just reopens the URL, GET re-renders and they can still see the seed.
func (s *SeedDrop) RenderGET(w http.ResponseWriter, r *http.Request, token string, item *handoff.Item) {
	Payload, _ := item.Payload.(*seedPayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	action := "/" + s.core.Prefix() + "/" + token
	_ = seedDropPage(Payload.profile, Payload.mnemonic, action).Render(r.Context(), w)
}

// consumePOST implements handoffHandler: the human's explicit confirmation
// that they stored the seed. This is the single point where the seed drop is
// consumed AND the at-rest recovery copy is destroyed — the destructive action
// is gated behind the CSRF origin check in handle() and a deliberate human
// click, never a fire-and-forget GET.
func (s *SeedDrop) ConsumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoff.Item) bool {
	Payload, _ := item.Payload.(*seedPayload)

	// Only on explicit human confirmation do we destroy the at-rest recovery
	// copy. Branch on whether the copy is actually gone — the security-relevant
	// fact. A marker-clear bookkeeping error (gone==true, err!=nil) must not be
	// reported as a removal failure.
	gone, err := vault.NewProvisioner().MarkSeedRetrieved(Payload.profile)
	if !gone {
		// The at-rest copy could not be removed. Report this truthfully: telling
		// the human it was removed while the plaintext mnemonic lingers on disk
		// would leave the vault's only recovery credential silently exposed.
		// Keep the token live so they can retry (and an operator can investigate)
		// rather than consuming it and stranding them with no way back.
		s.core.Logf().Error("failed to remove at-rest recovery copy", zap.String("profile", Payload.profile), zap.Error(err))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusInternalServerError)
		_ = seedDropRemovalFailedPage().Render(r.Context(), w)
		return false // do not consume the token while the at-rest copy survives
	}
	if err != nil {
		// The copy is gone; only the KeepSeed marker-clear failed to persist.
		s.core.Logf().Error("seed removed but keep-seed marker-clear failed", zap.String("profile", Payload.profile), zap.Error(err))
	}
	s.core.Logf().Info("seed drop confirmed; at-rest recovery copy removed", zap.String("profile", Payload.profile))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = seedDropConfirmedPage().Render(r.Context(), w)
	return true
}

// count returns the number of currently registered, unexpired drops.
func (s *SeedDrop) Count() int {
	return s.core.Count()
}

// setNow overrides the clock used for expiry (test seam).
func (s *SeedDrop) SetNow(f func() time.Time) {
	s.core.SetNow(f)
}
