package mcp

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OOBRestore completes a vault restore from a recovery mnemonic the human
// enters in a browser, so the seed never transits the MCP/LLM channel.
//
// vault restore --agent currently returns a next_step telling the human to
// re-run with --seed-stdin. In the OOB flow the human instead pastes the seed
// into a one-time /restore/<token> page; the form POST submits it to a
// host-side handler that runs the shared restore completion (restoreVault via
// RestoreRunner). The mnemonic travels human-browser-to-host over loopback or
// the transport mux, never through the agent channel.
//
// It is a collect-direction hand-off built on the shared handoffEndpoint core,
// which supplies the one-time/expiring URL, loopback-or-shared-mux bootstrap,
// and CSRF origin guard; this type supplies the form (GET) and restore (POST)
// behavior. Works over both stdio and HTTP/tunnel.
type OOBRestore struct {
	runner RestoreRunner
	core   handoffEndpoint
	// html holds the parsed restore form template.
	html *template.Template

	// mu guards outcomes. outcomes records the terminal outcome of each
	// restore attempt so the resume continuation can distinguish a succeeded
	// restore (report done) from a failed one (steer the agent), rather than
	// treating every claimed/spent token as restored. Keyed by token.
	mu       sync.Mutex
	outcomes map[string]*restoreOutcome
}

// restoreOutcome records the live/terminal result of a claimed restore attempt.
// The record is created at claim time (restore running) and settled after
// RunRestore: succeeded on success, or failed with the error text. An unsettled
// record (succeeded still false, err still empty) means the restore is still in
// progress (the browser approval is outstanding).
type restoreOutcome struct {
	succeeded bool
	err       string
	started   time.Time
}

// restorePayload is the per-token context for a pending restore.
type restorePayload struct {
	profile string
}

// DefaultRestoreTTL is how long an OOB restore URL stays valid.
const DefaultRestoreTTL = 30 * time.Minute

// NewOOBRestore creates an out-of-band restore coordinator backed by a
// RestoreRunner (implemented in pkg/cli over the shared restoreVault code).
func NewOOBRestore(runner RestoreRunner, ttl time.Duration) *OOBRestore {
	if ttl <= 0 {
		ttl = DefaultRestoreTTL
	}
	html := template.Must(template.New("restore").Parse(restorePageHTML))
	o := &OOBRestore{runner: runner, html: html, outcomes: map[string]*restoreOutcome{}}
	o.core = *newHandoff("restore", o, ttl)
	return o
}

// SetBaseURL sets the externally reachable base URL used to build restore URLs.
func (o *OOBRestore) SetBaseURL(baseURL string) {
	o.core.SetBaseURL(baseURL)
}

// WithLogger sets the zap logger the restore coordinator uses for lifecycle
// events.
func (o *OOBRestore) WithLogger(l *zap.Logger) *OOBRestore {
	o.core.WithLogger(l)
	return o
}

// registerHandlers mounts the restore page + POST routes on the shared mux.
func (o *OOBRestore) registerHandlers(mux *http.ServeMux) {
	o.core.registerHandlers(mux)
}

// Register mints a one-time, expiring URL that completes a restore for the
// given profile. Non-blocking: the restore runs only once the human submits
// the form.
func (o *OOBRestore) Register(profile string) string {
	return o.core.mint(&restorePayload{profile: profile})
}

// Stop shuts down the loopback listener, if any.
func (o *OOBRestore) Stop(ctx context.Context) {
	o.core.Stop(ctx)
}

// consumeOnGET reports that a GET does NOT consume the restore token (it is
// collected on POST; the form must be viewable/reloadable before submit).
func (o *OOBRestore) consumeOnGET() bool { return false }

