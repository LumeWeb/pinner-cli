package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The seed drop coordinator's Register/tokenDone/resolve behavior above is the
// live path the catalog-op create hand-off drives.

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
