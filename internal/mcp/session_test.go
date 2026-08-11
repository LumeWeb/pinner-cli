package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/looplab/fsm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestFSM creates a simple 3-state FSM: step1 -> step2 -> complete.
func newTestFSM() *fsm.FSM {
	return fsm.NewFSM("step1",
		[]fsm.EventDesc{
			{Name: "next", Src: []string{"step1"}, Dst: "step2"},
			{Name: "next", Src: []string{"step2"}, Dst: "complete"},
		},
		nil,
	)
}

// newTestSteps returns step definitions matching the test FSM.
func newTestSteps() []StepDef {
	return []StepDef{
		{Name: "step1", Event: "next", Handler: func(_ context.Context, _ *Session, _ json.RawMessage) (string, error) { return "", nil }},
		{Name: "step2", Event: "next", Handler: func(_ context.Context, _ *Session, _ json.RawMessage) (string, error) { return "", nil }},
	}
}

func TestSessionStore_Create(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	sess, err := store.Create("test-state", newTestFSM(), newTestSteps())
	require.NoError(t, err)

	assert.NotEmpty(t, sess.ID)
	assert.False(t, sess.CreatedAt.IsZero())
	assert.True(t, sess.ExpiresAt.After(sess.CreatedAt))
	assert.Equal(t, DefaultSessionTTL, sess.ExpiresAt.Sub(sess.CreatedAt))
	assert.Equal(t, "step1", sess.FSM.Current())
	assert.Equal(t, "test-state", sess.State())

	// Verify the session is persisted in the store.
	retrieved, err := store.Get(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, retrieved.ID)
}

func TestSessionStore_Get_NotFound(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	_, err := store.Get("nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_Delete(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	store.Delete(sess.ID)

	_, err = store.Get(sess.ID)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_Delete_NoOp(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	// Should not panic.
	store.Delete("nonexistent")
}

func TestSessionStore_Expiry(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithTTL(50 * time.Millisecond)
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	// Immediately retrievable.
	_, err = store.Get(sess.ID)
	require.NoError(t, err)

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	_, err = store.Get(sess.ID)
	assert.ErrorIs(t, err, ErrSessionExpired)

	// Expired session should be removed from the store.
	assert.Equal(t, 0, store.Count())
}

func TestSessionStore_Cleanup(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithTTL(50 * time.Millisecond)
	s1, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	s2, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	assert.Equal(t, 2, store.Count())

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	removed := store.Cleanup()
	assert.Equal(t, 2, removed)
	assert.Equal(t, 0, store.Count())

	// Cleanup should have no effect on already-empty store.
	removed = store.Cleanup()
	assert.Equal(t, 0, removed)

	// Records should be gone.
	_ = s1
	_ = s2
	_, err = store.Get(s1.ID)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_Cleanup_KeepsUnexpired(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithTTL(50 * time.Millisecond)
	expired, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	_ = expired

	time.Sleep(100 * time.Millisecond)

	// Create a fresh, unexpired session.
	live, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	_ = live

	removed := store.Cleanup()
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, store.Count())

	_, err = store.Get(live.ID)
	require.NoError(t, err)
}

func TestSessionStore_Count(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	assert.Equal(t, 0, store.Count())

	s1, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	assert.Equal(t, 1, store.Count())

	s2, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	assert.Equal(t, 2, store.Count())

	store.Delete(s1.ID)
	assert.Equal(t, 1, store.Count())

	store.Delete(s2.ID)
	assert.Equal(t, 0, store.Count())
}

func TestSessionStore_Concurrent(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithLimits(DefaultSessionTTL, 200)
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half: create sessions concurrently.
	ids := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			sess, err := store.Create(nil, newTestFSM(), newTestSteps())
			if err != nil {
				t.Errorf("store.Create failed: %v", err)
				return
			}
			ids <- sess.ID
		}()
	}

	// Half: read concurrently (from the same IDs once available).
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			store.Cleanup()
			store.Count()
		}()
	}

	wg.Wait()
	close(ids)

	created := 0
	for id := range ids {
		created++
		_, err := store.Get(id)
		assert.NoError(t, err, "session %s should exist", id)
	}
	assert.Equal(t, goroutines, created)
	assert.Equal(t, goroutines, store.Count())
}

func TestSession_Touch(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithTTL(50 * time.Millisecond)
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	originalExpiry := sess.ExpiresAt

	// Touch extends expiry.
	sess.Touch(DefaultSessionTTL)
	assert.True(t, sess.ExpiresAt.After(originalExpiry))
}

func TestSession_IsExpired(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithTTL(50 * time.Millisecond)
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	assert.False(t, sess.IsExpired())

	time.Sleep(100 * time.Millisecond)

	assert.True(t, sess.IsExpired())
}

