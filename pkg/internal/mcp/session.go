package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
	"github.com/looplab/fsm"
)

// DefaultSessionTTL is the lifetime of a wizard session before it expires.
const DefaultSessionTTL = 30 * time.Minute

// DefaultMaxSessions is the maximum number of concurrent wizard sessions
// the store will hold. This prevents unbounded memory growth from abandoned
// sessions.
const DefaultMaxSessions = 100

// ErrSessionNotFound is returned when a session lookup fails.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionExpired is returned when a session exists but has passed its TTL.
var ErrSessionExpired = errors.New("session expired")

// ErrSessionStoreFull is returned when the session store has reached its
// maximum capacity and no expired sessions can be reclaimed.
var ErrSessionStoreFull = errors.New("session store is full")

// WizardState is the opaque state carried by a session. Each wizard type
// (websites, setup) stores its own state struct here.
type WizardState any

// StepHandler validates and applies input for a single wizard step.
// The context is the request context from the MCP tool call.
type StepHandler func(ctx context.Context, session *Session, input json.RawMessage) error

// StepDef describes a single wizard step for MCP session purposes.
type StepDef struct {
	// Name is the FSM state name for this step.
	Name string
	// Event is the FSM event that transitions out of this step.
	Event string
	// Handler processes the input for this step. May be nil for read-only steps.
	Handler StepHandler
	// Schema returns the JSON schema describing the expected input for the
	// next step, or nil if this is the final step.
	Schema func(current *Session) *jsonschema.Schema
}

// Session holds a single wizard's state, FSM, step definitions, and timing.
type Session struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time

	mu        sync.RWMutex
	advanceMu sync.Mutex
	state     WizardState
	FSM       *fsm.FSM
	steps     []StepDef
	stepMap   map[string]StepDef
}

// State returns the wizard state. The returned value is safe to read
// concurrently but callers must not mutate it without holding the session.
func (s *Session) State() WizardState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetState replaces the wizard state.
func (s *Session) SetState(state WizardState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// CurrentStep returns the StepDef matching the FSM's current state.
// Returns ok=false if the current state has no step definition (e.g. "complete").
func (s *Session) CurrentStep() (StepDef, bool) {
	step, ok := s.stepMap[s.FSM.Current()]
	return step, ok
}

// NextSchema returns the JSON schema for the next expected step input,
// or nil if the wizard is complete.
func (s *Session) NextSchema() *jsonschema.Schema {
	step, ok := s.CurrentStep()
	if !ok || step.Schema == nil {
		return nil
	}
	return step.Schema(s)
}

// IsExpired returns true if the session has passed its TTL.
func (s *Session) IsExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Now().After(s.ExpiresAt)
}

// Touch extends the session's expiry by the TTL.
func (s *Session) Touch(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ExpiresAt = time.Now().Add(ttl)
}

// SessionStore is a concurrency-safe, TTL-bounded in-memory store of wizard
// sessions. It is not persisted: sessions die when the process exits.
// The store enforces a maximum session count to prevent unbounded memory
// growth from abandoned sessions.
type SessionStore struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	ttl         time.Duration
	maxSessions int
	now         func() time.Time
}

// NewSessionStore creates a SessionStore with the default 30-minute TTL
// and DefaultMaxSessions capacity.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions:    make(map[string]*Session),
		ttl:         DefaultSessionTTL,
		maxSessions: DefaultMaxSessions,
		now:         time.Now,
	}
}

// NewSessionStoreWithTTL creates a SessionStore with a custom TTL and
// DefaultMaxSessions capacity.
func NewSessionStoreWithTTL(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions:    make(map[string]*Session),
		ttl:         ttl,
		maxSessions: DefaultMaxSessions,
		now:         time.Now,
	}
}

// NewSessionStoreWithLimits creates a SessionStore with a custom TTL and
// maximum session count.
func NewSessionStoreWithLimits(ttl time.Duration, maxSessions int) *SessionStore {
	return &SessionStore{
		sessions:    make(map[string]*Session),
		ttl:         ttl,
		maxSessions: maxSessions,
		now:         time.Now,
	}
}

// Create creates a new session with the given wizard state, FSM, and step
// definitions. The session ID is a generated UUID. The TTL is set from the
// store's configured duration. If the store is at capacity, expired sessions
// are evicted first; if it is still full, Create returns the pre-built
// session and ErrSessionStoreFull.
func (s *SessionStore) Create(state WizardState, fsmInst *fsm.FSM, steps []StepDef) (*Session, error) {
	stepMap := make(map[string]StepDef, len(steps))
	for _, step := range steps {
		stepMap[step.Name] = step
	}

	now := s.now()
	sess := &Session{
		ID:        uuid.New().String(),
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		state:     state,
		FSM:       fsmInst,
		steps:     steps,
		stepMap:   stepMap,
	}

	s.mu.Lock()
	// Evict expired sessions to reclaim capacity.
	if len(s.sessions) >= s.maxSessions {
		for id, existing := range s.sessions {
			if existing.IsExpired() {
				delete(s.sessions, id)
			}
		}
	}
	if len(s.sessions) >= s.maxSessions {
		s.mu.Unlock()
		return sess, ErrSessionStoreFull
	}
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	return sess, nil
}

// Get returns the session with the given ID. Returns ErrSessionNotFound if no
// session exists, or ErrSessionExpired if the session has expired (the session
// is also removed from the store in that case).
func (s *SessionStore) Get(id string) (*Session, error) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	if sess.IsExpired() {
		s.Delete(id)
		return nil, ErrSessionExpired
	}

	return sess, nil
}

// Delete removes a session from the store. No-op if it doesn't exist.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Cleanup removes all expired sessions from the store. Returns the number
// of sessions removed.
func (s *SessionStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, sess := range s.sessions {
		if sess.IsExpired() {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

// Count returns the number of sessions currently in the store.
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// AdvanceSession calls the current step's handler with the given input, then
// fires the FSM event to transition to the next step. It is a helper for
// wizard tool implementations that centralizes the lookup→handle→transition
// flow.
func AdvanceSession(ctx context.Context, sess *Session, input json.RawMessage) error {
	sess.advanceMu.Lock()
	defer sess.advanceMu.Unlock()

	step, ok := sess.CurrentStep()
	if !ok {
		return errors.New("no active step to advance")
	}

	if step.Handler != nil {
		if err := step.Handler(ctx, sess, input); err != nil {
			return err
		}
	}

	if err := sess.FSM.Event(ctx, step.Event); err != nil {
		return err
	}

	return nil
}
