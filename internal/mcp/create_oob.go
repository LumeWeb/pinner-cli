package mcp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OOBCreate provisions and activates a new vault from the browser, symmetric
// with OOBRestore and so the fresh recovery seed (and the Sia device approval)
// never transit the MCP/LLM channel.
//
// vault create --agent mints a one-time /create/<token> page. The human opens
// it, approves the Sia device connection in a browser (the page streams the
// approval link), and on approval the coordinator activates the vault and mints
// a one-time seeddrop for the freshly generated recovery seed, which the human
// then retrieves in another browser page. Create and restore are the same core
// flow; the only difference is seed origin (generated vs provided), so this
// coordinator mirrors OOBRestore except that it GENERATES a seed and delivers
// it (seed OUT) instead of accepting a human-entered one (seed IN).
//
// It is a collect-direction hand-off built on the shared handoffEndpoint core,
// which supplies the one-time/expiring URL, loopback-or-shared-mux bootstrap,
// and CSRF origin guard. Works over both stdio and HTTP/tunnel.
type OOBCreate struct {
	runner   CreateRunner
	seedDrop *SeedDrop
	core     handoffEndpoint

	// mu guards outcomes. outcomes records the terminal + seed-delivery state of
	// each create attempt so the resume continuation can distinguish a completed
	// create (vault active AND seed retrieved) from a failed or still-pending
	// one. Keyed by token.
	mu       sync.Mutex
	outcomes map[string]*createOutcome
}

// createOutcome records the live/terminal result of a claimed create attempt.
// succeeded is set when the vault is active (approval + registration done);
// err is set on a failed create. seedToken holds the one-time seeddrop token for
// the freshly generated seed, so the continuation can detect when the human has
// retrieved the seed. An unsettled record (succeeded false, err empty, no
// seedToken) means the create is still in progress (browser approval or
// registration outstanding).
type createOutcome struct {
	succeeded bool
	err       string
	seedToken string
	started   time.Time
}

// createPayload is the per-token context for a pending create.
type createPayload struct {
	profile string
}

// DefaultCreateTTL is how long an OOB create URL stays valid.
const DefaultCreateTTL = DefaultRestoreTTL

// NewOOBCreate creates an out-of-band create coordinator backed by a
// CreateRunner (implemented in internal/cli over the shared Provisioner.Create
// path) and a SeedDrop used to deliver the freshly generated seed.
func NewOOBCreate(runner CreateRunner, seedDrop *SeedDrop, ttl time.Duration) *OOBCreate {
	if ttl <= 0 {
		ttl = DefaultCreateTTL
	}
	c := &OOBCreate{runner: runner, seedDrop: seedDrop, outcomes: map[string]*createOutcome{}}
	c.core = *newHandoff("create", c, ttl)
	return c
}

// SetBaseURL sets the externally reachable base URL used to build create URLs.
func (c *OOBCreate) SetBaseURL(baseURL string) {
	c.core.SetBaseURL(baseURL)
}

// WithLogger sets the zap logger the create coordinator uses for lifecycle
// events.
func (c *OOBCreate) WithLogger(l *zap.Logger) *OOBCreate {
	c.core.WithLogger(l)
	return c
}

// registerHandlers mounts the create page + POST routes on the shared mux.
func (c *OOBCreate) registerHandlers(mux *http.ServeMux) {
	c.core.registerHandlers(mux)
}

// Register mints a one-time, expiring URL that creates and activates a vault
// for the given profile. Non-blocking: the create runs only once the human
// approves on the page.
func (c *OOBCreate) Register(profile string) string {
	return c.core.mint(&createPayload{profile: profile})
}

// Stop shuts down the loopback listener, if any.
func (c *OOBCreate) Stop(ctx context.Context) {
	c.core.Stop(ctx)
}

// consumeOnGET reports that a GET does NOT consume the create token (it is
// collected on POST; the page must be viewable/reloadable before submit).
func (c *OOBCreate) consumeOnGET() bool { return false }

// renderGET implements handoffHandler: show the one-time create page that starts
// the vault creation + Sia device approval.
func (c *OOBCreate) renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) {
	payload, _ := item.payload.(*createPayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, createPageGet(htmlEscape(payload.profile), htmlEscape(token)))
}

