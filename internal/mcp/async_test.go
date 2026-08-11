package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsyncHandleRoundTrip verifies mint -> status -> update -> terminal flow.
func TestAsyncHandleRoundTrip(t *testing.T) {
	s := NewAsyncHandleStore(time.Minute, 10)
	id := s.Create("running", map[string]any{"tool": "restore"})
	require.NotEmpty(t, id)

	status, data, err := s.Get(id)
	require.NoError(t, err)
	assert.Equal(t, "running", status)
	assert.Equal(t, "restore", data["tool"])

	require.NoError(t, s.Set(id, "done", map[string]any{"ok": true}))
	status, data, err = s.Get(id)
	require.NoError(t, err)
	assert.Equal(t, "done", status)
	assert.Equal(t, true, data["ok"])
}

// TestAsyncHandleNotFound verifies unknown handles are reported, not hung.
func TestAsyncHandleNotFound(t *testing.T) {
	s := NewAsyncHandleStore(time.Minute, 10)
	_, _, err := s.Get("nope")
	assert.ErrorIs(t, err, ErrHandleNotFound)
	assert.ErrorIs(t, s.Set("nope", "done", nil), ErrHandleNotFound)
}

// TestAsyncHandleExpiry verifies expired handles are evicted and reported.
func TestAsyncHandleExpiry(t *testing.T) {
	s := NewAsyncHandleStore(time.Minute, 10)
	// Force a short TTL and a controllable clock.
	s = NewAsyncHandleStore(time.Minute, 10)
	id := s.Create("running", nil)
	require.NoError(t, s.Set(id, "running", nil))

	// Advance the clock past expiry.
	future := time.Now().Add(2 * time.Minute)
	s.now = func() time.Time { return future }

	_, _, err := s.Get(id)
	assert.ErrorIs(t, err, ErrHandleExpired)
	// Subsequent lookups report not-found (item evicted).
	_, _, err = s.Get(id)
	assert.ErrorIs(t, err, ErrHandleNotFound)
}
