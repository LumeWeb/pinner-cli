package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRestoreRunner struct {
	profile string
	calls   int
}

func (f *fakeRestoreRunner) RestoreProfileName() string { return f.profile }
func (f *fakeRestoreRunner) RunRestore(ctx context.Context, profile, mnemonic string) (string, error) {
	f.calls++
	if mnemonic == "" {
		return "", assert.AnError
	}
	return "vault-abc123", nil
}

// buildRestore returns a wired OOBRestore with a fake runner and a thrown-away
// mux on which /restore/ is mounted for direct serving in tests.
func buildRestoreServer() (*OOBRestore, *http.ServeMux, *fakeRestoreRunner) {
	runner := &fakeRestoreRunner{profile: "default"}
	o := NewOOBRestore(runner, time.Minute)
	o.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	o.registerHandlers(mux)
	return o, mux, runner
}

func TestOOBRestoreFormSingleUse(t *testing.T) {
	o, mux, runner := buildRestoreServer()
	url := o.Register("default")
	require.Contains(t, url, "/restore/")

	// GET serves the form.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Recovery phrase")

	// POST from a foreign origin is rejected (CSRF).
	badReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=secret words"))
	badReq.Header.Set("Origin", "http://evil.example")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	require.Equal(t, http.StatusForbidden, badRec.Code)
	require.Zero(t, runner.calls, "no restore should run on a rejected POST")

	// POST with a matching origin completes the restore once.
	okReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=alpha+beta+gamma"))
	okReq.Header.Set("Origin", "http://127.0.0.1:9999")
	okReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	okRec := httptest.NewRecorder()
	mux.ServeHTTP(okRec, okReq)
	require.Equal(t, http.StatusOK, okRec.Code)
	require.Equal(t, 1, runner.calls)
	require.Contains(t, okRec.Body.String(), "vault-abc123")

	// The token is now consumed: a repeat POST is not found.
	repeat := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=again"))
	repeat.Header.Set("Origin", "http://127.0.0.1:9999")
	repeat.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	repeatRec := httptest.NewRecorder()
	mux.ServeHTTP(repeatRec, repeat)
	require.Equal(t, http.StatusNotFound, repeatRec.Code)
}

func TestOOBRestoreExpiry(t *testing.T) {
	o, mux, _ := buildRestoreServer()
	url := o.Register("default")

	// Advance past expiry.
	o.now = func() time.Time { return time.Now().Add(2 * time.Minute) }

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOOBRestoreAttachURL(t *testing.T) {
	o, _, _ := buildRestoreServer()

	// Only pinner_vault_restore with a profile mints a URL.
	assert.Equal(t, "", attachRestoreURL(`{"profile":"default"}`, "pinner_status", o))
	assert.Equal(t, "", attachRestoreURL(`{"profile":"default"}`, "pinner_vault_restore", nil))
	u := attachRestoreURL(`{"profile":"default","next_step":"re-run"}`, "pinner_vault_restore", o)
	require.Contains(t, u, "/restore/")
}

// TestSeedDropStdioLoopback verifies the stdio path (no base URL): Register
// returns a reachable 127.0.0.1 loopback URL and the drop serves once.
func TestSeedDropStdioLoopback(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	// No base URL -> loopback listener is started lazily by Register.
	url := d.Register("default", "loopback seed words")
	require.True(t, strings.HasPrefix(url, "http://127.0.0.1:"), "expected loopback URL, got %q", url)
	defer d.Stop(context.Background())

	// Hit the real loopback listener; the seed is shown once.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "loopback seed words")

	// Second read is gone (single use).
	resp2, err := client.Get(url)
	require.NoError(t, err)
	resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

// TestOOBRestoreStdioLoopback verifies the stdio path mints a loopback URL.
func TestOOBRestoreStdioLoopback(t *testing.T) {
	o := NewOOBRestore(&fakeRestoreRunner{profile: "default"}, time.Minute)
	url := o.Register("default")
	require.True(t, strings.HasPrefix(url, "http://127.0.0.1:"), "expected loopback URL, got %q", url)

	// The form is reachable over the loopback listener.
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	defer o.Stop(context.Background())
}