// consumePOST implements handoffHandler: run the create (generate seed + Sia
// approval + register + activate) and consume the token (single use). The
// response is streamed so the Sia browser approval does not hang the human:
// onApproval writes + flushes the approval link before Create blocks, then on
// success the handler mints a one-time seeddrop for the fresh seed and streams
// that retrieval link.
func (c *OOBCreate) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) (consumed bool) {
	payload, _ := item.payload.(*createPayload)

	// Missing runner or seeddrop is a server misconfiguration: do NOT consume
	// the token (nothing ran) so the one-time URL stays valid.
	if c.runner == nil || c.seedDrop == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Vault create is not configured for this server.\n"))
		return
	}

	profile := payload.profile

	// Claim the token atomically BEFORE the blocking create (which can wait
	// minutes on a Sia approval), so a concurrent or repeated POST is rejected
	// instead of issuing a second approval or registering a second device.
	if !c.core.claim(token) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "A vault create is already in progress or this link was already used.", http.StatusGone)
		return
	}
	consumed = true

	// Record the in-flight outcome so the resume continuation knows the claimed
	// token is still creating (not yet succeeded or failed). Reap stale terminal
	// outcomes here so an abandoned create (never resumed) does not accumulate.
	out := &createOutcome{started: time.Now()}
	c.mu.Lock()
	c.pruneOutcomesLocked(time.Now().Add(-DefaultCreateTTL))
	c.outcomes[token] = out
	c.mu.Unlock()
	settle := func(succeeded bool, errText, seedToken string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		out.succeeded = succeeded
		out.err = errText
		out.seedToken = seedToken
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	flusher, hasFlusher := w.(http.Flusher)
	stream := func(s string) {
		fmt.Fprint(w, s)
		if hasFlusher {
			flusher.Flush()
		}
	}

	stream(createPageStart(htmlEscape(profile)))

	// Run the create. It GENERATES a fresh seed and blocks waiting for the Sia
	// browser approval; onApproval streams the approval link here first.
	vaultID, seed, _, err := c.runner.RunCreate(r.Context(), profile, func(approvalURL string) {
		stream(createPageApproval(htmlEscape(profile), htmlEscape(approvalURL)))
	})
	if err != nil {
		settle(false, err.Error(), "")
		stream(createPageError(htmlEscape(err.Error())))
		stream(progressPageEnd())
		return
	}

	// Vault is active; deliver the fresh seed by minting a one-time seeddrop.
	seedURL := c.seedDrop.Register(profile, seed)
	seedToken := vaultTokenFromURL(seedURL)
	settle(true, "", seedToken)
	stream(createPageDone(htmlEscape(profile), htmlEscape(vaultID), htmlEscape(seedURL)))
	stream(progressPageEnd())
	return
}

func (c *OOBCreate) count() int {
	return c.core.count()
}

// setNow overrides the clock used for expiry (test seam).
func (c *OOBCreate) setNow(f func() time.Time) {
	c.core.setNow(f)
}

// tokenDone reports the create coordinator token's state for the resume
// continuation. Compared to restore, create has two sub-stages after the last
// POST-triggered state: the vault must be ACTIVE (approval + registration) AND
// the freshly generated seed must have been retrieved from its one-time
// seeddrop. done requires both. Everything else routes to pending/failed/expired
// exactly like restore so an unfinished create is never reported as done.
//
//	succeeded + seed retrieved        -> done
//	succeeded + seed not yet retrieved -> pending (activity: retrieve seed)
//	failed outcome                     -> failed (steer, never done)
//	live item                          -> pending (not approved yet)
//	handoffExpired                     -> expired
//	no outcome + spent                 -> absent/evicted (dead -> steer)
func (c *OOBCreate) tokenDone(token string) (done, failed, expired, pending bool) {
	if token == "" {
		return false, false, false, false
	}
	item, reason := c.core.resolve(token)
	if item != nil {
		return false, false, false, true // still live: nothing approved/received yet
	}
	if reason == handoffExpired {
		return false, false, true, false
	}
	c.mu.Lock()
	out, ok := c.outcomes[token]
	var succeeded bool
	var errText string
	var seedToken string
	if ok {
		succeeded, errText, seedToken = out.succeeded, out.err, out.seedToken
	}
	c.mu.Unlock()
	if !ok {
		c.pruneOutcomes()
		return false, false, false, false // spent tombstone evicted; dead -> steer
	}
	if errText != "" {
		return false, true, false, false // create failed; never report done
	}
	if !succeeded {
		return false, false, false, true // claimed but still running (approval outstanding)
	}
	// Vault is active. Await the seed retrieval from the one-time seeddrop.
	if c.seedDrop == nil {
		return true, false, false, false // no seeddrop wired; active is terminal
	}
	seedDone, seedExpired, seedPending := c.seedDrop.tokenDone(seedToken)
	if seedDone {
		return true, false, false, false
	}
	if seedExpired {
		// Seeddrop expired before retrieval: the vault is active but the human
		// lost the only seed delivery. Report done at the coordinator level would
		// be misleading; the active vault exists but the seed is unrecoverable.
		// Surface as expired so the agent steers a fresh create retry.
		return false, false, true, false
	}
	if seedPending {
		return false, false, false, true
	}
	return false, false, false, true
}

