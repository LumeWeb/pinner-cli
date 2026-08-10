package mcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	// Second read must be gone (single use).
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusNotFound, rec2.Code)
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
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAttachSeedDropMintsURL(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")

	// Write a fake seed file on the host.
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "recovery.seed")
	require.NoError(t, os.WriteFile(seedPath, []byte("one two three\n"), 0600))

	out := `{"profile":"default","seed_path":"` + seedPath + `","next_step":"run restore"}`
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
