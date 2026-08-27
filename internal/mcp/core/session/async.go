package session

import (
	"errors"
	"sync"
	"time"
)

// AsyncHandleStore is a concurrency-safe, TTL-bounded store of async operation
// handles. It backs the async/status pattern the gateway uses for long-running
// or approval-gated operations: a tool starts an operation, mints a handle, and
// returns immediately; a matching *_status tool polls the handle until it
// reaches a terminal state.
//
// It is intentionally lightweight and decoupled from the wizard SessionStore,
// which is FSM-specific. A handle carries an opaque payload plus an expiry.
type AsyncHandleStore struct {
	mu     sync.Mutex
	items  map[string]*asyncItem
	ttl    time.Duration
	maxLen int
	now    func() time.Time
}

type asyncItem struct {
	status    string
	data      map[string]any
	createdAt time.Time
	expiresAt time.Time
}

// Errors returned by the async handle store.
var (
	ErrHandleNotFound = errors.New("async handle not found")
	ErrHandleExpired  = errors.New("async handle expired")
)

// NewAsyncHandleStore creates an AsyncHandleStore with the given TTL and
// maximum item count.
func NewAsyncHandleStore(ttl time.Duration, maxLen int) *AsyncHandleStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxSessions
	}
	return &AsyncHandleStore{
		items:  make(map[string]*asyncItem),
		ttl:    ttl,
		maxLen: maxLen,
		now:    time.Now,
	}
}

// Create mints a new handle with the given initial status and data and returns
// the handle id. If the store is at/over capacity it evicts expired items
// first; if still over capacity it reuses the map (best-effort) — handles are
// cheap and expiry-bounded, so we never fail a start on capacity.
func (s *AsyncHandleStore) Create(status string, data map[string]any) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.items) >= s.maxLen {
		for id, it := range s.items {
			if s.now().After(it.expiresAt) {
				delete(s.items, id)
			}
		}
	}

	now := s.now()
	id := RandomID()
	s.items[id] = &asyncItem{
		status:    status,
		data:      data,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	return id
}

// CreateWithID ensures a handle with the caller-supplied id exists: creating
// it if absent, or refreshing an existing entry's expiry (and data) if present.
// It is the companion to Create for callers that need the handle id to equal a
// coordinator-generated id (e.g. the OOB login request id, so the resume handle
// and the approval-link token are the same value). Idempotent and concurrency-
// safe; like Create it never fails on capacity (it evicts expired items first
// and otherwise allows the map to grow, bounded by TTL).
func (s *AsyncHandleStore) CreateWithID(id, status string, data map[string]any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if it, ok := s.items[id]; ok {
		it.status = status
		if data != nil {
			it.data = data
		}
		it.expiresAt = s.now().Add(s.ttl)
		return id
	}
	if len(s.items) >= s.maxLen {
		for oldID, it := range s.items {
			if s.now().After(it.expiresAt) {
				delete(s.items, oldID)
			}
		}
	}
	now := s.now()
	s.items[id] = &asyncItem{
		status:    status,
		data:      data,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	return id
}

// Get returns the current status and data for a handle. Returns
// ErrHandleNotFound if no such handle exists, or ErrHandleExpired if it has
// passed its TTL (the item is removed in that case).
func (s *AsyncHandleStore) Get(id string) (string, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return "", nil, ErrHandleNotFound
	}
	if s.now().After(it.expiresAt) {
		delete(s.items, id)
		return "", nil, ErrHandleExpired
	}
	return it.status, it.data, nil
}

// Set updates the status (and optionally data) for a handle, extending its
// expiry by the store TTL. No-op with ErrHandleNotFound if absent/expired.
func (s *AsyncHandleStore) Set(id, status string, data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return ErrHandleNotFound
	}
	if s.now().After(it.expiresAt) {
		delete(s.items, id)
		return ErrHandleExpired
	}
	it.status = status
	if data != nil {
		it.data = data
	}
	it.expiresAt = s.now().Add(s.ttl)
	return nil
}

// Delete removes a handle. No-op if absent.
func (s *AsyncHandleStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}

// SetNowFunc overrides the store's internal clock, primarily for tests that
// need to simulate expiry. Pass nil to restore the real clock (time.Now).
func (s *AsyncHandleStore) SetNowFunc(f func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f == nil {
		s.now = time.Now
		return
	}
	s.now = f
}