// forgetOutcome drops the outcome record for a token once a continuation has
// consumed its terminal result (done or failed).
func (c *OOBCreate) forgetOutcome(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.outcomes, token)
}

// pruneOutcomes drops terminal outcome records older than the create TTL.
func (c *OOBCreate) pruneOutcomes() {
	cutoff := time.Now().Add(-DefaultCreateTTL)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneOutcomesLocked(cutoff)
}

// pruneOutcomesLocked prunes terminal outcomes older than cutoff. Caller holds
// c.mu.
func (c *OOBCreate) pruneOutcomesLocked(cutoff time.Time) {
	for token, out := range c.outcomes {
		if (out.succeeded || out.err != "") && out.started.Before(cutoff) {
			delete(c.outcomes, token)
		}
	}
}

// progress page builders mirror OOBRestore's streaming contract (brand-neutral
// inline CSS; labelled id anchors for tests).

func createPageStart(profile string) string {
	return "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Create Pinner Vault</title><style>body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:520px;margin:3rem auto;padding:0 1rem;color:#1a1a1a;line-height:1.5}a{color:#0a66c2}footer{margin-top:2rem;font-size:.8rem;color:#999}</style></head><body><h1>Create Pinner Vault</h1><p>Profile: <strong>" + profile + "</strong></p><div id=\"status\">Preparing your new vault...</div>"
}

// createPageGet renders the initial create page: a one-time form whose POST
// starts the vault create + Sia device approval. Mirror of the restore form but
// with no mnemonic entry (the seed is generated, not provided).
func createPageGet(profile, token string) string {
	return `<!DOCTYPE html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Create Pinner Vault</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:520px;margin:3rem auto;padding:0 1rem;color:#1a1a1a;line-height:1.5}
h1{font-size:1.4rem;margin-bottom:.25rem}
p.dim{color:#666}
form{margin-top:1.5rem}
button{width:100%;padding:.7rem;margin-top:1rem;background:#111;color:#fff;border:0;border-radius:6px;font-size:1rem;cursor:pointer}
button:hover{background:#000}
.warn{background:#f0f7f7;border:1px solid #b8dcdc;border-radius:6px;padding:.75rem;font-size:.85rem;color:#0d2a2a}
footer{margin-top:2rem;font-size:.8rem;color:#999}
</style></head>
<body>
<h1>Create Pinner Vault</h1>
<p>Profile: <strong>` + profile + `</strong></p>
<p class="dim">Create a new vault on this machine and register this device. A fresh recovery seed is generated and shown exactly once for you to back up.</p>
<div class="warn">This creates a new vault identity. The MCP/agent channel never sees the recovery seed.</div>
<form method="post" action="/create/` + token + `">
<button type="submit">Create vault</button>
</form>
<footer>One-time page. It expires if unused.</footer>
</body></html>`
}

func createPageApproval(profile, approvalURL string) string {
	return "<p>To finish creating this vault, approve the device connection at:</p><p><a href=\"" + approvalURL + "\">" + approvalURL + "</a></p><p>Waiting for your approval...</p>"
}

func createPageDone(profile, vaultID, seedURL string) string {
	return "<h2>Vault created</h2><p>Profile <code>" + profile + "</code> is active (vault ID " + vaultID + ").</p><p id=\"seed-link\">Retrieve your one-time recovery seed: <a href=\"" + seedURL + "\">open seed page</a>.</p>"
}

func createPageError(errMsg string) string {
	return "<h2>Vault create failed</h2><p id=\"create-error\" role=\"alert\">" + errMsg + "</p>"
}
