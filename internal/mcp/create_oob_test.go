package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCreateRunner implements CreateRunner for tests (no network): RunCreate
// generates a deterministic seed, invokes onApproval, and returns a fixed vault
// ID. Supports blocking on an approval via started/release channels.
type fakeCreateRunner struct {
	calls   int
	seed    string
	vaultID string
	err     error // when set, RunCreate returns it

	mu      sync.Mutex
	started chan struct{} // if non-nil, closed when RunCreate begins
	release chan struct{} // if non-nil, RunCreate blocks until closed
}

func (f *fakeCreateRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeCreateRunner) RunCreate(ctx context.Context, profile string, onApproval func(approvalURL string)) (string, string, string, error) {
	f.mu.Lock()
	f.calls++
	started, release := f.started, f.release
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return "", "", "", ctx.Err()
		}
	}
	if onApproval != nil {
		onApproval("http://approve.sia")
	}
	if f.err != nil {
		return "", "", "", f.err
	}
	seed := f.seed
	if seed == "" {
		seed = "fresh generated seed phrase"
	}
	vaultID := f.vaultID
	if vaultID == "" {
		vaultID = "vault-created-123"
	}
	return vaultID, seed, "/seed/path", nil
}

// buildCreateServer returns a wired OOBCreate (whose internal SeedDrop is also
// mounted on the returned mux) and a mux on which /create/ + /seed/ routes are
// served for direct testing, mirroring how the adapter mounts the shared SeedDrop.
func buildCreateServer() (*OOBCreate, *http.ServeMux, *fakeCreateRunner) {
	runner := &fakeCreateRunner{}
	seedDrop := NewSeedDrop(time.Minute)
	c := NewOOBCreate(runner, seedDrop, time.Minute)
	c.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	c.registerHandlers(mux)
	seedDrop.registerHandlers(mux)
	return c, mux, runner
}

func TestOOBCreateFormSingleUse(t *testing.T) {
	c, mux, runner := buildCreateServer()
	url := c.Register("default")
	require.Contains(t, url, "/create/")

	// GET serves the page.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Create Pinner Vault")
	require.Contains(t, rec.Body.String(), "Create vault")

	// POST from a foreign origin is rejected (CSRF).
	badReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader(""))
	badReq.Header.Set("Origin", "http://evil.example")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	require.Equal(t, http.StatusForbidden, badRec.Code)
	require.Equal(t, 0, runner.callCount(), "a CSRF-rejected POST must not run the create")
}

func TestOOBCreateStreamsApprovalAndSeed(t *testing.T) {
	c, mux, runner := buildCreateServer()
	url := c.Register("default")

	postReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader(""))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postReq)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, runner.callCount())

	body := rec.Body.String()
	require.Contains(t, body, "approve.sia", "the Sia approval URL must be streamed")
	require.Contains(t, body, "seed-link", "a seed retrieval link must be streamed")
	require.Contains(t, body, "Vault created")
	require.NotContains(t, body, "fresh generated seed phrase", "the plaintext seed must never be reflected on the page body machine-parseably; it is delivered on a separate one-time seed link")
}

func TestOOBCreateFailureDoesNotDeliverSeed(t *testing.T) {
	c, mux, runner := buildCreateServer()
	runner.err = assert.AnError
	url := c.Register("default")

	postReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader(""))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postReq)
	require.Equal(t, http.StatusOK, rec.Code) // streamed after start (status committed)
	body := rec.Body.String()
	require.Contains(t, body, "create-error")
	require.NotContains(t, body, "seed-link", "a failed create must not offer a seed link")

	// The token is consumed (re-POST is rejected). Foreign origin is CSRF-blocked
	// before token consumption, so use the matching origin.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, postReq.Clone(context.Background()))
	require.Equal(t, http.StatusGone, rec2.Code)
}