func TestSession_State(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	sess, err := store.Create("initial", newTestFSM(), newTestSteps())
	require.NoError(t, err)

	assert.Equal(t, "initial", sess.State())

	sess.SetState("updated")
	assert.Equal(t, "updated", sess.State())
}

func TestSession_CurrentStep(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	step, ok := sess.CurrentStep()
	require.True(t, ok)
	assert.Equal(t, "step1", step.Name)

	// Advance FSM to step2.
	err = sess.FSM.Event(context.Background(), "next")
	require.NoError(t, err)

	step, ok = sess.CurrentStep()
	require.True(t, ok)
	assert.Equal(t, "step2", step.Name)

	// Advance FSM to complete.
	err = sess.FSM.Event(context.Background(), "next")
	require.NoError(t, err)

	// "complete" state has no step definition.
	_, ok = sess.CurrentStep()
	assert.False(t, ok)
}

func TestSession_NextSchema(t *testing.T) {
	t.Parallel()

	testSchema := schemaFor[DomainInput]()

	steps := []StepDef{
		{
			Name:   "step1",
			Event:  "next",
			Schema: func(_ *Session) *jsonschema.Schema { return testSchema },
		},
		{Name: "step2", Event: "next"},
	}

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), steps)
	require.NoError(t, err)

	schema := sess.NextSchema()
	require.NotNil(t, schema)
	require.NotNil(t, schema.Properties)
	_, ok := schema.Properties.Get("domain")
	assert.True(t, ok)

	// Advance to step2 (no schema defined).
	err = sess.FSM.Event(context.Background(), "next")
	require.NoError(t, err)

	schema = sess.NextSchema()
	assert.Nil(t, schema)

	// Advance to complete.
	err = sess.FSM.Event(context.Background(), "next")
	require.NoError(t, err)

	schema = sess.NextSchema()
	assert.Nil(t, schema)
}

func TestAdvanceSession(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	// step1 -> step2
	_, err = AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "step2", sess.FSM.Current())

	// step2 -> complete
	_, err = AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "complete", sess.FSM.Current())

	// No more steps to advance.
	_, err = AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	assert.Error(t, err)
}

func TestAdvanceSession_HandlerError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("validation failed")
	steps := []StepDef{
		{
			Name:    "step1",
			Event:   "next",
			Handler: func(_ context.Context, _ *Session, _ json.RawMessage) (string, error) { return "", handlerErr },
		},
	}

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), steps)
	require.NoError(t, err)

	_, err = AdvanceSession(context.Background(), sess, json.RawMessage(`{}`))
	assert.ErrorIs(t, err, handlerErr)
	// FSM should NOT have transitioned because the handler failed.
	assert.Equal(t, "step1", sess.FSM.Current())
}

func TestAdvanceSession_NilHandler(t *testing.T) {
	t.Parallel()

	steps := []StepDef{
		{Name: "step1", Event: "next"}, // nil handler
	}

	store := NewSessionStore()
	sess, err := store.Create(nil, newTestFSM(), steps)
	require.NoError(t, err)

	_, err = AdvanceSession(context.Background(), sess, nil)
	require.NoError(t, err)
	assert.Equal(t, "step2", sess.FSM.Current())
}

func TestNewSessionStoreWithTTL(t *testing.T) {
	t.Parallel()

	customTTL := 5 * time.Minute
	store := NewSessionStoreWithTTL(customTTL)
	sess, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)

	assert.Equal(t, customTTL, sess.ExpiresAt.Sub(sess.CreatedAt))
}

func TestSessionStore_MaxSessions(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithLimits(DefaultSessionTTL, 3)

	for i := 0; i < 3; i++ {
		_, err := store.Create(nil, newTestFSM(), newTestSteps())
		require.NoError(t, err)
	}
	assert.Equal(t, 3, store.Count())

	// Fourth session should fail.
	_, err := store.Create(nil, newTestFSM(), newTestSteps())
	assert.ErrorIs(t, err, ErrSessionStoreFull)
	assert.Equal(t, 3, store.Count())
}

func TestSessionStore_MaxSessions_EvictsExpired(t *testing.T) {
	t.Parallel()

	store := NewSessionStoreWithLimits(50*time.Millisecond, 2)

	// Fill to capacity.
	s1, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	s2, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	assert.Equal(t, 2, store.Count())

	// Wait for s1 to expire.
	time.Sleep(100 * time.Millisecond)

	// s2 is also expired: touch it to keep it alive.
	s2.Touch(50 * time.Millisecond)

	// New session should evict expired s1 and succeed.
	s3, err := store.Create(nil, newTestFSM(), newTestSteps())
	require.NoError(t, err)
	assert.Equal(t, 2, store.Count())

	// s1 should be gone.
	_, err = store.Get(s1.ID)
	assert.ErrorIs(t, err, ErrSessionNotFound)

	_ = s3
}
