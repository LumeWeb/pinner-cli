package handoff

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transport"
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
type Endpoint struct {
	loopback transport.LoopbackServer

	mu    sync.Mutex
	items map[string]*Item
	// spent records consumed or expired tokens so a re-open of a one-time URL
	// can be told apart from a token that never existed, and so a spent link
	// renders a branded "link no longer active" page instead of a bare 404.
	// spentOrder is an insertion-ordered index (FIFO) of spent keys, so the
	// oldest tombstone can be evicted in O(1) when the map exceeds its cap.
	spent      map[string]time.Time
	spentOrder []string
	ttl        time.Duration
	now        func() time.Time

	// logger logs hand-off lifecycle events (mint, consume, expire). It
	// defaults to the shared package logger.
	logger *zap.Logger

	prefix  string
	handler Handler

	// Reaper, used by resumable flows (login) so pending items do not
	// accumulate for the process lifetime. Simple expiring coordinators rely on
	// lazy expiry instead and leave these nil.
	reaperCtx    context.Context
	reaperCancel context.CancelFunc
}

// maxSpentTombstones is the capacity cap for spent-link tombstones. Consumed
// and expired one-time URLs are remembered indefinitely so a re-open can still
// explain that the link was used/expired (a human may re-open a login, seed, or
// restore link well after it was spent, so the explanation must not vanish on a
// clock). To keep memory bounded on a long-running MCP process, the oldest
// tombstones are evicted only when the map exceeds this cap (FIFO), so the
// spent explanation persists for any link within retention while the total
// memory stays flat.
const MaxSpentTombstones = 10000

// handoffItem is a single pending hand-off. payload is interpreted by the
// concrete handler (it may hold a secret to display, an input to collect, or a
// resumable workflow state).
type Item struct {
	Payload   any
	expiresAt time.Time
}

// handoffHandler supplies the per-secret GET and POST behavior. The core
// handles routing, CSRF, expiry, and single-use bookkeeping.
type Handler interface {
	// renderGET renders the GET page for a pending token. If consumeOnGET is
	// true, the token is consumed after render (read-direction flows that show
	// a secret exactly once, like a seed drop).
	RenderGET(w http.ResponseWriter, r *http.Request, token string, item *Item)
	// consumeOnGET reports whether a GET should consume the token (single-use
	// display flows). Collect-direction flows (which take input on POST) return
	// false.
	ConsumeOnGET() bool
	// consumePOST handles a CSRF-validated POST. It returns consumed=true when
	// the token must be deleted immediately (single-use collect flows); false
	// keeps it for a later outcome (resumable/polling flows like login).
	ConsumePOST(w http.ResponseWriter, r *http.Request, token string, item *Item) (consumed bool)
}

// newHandoff creates a handoff core with the given route prefix and handler.
func New(prefix string, handler Handler, ttl time.Duration) *Endpoint {
	if ttl <= 0 {
		ttl = DefaultHandoffTTL
	}
	return &Endpoint{
		items:   make(map[string]*Item),
		spent:   make(map[string]time.Time),
		ttl:     ttl,
		now:     time.Now,
		prefix:  strings.Trim(prefix, "/"),
		handler: handler,
		logger:  log,
	}
}

// Prefix returns the route prefix this endpoint serves under (e.g. "account").
// It is used by concrete handlers to form the self-referential action URL.
func (h *Endpoint) Prefix() string { return h.prefix }

// Spent returns a snapshot of the spent-tombstone map (hand-off tokens that
// have already been consumed/expired and are retained only to reject re-use).
// Tests use it to assert oldest-first eviction under MaxSpentTombstones.
func (h *Endpoint) Spent() map[string]time.Time {
	m := make(map[string]time.Time, len(h.spent))
	for k, v := range h.spent {
		m[k] = v
	}
	return m
}

// WithLogger sets the zap logger the hand-off endpoint uses for lifecycle
// events. It defaults to the shared package logger.
func (h *Endpoint) WithLogger(l *zap.Logger) *Endpoint {
	if l != nil {
		h.logger = l
	}
	return h
}

// logf returns the hand-off endpoint's logger, falling back to the package
// logger.
func (h *Endpoint) Logf() *zap.Logger {
	if h.logger != nil {
		return h.logger
	}
	return log
}

// DefaultHandoffTTL is how long a hand-off URL stays valid before it expires.
const DefaultHandoffTTL = 30 * time.Minute

// SetBaseURL sets the externally reachable base URL (public/tunnel in HTTP
// mode, or empty for the loopback-derived URL in stdio mode).
func (h *Endpoint) SetBaseURL(url string) {
	h.loopback.SetBaseURL(url)
}

// mint registers a payload and returns its one-time URL. It ensures the
// loopback listener is running in stdio mode so the URL is always reachable.
func (h *Endpoint) Mint(Payload any) string {
	if err := h.loopback.EnsureLoopback(h.RegisterHandlers); err != nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	token := session.StrongRandomID()
	h.items[token] = &Item{Payload: Payload, expiresAt: h.now().Add(h.ttl)}
	h.Logf().Debug("one-time hand-off minted", zap.String("prefix", h.prefix))
	return h.loopback.URLFor(h.prefix, token)
}

// registerHandlers mounts the /<prefix>/ route on the shared mux.
func (h *Endpoint) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/"+h.prefix+"/", h.handle)
}

