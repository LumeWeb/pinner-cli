package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.sia.tech/core/types"
)

// validAppKeyHexMCP returns a valid Sia private-key hex for driving the real
// Provisioner.Create in mcp tests (mcp-local mirror of the vault unit helper).
func validAppKeyHexMCP(t *testing.T) string {
	t.Helper()
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	return hex.EncodeToString(types.NewPrivateKeyFromSeed(seed))
}

// stubConnMCP is a ConnectionFlow returning a canned app key hex, mirroring the
// vault unit-test stub so mcp tests can drive the real Provisioner locally.
type stubConnMCP struct {
	appKeyHex string
}

func (s *stubConnMCP) Request(ctx context.Context) (string, error) { return "http://approve", nil }
func (s *stubConnMCP) WaitAndRegister(ctx context.Context) (string, error) {
	return s.appKeyHex, nil
}

// isoVaultPaths redirects the vault registry/seed/DB paths to a temp dir so
// config-touching vault operations in these tests never write to real user
// config (mirrors the isolation used across the mcp vault tests).
func isoVaultPaths(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
}

// The seed drop coordinator's Register/tokenDone/resolve behavior above is the
// live path the catalog-op create hand-off drives.

func TestSeedDropSingleUse(t *testing.T) {
	// Isolate vault paths: the confirmation hook touches the keep-seed file,
	// so the test must not write to real user config.
	isoVaultPaths(t)
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	url := d.Register("default", "alpha beta gamma")
	require.Contains(t, url, "/seed/")

	mux := http.NewServeMux()
	d.registerHandlers(mux)

	// GET renders the seed plus a confirmation form; it does NOT consume the
	// token, so a failed transport or prefetch never strands the human.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "alpha beta gamma")
	require.Contains(t, rec.Body.String(), `method="post"`, "the page must include a confirmation form")

	// A second GET before confirmation re-renders (retry is possible).
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec2.Code, "a GET before confirmation must re-render the seed")
	require.Contains(t, rec2.Body.String(), "alpha beta gamma")

	// A cross-origin POST is forged (CSRF) and must be rejected while the
	// token is still live, AND must not consume it.
	recForge := httptest.NewRecorder()
	forge := httptest.NewRequest(http.MethodPost, url, nil)
	forge.Header.Set("Origin", "http://evil.example")
	mux.ServeHTTP(recForge, forge)
	require.Equal(t, http.StatusForbidden, recForge.Code, "a cross-origin confirmation POST must be rejected")
	rec5 := httptest.NewRecorder()
	mux.ServeHTTP(rec5, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec5.Code, "a forged POST must not consume the token")
	require.Contains(t, rec5.Body.String(), "alpha beta gamma")

	// Only the explicit, same-origin confirmation POST consumes the drop.
	rec3 := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, url, nil)
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	mux.ServeHTTP(rec3, postReq)
	require.Equal(t, http.StatusOK, rec3.Code)

	// After confirmation the link is spent: a branded "link no longer active"
	// page (410), not the seed again and not a bare 404.
	rec4 := httptest.NewRecorder()
	mux.ServeHTTP(rec4, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusGone, rec4.Code, "a consumed seed link must render 410 with the spent page")
	require.Contains(t, rec4.Body.String(), "no longer active")
	require.NotContains(t, rec4.Body.String(), "alpha beta gamma", "the seed must never be shown after confirmation")
}