// renderGET implements handoffHandler: show the one-time mnemonic entry form.
func (o *OOBRestore) renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) {
	payload, _ := item.payload.(*restorePayload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = o.html.Execute(w, map[string]any{
		"Profile": payload.profile,
		"Token":   token,
	})
}

// consumePOST implements handoffHandler: run the restore with the submitted
// mnemonic and consume the token (single use). The response is streamed so a
// fresh-device Sia browser approval does not hang the human: the OOB form is a
// standard form-POST navigation, so the browser renders the response
// incrementally, and RunRestore's onApproval callback writes + flushes the
// approval URL to this page *before* WaitAndRegister blocks. The human sees the
// approval link immediately, approves, and the handler then renders the result.
func (o *OOBRestore) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) (consumed bool) {
	payload, _ := item.payload.(*restorePayload)

	// Validation failures (missing runner, empty mnemonic) do NOT consume the
	// token: no restore ran, so the one-time URL stays valid for the human to
	// retry. Only a genuine restore attempt claims it (single use), so a
	// re-POST or concurrent POST cannot run the restore twice against the same
	// profile.
	if o.runner == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Restore is not configured for this server.\n"))
		return
	}

	profile := payload.profile
	mnemonic := strings.TrimSpace(r.FormValue("mnemonic"))
	if mnemonic == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The recovery phrase is required.\n"))
		return
	}

	// Claim the token atomically BEFORE the blocking restore. The restore can
	// wait minutes on a Sia browser approval, and the token is only removed from
	// the pending set by the core after consumePOST returns; an atomic claim
	// here removes it immediately (under the store mutex) so a concurrent or
	// repeated POST during the approval window is rejected instead of issuing a
	// second browser approval or registering a second device for the same seed.
	if !o.core.claim(token) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "A restore is already in progress or this link was already used.", http.StatusGone)
		return
	}
	// The one-time token is now claimed (removed + marked spent) by this
	// attempt, so the handler's return value truthfully reports it was
	// consumed. The core's `remove` on this return is an idempotent no-op since
	// claim already performed the deletion.
	consumed = true

	// Record the in-flight outcome so the resume continuation knows the claimed
	// token is still restoring (not yet succeeded or failed). Sweep stale
	// terminal outcomes here so an abandoned restore (never resumed) does not
	// accumulate: each new restore reaps terminal records older than the TTL.
	out := &restoreOutcome{started: time.Now()}
	o.mu.Lock()
	o.pruneOutcomesLocked(time.Now().Add(-DefaultRestoreTTL))
	o.outcomes[token] = out
	o.mu.Unlock()
	settle := func(succeeded bool, errText string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		out.succeeded = succeeded
		out.err = errText
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

	progress := progressPageStart(htmlEscape(profile))
	stream(progress)

	// Run the restore. On a fresh device it blocks waiting for the Sia browser
	// approval; onApproval streams the approval link to this page so the human
	// is told where to approve rather than hanging silently. The approval URL
	// arrives before WaitAndRegister blocks, so the link is rendered first.
	vaultID, err := o.runner.RunRestore(r.Context(), profile, mnemonic, func(approvalURL string) {
		stream(progressPageApproval(htmlEscape(profile), htmlEscape(approvalURL)))
	})
	if err != nil {
		settle(false, err.Error())
		// The restore failed after the page was already streamed (the approval
		// link / progress was written before the outcome was known), so the
		// HTTP status is already committed as 200; a 5xx cannot be retroactively
		// applied. To keep the failure unambiguous for humans and automated
		// consumers alike, render a distinct error banner (no-store prevents
		// caching) and escape the error text, which may embed mnemonic-derived
		// content, so it cannot be reflected as executable markup.
		stream(progressPageError(htmlEscape(err.Error())))
		stream(progressPageEnd())
		return
	}
	stream(progressPageDone(htmlEscape(profile), htmlEscape(vaultID)))
	stream(progressPageEnd())
	settle(true, "")
	return
}

func (o *OOBRestore) count() int {
	return o.core.count()
}

// setNow overrides the clock used for expiry (test seam).
func (o *OOBRestore) setNow(f func() time.Time) {
	o.core.setNow(f)
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

const restorePageHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Restore Pinner Vault</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:520px;margin:3rem auto;padding:0 1rem;color:#1a1a1a;line-height:1.5}
h1{font-size:1.4rem;margin-bottom:.25rem}
p.dim{color:#666}
form{margin-top:1.5rem}
textarea{width:100%;min-height:120px;font-family:ui-monospace,Menlo,Consolas,monospace;padding:.6rem;border:1px solid #ccc;border-radius:6px;font-size:14px}
button{width:100%;padding:.7rem;margin-top:1rem;background:#111;color:#fff;border:0;border-radius:6px;font-size:1rem;cursor:pointer}
button:hover{background:#000}
.warn{background:#fff7ed;border:1px solid #fed7aa;border-radius:6px;padding:.75rem;font-size:.85rem;color:#7c2d12}
footer{margin-top:2rem;font-size:.8rem;color:#999}
</style></head>
<body>
<h1>Restore Pinner Vault</h1>
<p>Profile: <strong>{{.Profile}}</strong></p>
<p class="dim">Enter your recovery phrase to restore this vault. It is sent from your browser directly to the vault process on this machine and is used once.</p>
<div class="warn">This recovery phrase is the only way back into the vault. Enter it only on a trusted device. The MCP/agent channel never sees it.</div>
<form method="post" action="/restore/{{.Token}}">
<label for="mnemonic">Recovery phrase</label><br>
<textarea name="mnemonic" id="mnemonic" required autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="word1 word2 word3 ..."></textarea>
<button type="submit">Restore vault</button>
</form>
<footer>One-time page. It expires if unused.</footer>
</body></html>`

func progressPageStart(profile string) string {
	return "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Restoring Pinner Vault</title><style>body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:520px;margin:3rem auto;padding:0 1rem;color:#1a1a1a;line-height:1.5}a{color:#0a66c2}footer{margin-top:2rem;font-size:.8rem;color:#999}</style></head><body><h1>Restoring Pinner Vault</h1><p>Profile: <strong>" + profile + "</strong></p><div id=\"status\">Starting restore...</div>"
}

func progressPageApproval(profile, approvalURL string) string {
	return "<p>To finish restoring this vault, approve the device connection at:</p><p><a href=\"" + approvalURL + "\">" + approvalURL + "</a></p><p>Waiting for your approval...</p>"
}

func progressPageDone(profile, vaultID string) string {
	return "<h2>Vault restored</h2><p>Profile <code>" + profile + "</code> is ready (vault ID " + vaultID + ").</p>"
}

func progressPageError(errMsg string) string {
	return "<h2>Restore failed</h2><p id=\"restore-error\" role=\"alert\">" + errMsg + "</p>"
}

func progressPageEnd() string {
	return "<footer>This page reflects the on-host restore state. The recovery phrase never leaves your browser.</footer></body></html>"
}
