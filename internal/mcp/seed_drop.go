package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
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
// recovery mnemonic for a profile. It returns the full URL the human opens.
// It is a confirm-direction hand-off: GET renders the seed plus a
// CSRF-guarded confirmation form; only the explicit human confirmation POST
// consumes the token and destroys the at-rest recovery copy.
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

// consumeOnGET reports that a GET does NOT consume the seed drop. GET only
// renders the seed and a confirmation form; a failed transport, prefetch, or
// link-expander must not consume the token or destroy the at-rest recovery
// copy, or the human would be stranded with a 410 and a dead vault. Only the
// explicit confirmation POST consumes it.
func (s *SeedDrop) consumeOnGET() bool { return false }

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
func (s *SeedDrop) renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) {
	payload, _ := item.payload.(*seedPayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	action := "/" + s.core.prefix + "/" + token
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8">
<title>Pinner vault recovery seed</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:2rem auto;padding:0 1rem;line-height:1.5}
code{display:block;white-space:pre-wrap;background:#f4f4f4;border:1px solid #ddd;padding:1rem;border-radius:6px;font-size:1.1rem}
.warn{color:#9a2f2f;font-weight:600}
button{font-size:1rem;padding:.6rem 1.2rem;border-radius:6px;border:1px solid #999;background:#fff;cursor:pointer}
</style></head><body>
<h1>Vault recovery seed</h1>
<p>Profile: <strong>%s</strong></p>
<p class="warn">This is the only way back into this vault. Write it down and store it somewhere safe. Do not share it.</p>
<code>%s</code>
<p>Only click <em>I have stored my recovery seed</em> once you have safely saved it. After you confirm, this link is invalidated and the on-disk copy is removed.</p>
<form method="post" action="%s">
<button type="submit">I have stored my recovery seed</button>
</form>
</body></html>`, htmlEscape(payload.profile), htmlEscape(payload.mnemonic), htmlEscape(action))
}

// consumePOST implements handoffHandler: the human's explicit confirmation
// that they stored the seed. This is the single point where the seed drop is
// consumed AND the at-rest recovery copy is destroyed — the destructive action
// is gated behind the CSRF origin check in handle() and a deliberate human
// click, never a fire-and-forget GET.
func (s *SeedDrop) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) bool {
	payload, _ := item.payload.(*seedPayload)

	// Only now — on explicit human confirmation — clear the profile's KeepSeed
	// marker and remove the at-rest copy. If the human never confirms, the kept
	// seed remains on disk as the durable recovery backup for a vault they were
	// creating fresh (no prior key), which is correct.
	if err := vault.NewProvisioner().MarkSeedRetrieved(payload.profile); err != nil {
		// The at-rest copy could not be removed. Report this truthfully: telling
		// the human it was removed while the plaintext mnemonic lingers on disk
		// would leave the vault's only recovery credential silently exposed.
		// Keep the token live so they can retry (and an operator can investigate)
		// rather than consuming it and stranding them with no way back.
		s.core.logf().Error("failed to remove at-rest recovery copy", zap.String("profile", payload.profile), zap.Error(err))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8">
<title>Recovery seed — removal failed</title></head><body>
<p>You have confirmed your recovery seed, but the on-disk copy could <strong>not</strong> be removed. Do not discard anything. Contact your administrator before proceeding — the recovery copy may still exist on this host and must be securely erased.</p>
</body></html>`)
		return false // do not consume the token while the at-rest copy survives
	}
	s.core.logf().Info("seed drop confirmed; at-rest recovery copy removed", zap.String("profile", payload.profile))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8">
<title>Recovery seed confirmed</title></head><body>
<p>Your recovery seed is confirmed. This link is no longer active and the on-disk copy has been removed. You can now close this window.</p>
</body></html>`)
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
