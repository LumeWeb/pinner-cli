package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustJSONQuote returns the JSON-escaped string literal for s, so filesystem
// paths with backslashes (Windows) are embedded validly in a JSON handoff.
func mustJSONQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestSeedDropSingleUse(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	url := d.Register("default", "alpha beta gamma")
	require.Contains(t, url, "/seed/")

	mux := http.NewServeMux()
	d.registerHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "alpha beta gamma")

	// Second read must be spent: a branded "link no longer active" page (410),
	// not the seed again and not a bare 404.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusGone, rec2.Code, "a consumed seed link must render 410 with the spent page")
	require.Contains(t, rec2.Body.String(), "no longer active")
	require.NotContains(t, rec2.Body.String(), "alpha beta gamma", "the seed must never be shown twice")
}

func TestSeedDropExpiry(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	url := d.Register("default", "secret words")

	mux := http.NewServeMux()
	d.registerHandlers(mux)

	// Advance past expiry.
	d.setNow(func() time.Time { return time.Now().Add(2 * time.Minute) })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusGone, rec.Code, "an expired seed link must render 410 with the spent page")
	require.Contains(t, rec.Body.String(), "no longer active")
	require.Contains(t, rec.Body.String(), "expired")
	require.NotContains(t, rec.Body.String(), "secret words", "the seed must never be shown after expiry")
}

// TestSeedDropTombstonePrunedOnWrite verifies spent tombstones do not grow
// without bound. Because the SeedDrop/OOBRestore coordinators never start the
// periodic reaper, tombstone pruning must happen lazily on the read/write
// path: a consumed token's tombstone older than spentHoldTTL is dropped the
// next time resolve/remove runs, so the in-memory map stays bounded.
func TestSeedDropTombstonePrunedOnWrite(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	url := d.Register("default", "alpha beta gamma")

	// Consume the drop: viewing it once creates a "used" tombstone.
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	token := seedTokenFromURL(t, url)
	require.Contains(t, d.core.spent, token, "a consumed drop must leave a tombstone")

	// Advance the clock past the tombstone hold window, then perform a write
	// (register + consume another drop) to trigger lazy pruning.
	base := time.Now()
	d.setNow(func() time.Time { return base.Add(spentHoldTTL + time.Minute) })
	url2 := d.Register("default", "second words")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, url2, nil))
	require.Equal(t, http.StatusOK, rec2.Code)

	token2 := seedTokenFromURL(t, url2)
	require.NotContains(t, d.core.spent, token, "a tombstone older than spentHoldTTL must be pruned on the write path")
	require.Contains(t, d.core.spent, token2, "a freshly consumed token's tombstone must be retained")
}

// seedTokenFromURL extracts the token (the final /seed/<token> path segment)
// from a seed drop URL.
func seedTokenFromURL(t *testing.T, seedURL string) string {
	t.Helper()
	u, err := url.Parse(seedURL)
	require.NoError(t, err)
	return filepath.Base(u.Path)
}

func TestAttachSeedDropMintsURL(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")

	// Write a fake seed file on the host.
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "recovery.seed")
	require.NoError(t, os.WriteFile(seedPath, []byte("one two three\n"), 0600))

	// Build the JSON handoff by marshaling so Windows drive-letter paths
	// (D:\a\...\recovery.seed) are JSON-escaped correctly (a raw backslash is
	// an invalid JSON escape and would make Unmarshal fail on Windows).
	out := `{"profile":"default","seed_path":` + mustJSONQuote(seedPath) + `,"next_step":"run restore"}`
	text, extra := attachSeedDrop(out, "pinner_vault_create", d)
	// Text unchanged; structured content carries the URL.
	assert.Equal(t, out, text)
	require.NotNil(t, extra)
	url, _ := extra["seed_url"].(string)
	require.Contains(t, url, "/seed/")

	// The mnemonic must NOT appear in extra; only the URL to retrieve it.
	require.NotContains(t, extra, "one two three")
}

func TestAttachSeedDropIgnoresOtherCommandsAndNil(t *testing.T) {
	out := `{"profile":"default","seed_path":"/tmp/x"}`
	text, extra := attachSeedDrop(out, "pinner_status", nil)
	assert.Equal(t, out, text)
	assert.Nil(t, extra)

	// Non-vault-create command with a live seed drop still passes through.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	text, extra = attachSeedDrop(out, "pinner_status", d)
	assert.Equal(t, out, text)
	assert.Nil(t, extra)
}
