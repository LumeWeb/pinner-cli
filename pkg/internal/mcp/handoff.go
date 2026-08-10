package mcp

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// handoffEndpoint is the shared core behind every "create or read X secret over
// a one-time HTTP endpoint without it touching the MCP/LLM channel" coordinator
// (OOB login, vault seed read, vault restore collect). It owns the mechanics
// that are identical across all of them:
//
//   - a one-time, expiring, unguessable /<prefix>/<token> URL,
//   - loopback-listener-or-shared-mux bootstrap (stdio vs HTTP/tunnel),
//   - CSRF origin checking on POST,
//   - single-use consumption (or retention for resumable/polling flows).
//
// Each concrete coordinator embeds a handoffEndpoint, sets the route prefix,
// and supplies a handoffHandler for the GET page and POST consume that are
// specific to the secret being handled.
type handoffEndpoint struct {
	loopback loopbackServer

	mu    sync.Mutex
	items map[string]*handoffItem
	// spent records recently-consumed or expired tokens so a re-open of a
	// one-time URL can be told apart from a token that never existed, and so a
	// spent link renders a branded "link no longer active" page instead of a
	// bare 404. Entries are pruned lazily after spentHoldTTL.
	spent map[string]time.Time
	ttl   time.Duration
	now   func() time.Time

	prefix  string
	handler handoffHandler

	// Reaper, used by resumable flows (login) so pending items do not
	// accumulate for the process lifetime. Simple expiring coordinators rely on
	// lazy expiry instead and leave these nil.
	reaperCtx    context.Context
	reaperCancel context.CancelFunc
}

// spentHoldTTL is how long a consumed/expired token's tombstone is kept so a
// re-open can render the spent-link page. It must outlive the read that would
// follow a single-use GET (rendering then the client immediately closes), but
// need not persist for the whole process: spent URLs only need to explain
// themselves for a short window before the tombstone can be forgotten.
const spentHoldTTL = 10 * time.Minute

// handoffItem is a single pending hand-off. payload is interpreted by the
// concrete handler (it may hold a secret to display, an input to collect, or a
// resumable workflow state).
type handoffItem struct {
	payload   any
	expiresAt time.Time
}

// handoffHandler supplies the per-secret GET and POST behavior. The core
// handles routing, CSRF, expiry, and single-use bookkeeping.
type handoffHandler interface {
	// renderGET renders the GET page for a pending token. If consumeOnGET is
	// true, the token is consumed after render (read-direction flows that show
	// a secret exactly once, like a seed drop).
	renderGET(w http.ResponseWriter, r *http.Request, token string, item *handoffItem)
	// consumeOnGET reports whether a GET should consume the token (single-use
	// display flows). Collect-direction flows (which take input on POST) return
	// false.
	consumeOnGET() bool
	// consumePOST handles a CSRF-validated POST. It returns consumed=true when
	// the token must be deleted immediately (single-use collect flows); false
	// keeps it for a later outcome (resumable/polling flows like login).
	consumePOST(w http.ResponseWriter, r *http.Request, token string, item *handoffItem) (consumed bool)
}

// newHandoff creates a handoff core with the given route prefix and handler.
func newHandoff(prefix string, handler handoffHandler, ttl time.Duration) *handoffEndpoint {
	if ttl <= 0 {
		ttl = DefaultHandoffTTL
	}
	return &handoffEndpoint{
		items:   make(map[string]*handoffItem),
		spent:   make(map[string]time.Time),
		ttl:     ttl,
		now:     time.Now,
		prefix:  strings.Trim(prefix, "/"),
		handler: handler,
	}
}

// DefaultHandoffTTL is how long a hand-off URL stays valid before it expires.
const DefaultHandoffTTL = 30 * time.Minute

// SetBaseURL sets the externally reachable base URL (public/tunnel in HTTP
// mode, or empty for the loopback-derived URL in stdio mode).
func (h *handoffEndpoint) SetBaseURL(url string) {
	h.loopback.SetBaseURL(url)
}

// mint registers a payload and returns its one-time URL. It ensures the
// loopback listener is running in stdio mode so the URL is always reachable.
func (h *handoffEndpoint) mint(payload any) string {
	if err := h.loopback.ensureLoopback(h.registerHandlers); err != nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	token := strongRandomID()
	h.items[token] = &handoffItem{payload: payload, expiresAt: h.now().Add(h.ttl)}
	return h.loopback.urlLocked(h.prefix, token)
}

// registerHandlers mounts the /<prefix>/ route on the shared mux.
func (h *handoffEndpoint) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/"+h.prefix+"/", h.handle)
}