func TestSeedDropExpiry(t *testing.T) {
	isoVaultPaths(t)
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

// TestSeedDropTombstonePrunedOnWrite verifies the spent-tombstone map stays
// memory-bounded. Because the SeedDrop/OOBRestore coordinators never start the
// periodic reaper, pruning must happen lazily on the read/write path: when the
// map exceeds maxSpentTombstones, the oldest tombstones are evicted (FIFO) so
// the map is capped while any spent URL within retention still explains itself.
func TestSeedDropTombstonePrunedOnWrite(t *testing.T) {
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")

	// Fill the map past the cap with synthetic tombstones inserted oldest-first
	// (via markSpentLocked so the FIFO eviction index stays in sync).
	base := time.Now().Add(-10 * time.Hour)
	for i := 0; i < maxSpentTombstones+5; i++ {
		d.core.markSpentLocked(fmt.Sprintf("syn-%d", i), base.Add(time.Duration(i)*time.Second))
	}

	// Trigger lazy pruning on the write path.
	d.core.remove("does-not-matter")

	// The map is capped at maxSpentTombstones, and the OLDEST-inserted keys
	// (the FIFO head) were evicted while the newest remain — an O(1)/O(overflow)
	// oldest-first eviction, not a per-entry re-scan.
	require.Equal(t, maxSpentTombstones, len(d.core.spent))
	// The first 6 inserted (syn-0..syn-5) are the oldest and must be gone.
	require.NotContains(t, d.core.spent, "syn-0")
	require.NotContains(t, d.core.spent, "syn-5")
	// The newest synthetic tombstones are retained.
	require.Contains(t, d.core.spent, fmt.Sprintf("syn-%d", maxSpentTombstones+4))
	require.Contains(t, d.core.spent, fmt.Sprintf("syn-%d", maxSpentTombstones-1))
}

// TestSeedDropRetrievalClearsKeptSeed verifies the end-to-end post-confirmation
// cleanup hook: a create-backup (KeepSeed) seed registered in a SeedDrop is
// removed from disk, and its profile's KeepSeed marker cleared, only when the
// human confirms via POST — a mere GET (which cannot prove delivery) must leave
// the at-rest recovery copy intact.
func TestSeedDropRetrievalClearsKeptSeed(t *testing.T) {
	isoVaultPaths(t)

	// Create a real active keep-seed profile; the seed is on disk + marked.
	prov := vault.NewProvisioner()
	_, err := prov.Create(context.Background(), vault.CreateRequest{
		Profile:       "claimhook",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) vault.ConnectionFlow { return &stubConnMCP{appKeyHex: validAppKeyHexMCP(t)} },
	})
	require.NoError(t, err)
	seedPath := vault.SeedPath("claimhook")
	_, err = os.Stat(seedPath)
	require.NoError(t, err, "the kept seed must be on disk before retrieval")
	reg, err := vault.LoadRegistry()
	require.NoError(t, err)
	require.True(t, reg.Profiles["claimhook"].KeepSeed, "create must mark the profile as keep-seed")

	// Register the same seed in a SeedDrop.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	url := d.Register("claimhook", "fresh mnemonic from create")

	// GET renders the seed (delivery may or may not have happened) and must NOT
	// destroy the at-rest copy or clear KeepSeed.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "fresh mnemonic from create")
	_, statErr := os.Stat(seedPath)
	require.NoError(t, statErr, "a GET alone must not remove the kept seed")
	reg, err = vault.LoadRegistry()
	require.NoError(t, err)
	require.True(t, reg.Profiles["claimhook"].KeepSeed, "a GET alone must not clear keep-seed")

	// Only the human's explicit, same-origin confirmation POST removes the
	// at-rest copy and clears the KeepSeed marker.
	recPost := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, url, nil)
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	mux.ServeHTTP(recPost, postReq)
	require.Equal(t, http.StatusOK, recPost.Code)
	_, statErr = os.Stat(seedPath)
	require.True(t, os.IsNotExist(statErr), "the keep-seed must be removed once the human confirms")
	reg, err = vault.LoadRegistry()
	require.NoError(t, err)
	require.False(t, reg.Profiles["claimhook"].KeepSeed, "the keep-seed marker must be cleared once confirmed")
}

// failingWriter simulates a transport failure: every Write fails. Because GET
// never destroys the at-rest seed, a failed/partial GET must leave the recovery
// credential and token fully intact and retryable.
type failingWriter struct {
	header http.Header
}

func (f *failingWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *failingWriter) WriteHeader(int) {}
func (f *failingWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("simulated transport failure")
}

