package ipfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// dnsTok returns a per-test fake bearer token (not a real credential; the
// gate only compares it against AuthorizeToken).
func dnsTok(t *testing.T) string {
	return "dns-test-token/" + t.Name()
}

func newDNS(t *testing.T, fn func(*Server)) *httptest.Server {
	t.Helper()
	s := NewServer()
	s.AuthorizeToken(dnsTok(t))
	if fn != nil {
		fn(s)
	}
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	return ts
}

func TestDnsZonesRequireAuth(t *testing.T) {
	ts := newDNS(t, nil)
	cases := []struct {
		method, path string
	}{
		{"GET", "/api/dns/zones"},
		{"POST", "/api/dns/zones"},
		{"GET", "/api/dns/zones/1"},
		{"PUT", "/api/dns/zones/1"},
		{"DELETE", "/api/dns/zones/1"},
		{"POST", "/api/dns/zones/1/validate"},
		{"GET", "/api/dns/zones/1/records"},
		{"POST", "/api/dns/zones/1/records"},
		{"GET", "/api/dns/zones/1/records/www/A"},
		{"PUT", "/api/dns/zones/1/records/www/A"},
		{"DELETE", "/api/dns/zones/1/records/www/A"},
	}
	for _, c := range cases {
		resp, _ := do(t, c.method, ts.URL+c.path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", c.method, c.path, resp.StatusCode)
		}
	}
}

// createZoneTest creates a zone via the API and returns its id.
func createZoneTest(t *testing.T, ts *httptest.Server, domain string) int {
	t.Helper()
	resp, b := do(t, "POST", ts.URL+"/api/dns/zones", dnsTok(t),
		strings.NewReader(`{"domain":"`+domain+`","nameservers":["ns1.example.com"]}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create zone status=%d body=%s", resp.StatusCode, b)
	}
	var z ZoneResponse
	if err := json.Unmarshal(b, &z); err != nil {
		t.Fatal(err)
	}
	if z.Domain != domain || z.Id == 0 {
		t.Fatalf("bad created zone: %+v", z)
	}
	return z.Id
}

func TestDnsZonesCRUD(t *testing.T) {
	ts := newDNS(t, nil)
	tok := dnsTok(t)

	// create
	id := createZoneTest(t, ts, "example.com")

	// list contains it
	resp, b := do(t, "GET", ts.URL+"/api/dns/zones", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, b)
	}
	var list ZoneListResponseResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].Id != id {
		t.Fatalf("expected 1 zone id=%d, got %+v", id, list)
	}

	// get
	resp, b = do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(id), tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", resp.StatusCode, b)
	}
	var z ZoneResponse
	if err := json.Unmarshal(b, &z); err != nil {
		t.Fatal(err)
	}
	if z.Domain != "example.com" {
		t.Fatalf("get domain=%q want example.com", z.Domain)
	}

	// get unknown -> 404
	resp, _ = do(t, "GET", ts.URL+"/api/dns/zones/999", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown status=%d want 404", resp.StatusCode)
	}

	// validate
	resp, b = do(t, "POST", ts.URL+"/api/dns/zones/"+strconv.Itoa(id)+"/validate", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", resp.StatusCode, b)
	}
	var v ValidationResponse
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Valid {
		t.Fatalf("expected valid=true, got %+v", v)
	}

	// delete
	resp, _ = do(t, "DELETE", ts.URL+"/api/dns/zones/"+strconv.Itoa(id), tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d want 204", resp.StatusCode)
	}
	// subsequent get -> 404
	resp, _ = do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(id), tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status=%d want 404", resp.StatusCode)
	}
}

func TestDnsRecordsCRUD(t *testing.T) {
	ts := newDNS(t, nil)
	tok := dnsTok(t)
	zid := createZoneTest(t, ts, "example.com")

	// create a record (TXT for www)
	body := `{"name":"www","type":"A","content":"1.2.3.4","ttl":120}`
	resp, b := do(t, "POST", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records", tok,
		strings.NewReader(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create record status=%d body=%s", resp.StatusCode, b)
	}
	var rec dnsRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Name != "www" || rec.Type != "A" || rec.Content != "1.2.3.4" || rec.ZoneId != zid {
		t.Fatalf("bad created record: %+v", rec)
	}
	if rec.Id == "" {
		t.Fatalf("record id must be non-empty: %+v", rec)
	}

	// list records
	resp, b = do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list records status=%d body=%s", resp.StatusCode, b)
	}
	var rl recordListResponse
	if err := json.Unmarshal(b, &rl); err != nil {
		t.Fatal(err)
	}
	if rl.Total != 1 || len(rl.Data) != 1 || rl.Data[0].Id != rec.Id {
		t.Fatalf("expected 1 record id=%s, got %+v", rec.Id, rl)
	}

	// get record by name+type
	resp, b = do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records/www/A", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get record status=%d body=%s", resp.StatusCode, b)
	}
	var got dnsRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Content != "1.2.3.4" {
		t.Fatalf("get record content=%q want 1.2.3.4", got.Content)
	}

	// update record (change content)
	resp, b = do(t, "PUT", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records/www/A", tok,
		strings.NewReader(`{"name":"www","type":"A","content":"5.6.7.8","ttl":300}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update record status=%d body=%s", resp.StatusCode, b)
	}
	var upd dnsRecord
	if err := json.Unmarshal(b, &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Content != "5.6.7.8" || upd.Ttl != 300 {
		t.Fatalf("updated record=%+v", upd)
	}

	// whole-RRSet delete (no content body)
	resp, _ = do(t, "DELETE", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records/www/A", tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete record status=%d want 204", resp.StatusCode)
	}
	// records list is now empty
	resp, b = do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list after delete status=%d", resp.StatusCode)
	}
	_ = json.Unmarshal(b, &rl)
	if rl.Total != 0 {
		t.Fatalf("expected 0 records after delete, got %d", rl.Total)
	}
}

func TestDnsRecordContentScopedDelete(t *testing.T) {
	ts := newDNS(t, nil)
	tok := dnsTok(t)
	zid := createZoneTest(t, ts, "example.com")

	// two rdata values for the same name+type
	for _, c := range []string{"1.1.1.1", "2.2.2.2"} {
		resp, b := do(t, "POST", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records", tok,
			strings.NewReader(`{"name":"www","type":"A","content":"`+c+`"}`))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s status=%d body=%s", c, resp.StatusCode, b)
		}
	}

	// delete only the 1.1.1.1 value via a content selector body
	resp, _ := do(t, "DELETE", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records/www/A", tok,
		strings.NewReader(`{"content":"1.1.1.1"}`))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("content delete status=%d want 204", resp.StatusCode)
	}

	resp, b := do(t, "GET", ts.URL+"/api/dns/zones/"+strconv.Itoa(zid)+"/records", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var rl recordListResponse
	_ = json.Unmarshal(b, &rl)
	if rl.Total != 1 || rl.Data[0].Content != "2.2.2.2" {
		t.Fatalf("expected only 2.2.2.2 to remain, got %+v", rl)
	}
}