// handle routes a GET/POST to the concrete handler. A token that is valid
// (issued, not yet used, not yet expired) dispatches to the concrete handler; a
// token that is already used or expired renders the shared branded "link no
// longer active" page immediately (no submit required), so a human who reopens
// a one-time seed/restore URL learns it is spent instead of hitting a bare 404
// or a fresh form. Only a token that never existed gets a 404.
func (h *Endpoint) handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/"+h.prefix+"/")
	item, reason := h.Resolve(token)
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
		h.handler.RenderGET(w, r, token, item)
		if h.handler.ConsumeOnGET() {
			h.Remove(token)
		}
	case http.MethodPost:
		if !SameOrigin(r, h.loopback.AcceptedOrigins()...) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		consumed := h.handler.ConsumePOST(w, r, token, item)
		if consumed {
			h.Remove(token)
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
func (h *Endpoint) Resolve(token string) (*Item, NotActiveReason) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.PruneSpentLocked()
	if _, ok := h.spent[token]; ok {
		return nil, ReasonUsed
	}
	item, ok := h.items[token]
	if !ok {
		return nil, ""
	}
	if h.now().After(item.expiresAt) {
		delete(h.items, token)
		h.MarkSpentLocked(token, h.now())
		return nil, ReasonExpired
	}
	return item, ""
}

// markSpentLocked records a consumed/expired token as spent and appends it to
// the FIFO eviction order. It must be called with h.mu held. The order slice is
// kept in sync with eviction by pruneSpentLocked (which trims the head).
func (h *Endpoint) MarkSpentLocked(token string, at time.Time) {
	if _, ok := h.spent[token]; ok {
		return
	}
	h.spent[token] = at
	h.spentOrder = append(h.spentOrder, token)
}

// pruneSpentLocked bounds the spent-tombstone map to maxSpentTombstones by
// evicting the oldest entries (FIFO) only when the cap is exceeded, so a spent
// link's explanation persists as long as it is within retention instead of
// vanishing on a clock. It must be called with h.mu held. It runs on the
// read/write paths (resolve/remove) so tombstones cannot grow without bound
// even though the periodic reaper (startReaper) is not started by the
// SeedDrop/OOBRestore coordinators.
func (h *Endpoint) PruneSpentLocked() {
	// Evict the oldest tombstones from the FIFO head until the map is within
	// the cap. O(overflow), never a re-scan of the whole map per entry.
	for len(h.spent) > MaxSpentTombstones && len(h.spentOrder) > 0 {
		oldest := h.spentOrder[0]
		h.spentOrder = h.spentOrder[1:]
		if _, ok := h.spent[oldest]; ok {
			delete(h.spent, oldest)
		}
	}
}

// lookup returns a valid (non-expired) item for a token, pruning it if stale.
func (h *Endpoint) lookup(token string) (*Item, bool) {
	item, reason := h.Resolve(token)
	if item == nil {
		return nil, false
	}
	_ = reason
	return item, true
}

// spentPage renders the shared branded "link no longer active" page (410 Gone)
// for a used or expired one-time URL.
func (h *Endpoint) spentPage(w http.ResponseWriter, r *http.Request, reason NotActiveReason) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	detail := "This one-time link cannot be used again."
	if reason == ReasonExpired {
		detail = "This one-time link expired before it was used."
	}
	_ = NotActivePage(reason, detail).Render(r.Context(), w)
}

// remove deletes a token from the pending set and records it as spent so a
// re-open renders the spent-link page rather than a bare 404.
func (h *Endpoint) Remove(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.items, token)
	h.MarkSpentLocked(token, h.now())
	h.PruneSpentLocked()
}

// claim atomically takes a token for a single long-running handler, removing it
// from the pending set and marking it spent under the lock so a concurrent or
// repeated call cannot re-resolve the same token. It reports whether the token
// was present and claimed. Handlers that block for a long time (e.g. an OOB
// restore waiting on a browser approval) claim first so a re-POST during the
// window is rejected instead of re-entering the blocking work.
func (h *Endpoint) Claim(token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.items[token]; !ok {
		return false
	}
	delete(h.items, token)
	h.MarkSpentLocked(token, h.now())
	h.PruneSpentLocked()
	return true
}

// get is a lightweight direct fetch used by children that need the item for
// polling without going through the method-dispatch.
func (h *Endpoint) get(token string) (*Item, bool) {
	return h.lookup(token)
}

// visitRange iterates all pending items under the core's lock, letting a child
// (e.g. login outcome polling keyed by email+sessionID) scan the set. The
// callback must not call back into the core's locked methods.
func (h *Endpoint) visitRange(fn func(token string, item *Item)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, item := range h.items {
		fn(token, item)
	}
}

// startReaper spawns a goroutine that periodically prunes expired items.
func (h *Endpoint) startReaper(interval time.Duration) {
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

// pruneExpired removes items whose TTL has elapsed, moving them to spent
// tombstones, and bounds the spent map to maxSpentTombstones.
func (h *Endpoint) pruneExpired() {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, item := range h.items {
		if now.After(item.expiresAt) {
			delete(h.items, token)
			h.MarkSpentLocked(token, now)
		}
	}
	h.PruneSpentLocked()
}

// Stop shuts down the loopback listener and reaper, if any.
func (h *Endpoint) Stop(ctx context.Context) {
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
func (h *Endpoint) Count() int {
	h.pruneExpired()
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.items)
}

// setNow overrides the clock used for expiry (test seam).
func (h *Endpoint) SetNow(f func() time.Time) {
	if f == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.now = f
}
