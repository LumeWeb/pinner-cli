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
	// Isolate vault paths: the retrieval hook shadows the at-rest keep-seed
	// file, so the test must not touch the real user config.
	isoVaultPaths(t)
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

// TestSeedDropRetrievalClearsKeptSeed verifies the end-to-end post-retrieval
// cleanup hook: a create-backup (KeepSeed) seed registered in a SeedDrop is
// removed from disk, and its profile's KeepSeed marker cleared, the moment the
// human claims it via the one-time GET — so the plaintext recovery mnemonic
// does not linger at rest indefinitely.
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

	// Register the same seed in a SeedDrop and claim it exactly once.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	url := d.Register("claimhook", "fresh mnemonic from create")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "fresh mnemonic from create")

	// The retrieval hook must have removed the at-rest keep-seed copy and
	// cleared the profile's KeepSeed marker.
	_, statErr := os.Stat(seedPath)
	require.True(t, os.IsNotExist(statErr), "the keep-seed must be removed once the human claims it")
	reg, err = vault.LoadRegistry()
	require.NoError(t, err)
	require.False(t, reg.Profiles["claimhook"].KeepSeed, "the keep-seed marker must be cleared once retrieved")
}

// failingWriter simulates a transport failure: every Write fails, so a GET
// against a seeddrop cannot be considered "retrieved" and must not destroy the
// only at-rest recovery copy.
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

// TestSeedDropWriteFailurePreservesKeptSeed verifies that MarkSeedRetrieved is
// only invoked after the seed text is confirmed written to the client. A failed
// Write (prefetch, link-expander, transport loss, attacker racing the URL) must
// NOT clear KeepSeed or delete the at-rest recovery seed, or the active vault
// would permanently lose its only recovery credential without the human ever
// receiving it.
func TestSeedDropWriteFailurePreservesKeptSeed(t *testing.T) {
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

	// Register a seeddrop for the same profile and serve a GET whose Write
	// always fails.
	d := NewSeedDrop(time.Minute)
	d.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	d.registerHandlers(mux)
	url := d.Register("keeponfail", "mnemonic that must survive a failed write")

	mux.ServeHTTP(&failingWriter{}, httptest.NewRequest(http.MethodGet, url, nil))

	// The seed file and KeepSeed marker must be untouched: the client never
	// confirmed receipt, so the recovery credential must remain.
	after, err := os.ReadFile(seedPath)
	require.NoError(t, err, "the kept seed must survive a failed write")
	require.Equal(t, before, after, "the seed bytes must be unchanged after a failed write")
	reg, err := vault.LoadRegistry()
	require.NoError(t, err)
	require.True(t, reg.Profiles["keeponfail"].KeepSeed, "keep-seed must not be cleared when the write fails")
}
