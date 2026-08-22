package ipfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pinsTok returns a per-test fake bearer token (not a real credential).
func pinsTok(t *testing.T) string {
	return "pins-test-token/" + t.Name()
}

// newPins returns a fake content double with a seeded set of pins, wired to
// an httptest server. Seed order: pin-a (name "alpha"), pin-b (name "beta"),
// pin-c (name "charlie").
func newPins(t *testing.T) *httptest.Server {
	t.Helper()
	s := NewServer()
	s.AuthorizeToken(pinsTok(t))
	s.SeedPin("QmA", "alpha")
	s.SeedPin("QmB", "beta")
	s.SeedPin("QmC", "charlie")
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	return ts
}

func decodePins(t *testing.T, b []byte) PinResultsResponse {
	t.Helper()
	var list PinResultsResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("decode pins: %v body=%s", err, b)
	}
	return list
}

func TestPinsRequireAuth(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	cases := []struct{ method, path string }{
		{"GET", "/pins"},
		{"POST", "/pins"},
		{"GET", "/pins/req-QmX"},
		{"DELETE", "/pins/req-QmX"},
		{"POST", "/pins/req-QmX"},
	}
	for _, c := range cases {
		resp, _ := do(t, c.method, ts.URL+c.path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestGetPinsFiltersByCid(t *testing.T) {
	// This is the regression for the known fake gap: before the fix, GetPins
	// ignored the cid filter and returned every pin, so pinner-cli's
	// fetch-by-cid Status()/Unpin()/UpdatePin() took results[0] — the wrong
	// pin once more than one existed.
	ts := newPins(t)
	tok := pinsTok(t)

	resp, b := do(t, "GET", ts.URL+"/pins?cid=QmB", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	list := decodePins(t, b)
	if list.Count != 1 || len(list.Results) != 1 {
		t.Fatalf("cid filter: expected exactly 1 pin, got count=%d results=%d body=%s", list.Count, len(list.Results), b)
	}
	if list.Results[0].Pin.Cid != "QmB" || list.Results[0].Requestid != "req-QmB" {
		t.Fatalf("cid filter returned wrong pin: %+v", list.Results[0])
	}
}

func TestGetPinsFiltersByCidMulti(t *testing.T) {
	ts := newPins(t)
	tok := pinsTok(t)

	// Multiple CIDs: match if the pin carries any requested cid.
	resp, b := do(t, "GET", ts.URL+"/pins?cid=QmA&cid=QmC", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	list := decodePins(t, b)
	if list.Count != 2 {
		t.Fatalf("multi-cid filter: expected 2 pins, got count=%d body=%s", list.Count, b)
	}
	// Nonexistent cid -> empty result set.
	_, b = do(t, "GET", ts.URL+"/pins?cid=QmZZZ", tok, nil)
	list = decodePins(t, b)
	if resp.StatusCode != http.StatusOK || list.Count != 0 {
		t.Fatalf("unknown cid: expected empty, got count=%d status=%d body=%s", list.Count, resp.StatusCode, b)
	}
}

func TestGetPinsFiltersByNameAndMatch(t *testing.T) {
	ts := newPins(t)
	tok := pinsTok(t)

	// Exact name match (default strategy).
	_, b := do(t, "GET", ts.URL+"/pins?name=beta", tok, nil)
	list := decodePins(t, b)
	if list.Count != 1 || list.Results[0].Pin.Cid != "QmB" {
		t.Fatalf("exact name filter: expected 1 beta pin, got %+v body=%s", list, b)
	}

	// Exact name with no match should not substring-match.
	_, b = do(t, "GET", ts.URL+"/pins?name=bet", tok, nil)
	list = decodePins(t, b)
	if list.Count != 0 {
		t.Fatalf("exact name 'bet' should match nothing, got count=%d body=%s", list.Count, b)
	}

	// Partial name match (match=partial, used by pins_list search).
	_, b = do(t, "GET", ts.URL+"/pins?name=et&match=partial", tok, nil)
	list = decodePins(t, b)
	if list.Count != 1 || list.Results[0].Pin.Cid != "QmB" {
		t.Fatalf("partial name filter 'et': expected 1 beta pin, got %+v body=%s", list, b)
	}
}

func TestGetPinsFiltersByStatusAndLimit(t *testing.T) {
	s := NewServer()
	tok := pinsTok(t)
	s.AuthorizeToken(tok)
	// Give one pin a non-pinned status by writing directly to the store.
	s.SeedPin("QmA", "alpha")
	s.SeedPin("QmB", "beta")
	s.mu.Lock()
	s.pins["req-QmB"].Status = "failed"
	s.mu.Unlock()
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()

	// status filter excludes the failed pin.
	_, b := do(t, "GET", ts.URL+"/pins?status=pinned", tok, nil)
	list := decodePins(t, b)
	if list.Count != 1 || list.Results[0].Pin.Cid != "QmA" {
		t.Fatalf("status filter: expected 1 pinned, got %+v body=%s", list, b)
	}

	// limit caps the result count.
	_, b = do(t, "GET", ts.URL+"/pins?limit=1", tok, nil)
	list = decodePins(t, b)
	if list.Count != 1 || len(list.Results) != 1 {
		t.Fatalf("limit filter: expected 1 result, got count=%d results=%d body=%s", list.Count, len(list.Results), b)
	}
}

func TestPostPinsRequestidUpdatesPin(t *testing.T) {
	// pins_update backs onto POST /pins/{requestid} (boxo Replace).
	ts := newPins(t)
	tok := pinsTok(t)

	// Rename the QmB pin and set metadata.
	body := `{"cid":"QmB","name":"beta-renamed","meta":{"env":"prod","tier":"gold"}}`
	resp, b := do(t, "POST", ts.URL+"/pins/req-QmB", tok, strings.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.StatusCode, b)
	}
	var updated PinStatusResponse
	if err := json.Unmarshal(b, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Pin.Name == nil || *updated.Pin.Name != "beta-renamed" {
		t.Fatalf("name not updated: %+v", updated.Pin)
	}
	if updated.Pin.Meta == nil || (*updated.Pin.Meta)["env"] != "prod" {
		t.Fatalf("meta not updated: %+v", updated.Pin.Meta)
	}

	// Fetch by cid should now return the updated pin (regression: cid filter
	// must surface the renamed pin, not a stale whole-store results[0]).
	resp, b = do(t, "GET", ts.URL+"/pins?cid=QmB", tok, nil)
	list := decodePins(t, b)
	if list.Count != 1 || list.Results[0].Pin.Name == nil || *list.Results[0].Pin.Name != "beta-renamed" {
		t.Fatalf("fetch-by-cid after update returned stale pin: %+v body=%s", list, b)
	}
}

func TestPostPinsRequestidUnknown404(t *testing.T) {
	ts := newPins(t)
	tok := pinsTok(t)
	resp, b := do(t, "POST", ts.URL+"/pins/req-nope", tok, strings.NewReader(`{"name":"x"}`))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update unknown: expected 404, got %d body=%s", resp.StatusCode, b)
	}
}

func TestGetPinsRequestidFetch(t *testing.T) {
	ts := newPins(t)
	tok := pinsTok(t)
	resp, b := do(t, "GET", ts.URL+"/pins/req-QmC", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch status=%d body=%s", resp.StatusCode, b)
	}
	var got PinStatusResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Pin.Cid != "QmC" || got.Requestid != "req-QmC" {
		t.Fatalf("bad fetch-by-id: %+v", got)
	}

	// Unknown id -> 404.
	resp, b = do(t, "GET", ts.URL+"/pins/req-nope", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("fetch unknown: expected 404, got %d body=%s", resp.StatusCode, b)
	}
}

func TestDeletePinsRequestidRemoves(t *testing.T) {
	ts := newPins(t)
	tok := pinsTok(t)

	resp, b := do(t, "DELETE", ts.URL+"/pins/req-QmA", tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", resp.StatusCode, b)
	}

	// Removed pin no longer appears under the cid filter or the full list.
	_, b = do(t, "GET", ts.URL+"/pins?cid=QmA", tok, nil)
	list := decodePins(t, b)
	if list.Count != 0 {
		t.Fatalf("deleted pin still present: %+v body=%s", list, b)
	}
	_, b = do(t, "GET", ts.URL+"/pins", tok, nil)
	list = decodePins(t, b)
	if list.Count != 2 {
		t.Fatalf("after delete expected 2 pins, got count=%d body=%s", list.Count, b)
	}

	// Delete of unknown id -> 404.
	resp, b = do(t, "DELETE", ts.URL+"/pins/req-nope", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete unknown: expected 404, got %d body=%s", resp.StatusCode, b)
	}
}
