package mcp

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"
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
	o := &OOBRestore{runner: runner, html: html}
	o.core = *newHandoff("restore", o, ttl)
	return o
}

// SetBaseURL sets the externally reachable base URL used to build restore URLs.
func (o *OOBRestore) SetBaseURL(baseURL string) {
	o.core.SetBaseURL(baseURL)
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
// mnemonic and consume the token (single use).
func (o *OOBRestore) consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) (consumed bool) {
	payload, _ := item.payload.(*restorePayload)

	// Single-use: always consume the token so a re-POST or concurrent POST
	// cannot run the restore twice against the same profile. The core removes
	// the token after consumePOST returns true.
	defer func() { consumed = true }()

	if o.runner == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Restore is not configured for this server.\n"))
		return
	}

	mnemonic := strings.TrimSpace(r.FormValue("mnemonic"))
	if mnemonic == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The recovery phrase is required.\n"))
		return
	}

	// Run the restore. It may block on the Sia browser approval.
	vaultID, err := o.runner.RunRestore(r.Context(), payload.profile, mnemonic)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Restore failed: " + err.Error() + "\n"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("<html><body><h2>Vault restored</h2><p>Profile <code>" +
		htmlEscape(payload.profile) + "</code> is ready (vault ID " + htmlEscape(vaultID) + ").</p></body></html>"))
	return
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// count returns the number of pending, unexpired restore tokens.
func (o *OOBRestore) count() int {
	return o.core.count()
}

// setNow overrides the clock used for expiry (test seam).
func (o *OOBRestore) setNow(f func() time.Time) {
	o.core.setNow(f)
}

// vaultRestoreAgentOutput is the JSON shape `vault restore --agent` prints (see
// pkg/cli vaultRestoreApprovalResponse). Defined locally to avoid importing the
// CLI package.
type vaultRestoreAgentOutput struct {
	Profile  string `json:"profile"`
	NextStep string `json:"next_step"`
}

// restoreOOBEnabled reports whether an OOB restore coordinator is wired and
// usable (a non-nil coordinator makes the vault-restore browser hand-off
// reachable). Used to bypass the stdin-input gate for the agent-safe restore
// path.
func restoreOOBEnabled(o *OOBRestore) bool {
	return o != nil
}

// attachRestoreURL post-processes the stdout of `vault restore --agent`. When
// an OOB restore coordinator is wired, it mints a one-time /restore/<token> URL
// for the targeted profile so the human can supply the recovery seed in a
// browser instead of re-running with --seed-stdin. Returns the URL, or empty
// when there is nothing to attach.
func attachRestoreURL(stdout string, requestName string, oobRestore *OOBRestore) string {
	if oobRestore == nil {
		return ""
	}
	if requestName != "pinner_vault_restore" {
		return ""
	}
	var out vaultRestoreAgentOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil || out.Profile == "" {
		return ""
	}
	return oobRestore.Register(out.Profile)
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