// handle routes a GET/POST to the concrete handler. A token that is valid
// (issued, not yet used, not yet expired) dispatches to the concrete handler; a
// token that is already used or expired renders the shared branded "link no
// longer active" page immediately (no submit required), so a human who reopens
// a one-time seed/restore URL learns it is spent instead of hitting a bare 404
// or a fresh form. Only a token that never existed gets a 404.
func (h *handoffEndpoint) handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/"+h.prefix+"/")
	item, reason := h.resolve(token)
	if item == nil {
		if reason != "" {
			h.spentPage(w, r, reason)
			return
		}
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handler.renderGET(w, r, token, item)
		if h.handler.consumeOnGET() {
			h.remove(token)
		}
	case http.MethodPost:
		if !sameOrigin(r, h.loopback.acceptedOrigins()...) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		consumed := h.handler.consumePOST(w, r, token, item)
		if consumed {
			h.remove(token)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// resolve returns a valid (issued, unexpired) item for a token, or — when the
// token exists but is no longer usable — a nil item plus the reason it is spent
// (handoffUsed if it was consumed, handoffExpired if its TTL elapsed). For a
// token that never existed it returns nil and an empty reason, so the caller
// can keep a 404. Consumed and expired tokens are recorded as tombstones so the
// spent state is observable on a re-open.
func (h *handoffEndpoint) resolve(token string) (*handoffItem, handoffNotActiveReason) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneSpentLocked()
	if _, ok := h.spent[token]; ok {
		return nil, handoffUsed
	}
	item, ok := h.items[token]
	if !ok {
		return nil, ""
	}
	if h.now().After(item.expiresAt) {
		delete(h.items, token)
		h.spent[token] = h.now()
		return nil, handoffExpired
	}
	return item, ""
}

// pruneSpentLocked drops spent tombstones older than spentHoldTTL. It must be
// called with h.mu held. It runs on the read/write paths (resolve/remove) so
// tombstones cannot grow without bound even though the periodic reaper
// (startReaper) is not started by the SeedDrop/OOBRestore coordinators.
func (h *handoffEndpoint) pruneSpentLocked() {
	cutoff := h.now().Add(-spentHoldTTL)
	for token, spentAt := range h.spent {
		if spentAt.Before(cutoff) {
			delete(h.spent, token)
		}
	}
}

// lookup returns a valid (non-expired) item for a token, pruning it if stale.
func (h *handoffEndpoint) lookup(token string) (*handoffItem, bool) {
	item, reason := h.resolve(token)
	if item == nil {
		return nil, false
	}
	_ = reason
	return item, true
}

// spentPage renders the shared branded "link no longer active" page (410 Gone)
// for a used or expired one-time URL.
func (h *handoffEndpoint) spentPage(w http.ResponseWriter, r *http.Request, reason handoffNotActiveReason) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	detail := "This one-time link cannot be used again."
	if reason == handoffExpired {
		detail = "This one-time link expired before it was used."
	}
	_ = handoffNotActivePage(reason, detail).Render(r.Context(), w)
}

// remove deletes a token from the pending set and records it as spent so a
// re-open renders the spent-link page rather than a bare 404.
func (h *handoffEndpoint) remove(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.items, token)
	if _, ok := h.spent[token]; !ok {
		h.spent[token] = h.now()
	}
	h.pruneSpentLocked()
}

// get is a lightweight direct fetch used by children that need the item for
// polling without going through the method-dispatch.
func (h *handoffEndpoint) get(token string) (*handoffItem, bool) {
	return h.lookup(token)
}

// visitRange iterates all pending items under the core's lock, letting a child
// (e.g. login outcome polling keyed by email+sessionID) scan the set. The
// callback must not call back into the core's locked methods.
func (h *handoffEndpoint) visitRange(fn func(token string, item *handoffItem)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, item := range h.items {
		fn(token, item)
	}
}

// startReaper spawns a goroutine that periodically prunes expired items.
func (h *handoffEndpoint) startReaper(interval time.Duration) {
	h.mu.Lock()
	if h.reaperCancel != nil {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.reaperCtx = ctx
	h.reaperCancel = cancel
	h.mu.Unlock()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.pruneExpired()
			}
		}
	}()
}

// pruneExpired removes items whose TTL has elapsed and tombstones older than
// spentHoldTTL, bounding the maps over a long-running process.
func (h *handoffEndpoint) pruneExpired() {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, item := range h.items {
		if now.After(item.expiresAt) {
			delete(h.items, token)
			if _, ok := h.spent[token]; !ok {
				h.spent[token] = now
			}
		}
	}
	for token, spentAt := range h.spent {
		if now.Sub(spentAt) > spentHoldTTL {
			delete(h.spent, token)
		}
	}
}

// Stop shuts down the loopback listener and reaper, if any.
func (h *handoffEndpoint) Stop(ctx context.Context) {
	h.mu.Lock()
	cancel := h.reaperCancel
	h.reaperCancel = nil
	h.reaperCtx = nil
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	h.loopback.Stop(ctx)
}

// count returns the number of pending, unexpired items (tests + instrumentation).
func (h *handoffEndpoint) count() int {
	h.pruneExpired()
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.items)
}

// setNow overrides the clock used for expiry (test seam).
func (h *handoffEndpoint) setNow(f func() time.Time) {
	if f == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.now = f
}
