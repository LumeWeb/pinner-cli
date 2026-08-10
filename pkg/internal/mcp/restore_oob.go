package mcp

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"sync"
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
// Like SeedDrop, it works over both transports: registerHandlers mounts on the
// shared HTTP/tunnel mux (base URL set); Start() spins up a loopback listener
// on a random port for stdio mode. CSRF/Origin checks mirror the OOB login
// page, and each token is single-use and expiring.
type OOBRestore struct {
	mu       sync.Mutex
	pending  map[string]*restoreItem
	runner   RestoreRunner
	ttl      time.Duration
	now      func() time.Time
	loopback loopbackServer
	// html holds the parsed restore form template.
	html *template.Template
}

type restoreItem struct {
	profile   string
	expiresAt time.Time
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
	return &OOBRestore{
		pending: make(map[string]*restoreItem),
		runner:  runner,
		ttl:     ttl,
		now:     time.Now,
		html:    html,
	}
}

// SetBaseURL sets the externally reachable base URL used to build restore URLs.
func (o *OOBRestore) SetBaseURL(baseURL string) {
	o.loopback.SetBaseURL(baseURL)
}

// registerHandlers mounts the restore page + POST routes on the shared mux.
func (o *OOBRestore) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/restore/", o.handle)
}

// Register mints a one-time, expiring URL that completes a restore for the
// given profile. It returns the full URL the human opens. Non-blocking: the
// restore runs only once the human submits the form. It ensures the loopback
// listener is running in stdio mode so the URL is always reachable.
func (o *OOBRestore) Register(profile string) string {
	if err := o.loopback.ensureLoopback(o.registerHandlers); err != nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	token := randomID()
	o.pending[token] = &restoreItem{profile: profile, expiresAt: o.now().Add(o.ttl)}
	return o.loopback.urlLocked("restore", token)
}

// Stop shuts down the loopback listener, if any.
func (o *OOBRestore) Stop(ctx context.Context) {
	o.loopback.Stop(ctx)
}

// handle serves the restore form (GET) and completes the restore (POST).
func (o *OOBRestore) handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/restore/")
	o.mu.Lock()
	item, ok := o.pending[token]
	if !ok || o.now().After(item.expiresAt) {
		if ok {
			delete(o.pending, token)
		}
		o.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	o.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		o.renderForm(w, token, item)
	case http.MethodPost:
		// CSRF guard: only the loopback/host origin is accepted (same as OOB
		// login). The mnemonic field is consumed once.
		if !sameOrigin(r, o.acceptedOrigins()...) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		o.submit(w, r, token, item)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// acceptedOrigins returns the origins allowed to POST the restore form.
func (o *OOBRestore) acceptedOrigins() []string {
	return o.loopback.acceptedOrigins()
}

// renderForm shows the one-time mnemonic entry page.
func (o *OOBRestore) renderForm(w http.ResponseWriter, token string, item *restoreItem) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = o.html.Execute(w, map[string]any{
		"Profile": item.profile,
		"Token":   token,
	})
}

// submit consumes the token and runs the restore with the submitted mnemonic.
func (o *OOBRestore) submit(w http.ResponseWriter, r *http.Request, token string, item *restoreItem) {
	// Single use: consume the token before running so a re-POST or concurrent
	// POST cannot run the restore twice against the same profile.
	o.mu.Lock()
	if _, ok := o.pending[token]; !ok {
		o.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	delete(o.pending, token)
	o.mu.Unlock()

	if o.runner == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Restore is not configured for this server.\n"))
		return
	}

	mnemonic := r.FormValue("mnemonic")
	mnemonic = strings.TrimSpace(mnemonic)
	if mnemonic == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The recovery phrase is required.\n"))
		return
	}

	// Run the restore. It may block on the Sia browser approval.
	vaultID, err := o.runner.RunRestore(r.Context(), item.profile, mnemonic)
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
		htmlEscape(item.profile) + "</code> is ready (vault ID " + htmlEscape(vaultID) + ").</p></body></html>"))
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// count returns the number of pending unexpired restore tokens (tests).
func (o *OOBRestore) count() int {
	now := o.now()
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for token, it := range o.pending {
		if now.After(it.expiresAt) {
			delete(o.pending, token)
			continue
		}
		n++
	}
	return n
}

// vaultRestoreAgentOutput is the JSON shape `vault restore --agent` prints (see
// pkg/cli vaultRestoreApprovalResponse). Defined locally to avoid importing the
// CLI package.
type vaultRestoreAgentOutput struct {
	Profile  string `json:"profile"`
	NextStep string `json:"next_step"`
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