// TestSeedDropWriteFailureKeepsSeedRetryable verifies that a GET that fails to
// deliver (prefetch, link-expander, transport loss, attacker racing the URL)
// does NOT clear KeepSeed, delete the at-rest recovery seed, or consume the
// token. The human can retry and still claim the seed.
func TestSeedDropWriteFailureKeepsSeedRetryable(t *testing.T) {
	isoVaultPaths(t)

	// Create a real active keep-seed profile; the seed is on disk + marked.
	prov := vault.NewProvisioner()
	_, err := prov.Create(context.Background(), vault.CreateRequest{
		Profile:       "keeponfail",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) vault.ConnectionFlow { return &stubConnMCP{appKeyHex: validAppKeyHexMCP(t)} },
	})
	require.NoError(t, err)
	seedPath := vault.SeedPath("keeponfail")
	before, err := os.ReadFile(seedPath)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(before)))

	// Register a seeddrop and serve a GET whose Write always fails.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	url := d.Register("keeponfail", "mnemonic that must survive a failed write")

	mux.ServeHTTP(&failingWriter{}, httptest.NewRequest(http.MethodGet, url, nil))

	// Nothing was destroyed; the seed is untouched and still marked as keep.
	after, err := os.ReadFile(seedPath)
	require.NoError(t, err, "the kept seed must survive a failed write")
	require.Equal(t, before, after, "the seed bytes must be unchanged after a failed write")
	reg, err := vault.LoadRegistry()
	require.NoError(t, err)
	require.True(t, reg.Profiles["keeponfail"].KeepSeed, "keep-seed must not be cleared on a failed write")

	// The token is NOT consumed by the failed GET: a retry can still claim it.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code, "a failed GET must not consume the token; it is retryable")
	require.Contains(t, rec.Body.String(), "mnemonic that must survive a failed write")
}

// TestSeedDropConfirmFailureKeepsTokenLive verifies that when MarkSeedRetrieved
// fails to remove the at-rest recovery copy, consumePOST must NOT consume the
// token or falsely report removal: the human must be told the copy could not be
// deleted (500), the KeepSeed marker must survive, and the token must stay live
// so the failure is retryable rather than leaving the only recovery credential
// stranded.
func TestSeedDropConfirmFailureKeepsTokenLive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only dir chmod semantics differ on Windows")
	}
	isoVaultPaths(t)

	// Create a real active keep-seed profile; the seed is on disk + marked.
	prov := vault.NewProvisioner()
	_, err := prov.Create(context.Background(), vault.CreateRequest{
		Profile:       "keepfail",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) vault.ConnectionFlow { return &stubConnMCP{appKeyHex: validAppKeyHexMCP(t)} },
	})
	require.NoError(t, err)
	seedPath := vault.SeedPath("keepfail")
	before, err := os.ReadFile(seedPath)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(before)))

	// Register a seeddrop.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	url := d.Register("keepfail", "seed that must survive a failed removal")

	// Force MarkSeedRetrieved's os.Remove to fail by making the seed's own
	// parent directory read-only. The file exists, so Remove fails with a real
	// error (not NotExist), which MarkSeedRetrieved must surface rather than
	// swallow. The registry dir stays writable, so the KeepSeed marker is
	// cleared first — the on-disk recovery file is the critical thing that must
	// survive.
	seedDir := filepath.Dir(seedPath)
	require.NoError(t, os.Chmod(seedDir, 0o500))
	defer os.Chmod(seedDir, 0o700) // restore so isoVaultPaths cleanup can remove

	// Same-origin confirmation POST must surface a 500 and NOT consume.
	rec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, url, nil)
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	mux.ServeHTTP(rec, postReq)
	require.Equal(t, http.StatusInternalServerError, rec.Code, "a failed removal must render 500, not a success page")
	require.Contains(t, rec.Body.String(), "could <strong>not</strong> be removed", "the page must truthfully say removal failed")
	require.NotContains(t, rec.Body.String(), "has been removed", "the page must not falsely claim removal")

	// The at-rest copy must survive the failed removal.
	after, err := os.ReadFile(seedPath)
	require.NoError(t, err, "the at-rest copy must survive a failed removal")
	require.Equal(t, before, after, "the seed bytes must be unchanged after failed removal")

	// The token was NOT consumed: it is still retryable once the failure clears.
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, recGet.Code, "a failed confirmation must not consume the token; it stays live")
	require.Contains(t, recGet.Body.String(), "seed that must survive a failed removal")
}
