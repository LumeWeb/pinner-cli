package ipfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ipnsTok returns a per-test fake bearer token (not a real credential).
func ipnsTok(t *testing.T) string {
	return "ipns-test-token/" + t.Name()
}

// newIPNS returns a fake content double with a seeded IPNS key, wired to an
// httptest server. Returns the server URL and the seeded key id as a string.
func newIPNS(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	s := NewServer()
	s.AuthorizeToken(ipnsTok(t))
	seeded := s.SeedIPNSKey("seed-key")
	if seeded == nil || seeded.Id == 0 {
		t.Fatalf("SeedIPNSKey returned bad result: %+v", seeded)
	}
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	return ts, strconv.Itoa(seeded.Id)
}

func TestIPNSRequireAuth(t *testing.T) {
	s := NewServer()
	s.SeedIPNSKey("seed-key")
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	cases := []struct{ method, path string }{
		{"GET", "/api/ipns/keys"},
		{"POST", "/api/ipns/keys"},
		{"GET", "/api/ipns/keys/1"},
		{"DELETE", "/api/ipns/keys/1"},
		{"POST", "/api/ipns/keys/1/republish"},
		{"POST", "/api/ipns/publish"},
		{"GET", "/api/ipns/resolve/k51qzi5uqu5dgv00000001seed"},
	}
	for _, c := range cases {
		resp, _ := do(t, c.method, ts.URL+c.path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestIPNSKeyListSeeded(t *testing.T) {
	ts, id := newIPNS(t)
	tok := ipnsTok(t)

	resp, b := do(t, "GET", ts.URL+"/api/ipns/keys", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, b)
	}
	var list IPNSKeyListResponseResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].Id != mustInt(t, id) {
		t.Fatalf("expected 1 key id=%s, got %+v", id, list)
	}
	if list.Data[0].Name != "seed-key" || list.Data[0].IpnsName == "" {
		t.Fatalf("unexpected seeded key: %+v", list.Data[0])
	}
}

func TestIPNSKeyListSearchFilter(t *testing.T) {
	ts, _ := newIPNS(t)
	tok := ipnsTok(t)
	// add a second key that should be filtered out by the contains search
	post := strings.NewReader(`{"name":"other-key"}`)
	if resp, b := do(t, "POST", ts.URL+"/api/ipns/keys", tok, post); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}

	resp, b := do(t, "GET", ts.URL+"/api/ipns/keys?filters[name][contains]=seed", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d body=%s", resp.StatusCode, b)
	}
	var list IPNSKeyListResponseResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Data[0].Name != "seed-key" {
		t.Fatalf("expected only seed-key, got %+v", list)
	}
}

func TestIPNSKeyCreateGetDelete(t *testing.T) {
	ts, _ := newIPNS(t)
	tok := ipnsTok(t)

	// create
	resp, b := do(t, "POST", ts.URL+"/api/ipns/keys", tok, strings.NewReader(`{"name":"my-new-key"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}
	var created IPNSKeyResponse
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "my-new-key" || created.Id == 0 || created.IpnsName == "" {
		t.Fatalf("bad created key: %+v", created)
	}
	newID := strconv.Itoa(created.Id)

	// get
	resp, b = do(t, "GET", ts.URL+"/api/ipns/keys/"+newID, tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", resp.StatusCode, b)
	}
	var got IPNSKeyResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Id != created.Id || got.Name != "my-new-key" {
		t.Fatalf("bad get: %+v", got)
	}

	// delete
	resp, b = do(t, "DELETE", ts.URL+"/api/ipns/keys/"+newID, tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", resp.StatusCode, b)
	}

	// get after delete -> 404
	resp, b = do(t, "GET", ts.URL+"/api/ipns/keys/"+newID, tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d body=%s", resp.StatusCode, b)
	}
}

func TestIPNSKeyCreateRequiresName(t *testing.T) {
	ts, _ := newIPNS(t)
	tok := ipnsTok(t)
	resp, b := do(t, "POST", ts.URL+"/api/ipns/keys", tok, strings.NewReader(`{"name":""}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d body=%s", resp.StatusCode, b)
	}
}

func TestIPNSPublishResolveRepublish(t *testing.T) {
	ts, id := newIPNS(t)
	tok := ipnsTok(t)

	// publish a CID under the seeded key (key_id = seeded id)
	body := `{"cid":"QmPublish","key_id":` + id + `}`
	resp, b := do(t, "POST", ts.URL+"/api/ipns/publish", tok, strings.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", resp.StatusCode, b)
	}
	var pub IPNSPublishResponse
	if err := json.Unmarshal(b, &pub); err != nil {
		t.Fatal(err)
	}
	if pub.Value != "QmPublish" || pub.Name == "" || pub.Sequence == 0 {
		t.Fatalf("bad publish response: %+v", pub)
	}

	// resolve the key's ipns name -> the published cid
	name := pub.Name
	resp, b = do(t, "GET", ts.URL+"/api/ipns/resolve/"+name, tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resp.StatusCode, b)
	}
	var res IPNSResolveResponse
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	if res.Value != "QmPublish" || res.Name != name || res.Path != "/ipns/"+name {
		t.Fatalf("bad resolve: %+v", res)
	}

	// republish under the seeded key -> echoes success
	resp, b = do(t, "POST", ts.URL+"/api/ipns/keys/"+id+"/republish", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("republish status=%d body=%s", resp.StatusCode, b)
	}
	var repub IPNSRepublishResponse
	if err := json.Unmarshal(b, &repub); err != nil {
		t.Fatal(err)
	}
	if repub.Count != 1 || repub.Message == "" {
		t.Fatalf("bad republish: %+v", repub)
	}
}

func TestIPNSPublishUnknownKey(t *testing.T) {
	ts, _ := newIPNS(t)
	tok := ipnsTok(t)
	resp, b := do(t, "POST", ts.URL+"/api/ipns/publish", tok, strings.NewReader(`{"cid":"QmX","key_id":9999}`))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown key, got %d body=%s", resp.StatusCode, b)
	}
}

func TestIPNSResolveUnknownName(t *testing.T) {
	ts, _ := newIPNS(t)
	tok := ipnsTok(t)
	resp, b := do(t, "GET", ts.URL+"/api/ipns/resolve/nonexistent", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown name, got %d body=%s", resp.StatusCode, b)
	}
}
