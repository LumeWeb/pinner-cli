package ipfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// webTok returns a per-test fake bearer token (not a real credential).
func webTok(t *testing.T) string {
	return "websites-test-token/" + t.Name()
}

// newWebsites returns a fake content double with a seeded website for the
// given domain, wired to an httptest server.
func newWebsites(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	s := NewServer()
	s.AuthorizeToken(webTok(t))
	seeded := s.SeedWebsite("seed.example.com", "QmSeed", "ipfs")
	if seeded == nil || seeded.Id == 0 {
		t.Fatalf("SeedWebsite returned bad result: %+v", seeded)
	}
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	return ts, strconv.Itoa(seeded.Id)
}

func TestWebsitesRequireAuth(t *testing.T) {
	s := NewServer()
	s.SeedWebsite("seed.example.com", "QmSeed", "ipfs")
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	cases := []struct{ method, path string }{
		{"GET", "/api/websites"},
		{"POST", "/api/websites"},
		{"GET", "/api/websites/1"},
		{"PUT", "/api/websites/1"},
		{"DELETE", "/api/websites/1"},
		{"GET", "/api/websites/config"},
		{"GET", "/api/websites/seed.example.com/ssl-status"},
		{"POST", "/api/websites/1/validate"},
		{"GET", "/api/websites/1/domains"},
		{"POST", "/api/websites/1/domains"},
		{"DELETE", "/api/websites/1/domains/1"},
		{"PATCH", "/api/websites/1/domains/1"},
		{"POST", "/api/websites/1/domains/1/verify"},
		{"GET", "/api/websites/1/domains/1/dns-requirements"},
		{"POST", "/api/websites/1/domains/1/dane/republish"},
	}
	for _, c := range cases {
		resp, _ := do(t, c.method, ts.URL+c.path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestWebsitesListSeeded(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	resp, b := do(t, "GET", ts.URL+"/api/websites", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, b)
	}
	var list WebsiteItemResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].Id != mustInt(t, id) {
		t.Fatalf("expected 1 website id=%s, got %+v", id, list)
	}
	if list.Data[0].Domain != "seed.example.com" || list.Data[0].TargetHash != "QmSeed" {
		t.Fatalf("unexpected seeded item: %+v", list.Data[0])
	}
}

func TestWebsitesGetSeeded(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	resp, b := do(t, "GET", ts.URL+"/api/websites/"+id, tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", resp.StatusCode, b)
	}
	var w WebsiteResponse
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.Domain != "seed.example.com" || w.Status != "active" || w.TargetType != "ipfs" {
		t.Fatalf("unexpected website: %+v", w)
	}
	if w.Ssl == nil || w.Ssl.Status != "ready" {
		t.Fatalf("seeded website should carry ready ssl, got %+v", w.Ssl)
	}

	// unknown id -> 404
	resp, _ = do(t, "GET", ts.URL+"/api/websites/999", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown status=%d want 404", resp.StatusCode)
	}
}

func TestWebsitesCreate(t *testing.T) {
	ts, _ := newWebsites(t)
	tok := webTok(t)

	body := `{"domain":"new.example.com","target_hash":"QmNew","target_type":"ipfs","dns_hosting_enabled":true}`
	resp, b := do(t, "POST", ts.URL+"/api/websites", tok, strings.NewReader(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}
	var w WebsiteResponse
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.Domain != "new.example.com" || w.TargetHash != "QmNew" || w.Status != "pending" {
		t.Fatalf("bad created website: %+v", w)
	}
	if !w.DnsHostingEnabled {
		t.Fatalf("expected dns_hosting_enabled=true, got %+v", w)
	}

	// list now has 2
	resp, b = do(t, "GET", ts.URL+"/api/websites", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, b)
	}
	var list WebsiteItemResponse
	_ = json.Unmarshal(b, &list)
	if list.Total != 2 {
		t.Fatalf("expected 2 websites after create, got %d", list.Total)
	}
}

func TestWebsitesUpdate(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	body := `{"domain":"renamed.example.com","target_hash":"QmRenamed","target_type":"ipfs"}`
	resp, b := do(t, "PUT", ts.URL+"/api/websites/"+id, tok, strings.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.StatusCode, b)
	}
	var w WebsiteResponse
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.Domain != "renamed.example.com" || w.TargetHash != "QmRenamed" {
		t.Fatalf("bad updated website: %+v", w)
	}
}

func TestWebsitesEnableIPNS(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	// enable-ipns -> PUT with target_type=ipns
	body := `{"target_type":"ipns"}`
	resp, b := do(t, "PUT", ts.URL+"/api/websites/"+id, tok, strings.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable ipns status=%d body=%s", resp.StatusCode, b)
	}
	var w WebsiteResponse
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.TargetType != "ipns" {
		t.Fatalf("expected target_type=ipns, got %q", w.TargetType)
	}
	if w.IpnsKeyId == nil || *w.IpnsKeyId == 0 {
		t.Fatalf("enable ipns should allocate ipns_key_id, got %+v", w.IpnsKeyId)
	}
}

func TestWebsitesDelete(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	resp, _ := do(t, "DELETE", ts.URL+"/api/websites/"+id, tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d want 204", resp.StatusCode)
	}
	// subsequent get -> 404
	resp, _ = do(t, "GET", ts.URL+"/api/websites/"+id, tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status=%d want 404", resp.StatusCode)
	}
}

func TestWebsitesConfig(t *testing.T) {
	ts, _ := newWebsites(t)
	tok := webTok(t)

	resp, b := do(t, "GET", ts.URL+"/api/websites/config", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d body=%s", resp.StatusCode, b)
	}
	var cfg WebsiteConfigResponse
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.GatewayDomain == nil || *cfg.GatewayDomain == "" {
		t.Fatalf("config should carry a gateway domain, got %+v", cfg)
	}
	if cfg.Nameservers == nil || len(*cfg.Nameservers) == 0 {
		t.Fatalf("config should carry nameservers, got %+v", cfg)
	}
}

func TestWebsitesSSLStatus(t *testing.T) {
	ts, _ := newWebsites(t)
	tok := webTok(t)

	// by apex domain
	resp, b := do(t, "GET", ts.URL+"/api/websites/seed.example.com/ssl-status", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ssl status status=%d body=%s", resp.StatusCode, b)
	}
	var w WebsiteResponse
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.Ssl == nil || w.Ssl.Status != "ready" {
		t.Fatalf("expected ready ssl status, got %+v", w.Ssl)
	}

	// unknown domain -> 404
	resp, _ = do(t, "GET", ts.URL+"/api/websites/unknown.example.com/ssl-status", tok, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ssl status unknown status=%d want 404", resp.StatusCode)
	}
}

func TestWebsitesValidate(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	resp, b := do(t, "POST", ts.URL+"/api/websites/"+id+"/validate", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", resp.StatusCode, b)
	}
	var v WebsiteValidateResponse
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Valid || v.Reason != "validated" || v.Domain != "seed.example.com" {
		t.Fatalf("unexpected validate response: %+v", v)
	}
}

func TestWebsitesDomainsFlow(t *testing.T) {
	ts, id := newWebsites(t)
	tok := webTok(t)

	// seeded website has one bound domain (its apex)
	resp, b := do(t, "GET", ts.URL+"/api/websites/"+id+"/domains", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list domains status=%d body=%s", resp.StatusCode, b)
	}
	var dl DomainListResponse
	if err := json.Unmarshal(b, &dl); err != nil {
		t.Fatal(err)
	}
	if dl.Total != 1 {
		t.Fatalf("expected 1 bound domain, got %+v", dl)
	}
	domainID := strconv.Itoa(dl.Data[0].Id)

	// add a secondary domain
	addBody := `{"domain":"www.seed.example.com","namespace":"icann"}`
	resp, b = do(t, "POST", ts.URL+"/api/websites/"+id+"/domains", tok, strings.NewReader(addBody))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add domain status=%d body=%s", resp.StatusCode, b)
	}
	var added DomainResponse
	if err := json.Unmarshal(b, &added); err != nil {
		t.Fatal(err)
	}
	if added.Domain != "www.seed.example.com" || added.Namespace != "icann" {
		t.Fatalf("bad added domain: %+v", added)
	}

	// list now 2
	resp, b = do(t, "GET", ts.URL+"/api/websites/"+id+"/domains", tok, nil)
	_ = json.Unmarshal(b, &dl)
	if dl.Total != 2 {
		t.Fatalf("expected 2 domains after add, got %+v", dl)
	}

	// dns-requirements on the new (secondary) domain
	resp, b = do(t, "GET", ts.URL+"/api/websites/"+id+"/domains/"+strconv.Itoa(added.Id)+"/dns-requirements", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dns-requirements status=%d body=%s", resp.StatusCode, b)
	}
	var req DomainResponse
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	if req.Delegation == nil || req.Delegation.Nameservers == nil || len(*req.Delegation.Nameservers) == 0 {
		t.Fatalf("dns-requirements should carry delegation nameservers, got %+v", req.Delegation)
	}

	// verify
	resp, b = do(t, "POST", ts.URL+"/api/websites/"+id+"/domains/"+strconv.Itoa(added.Id)+"/verify", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", resp.StatusCode, b)
	}
	var v DomainResponse
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Id != added.Id {
		t.Fatalf("verify returned wrong domain: %+v", v)
	}

	// dane-republish
	resp, b = do(t, "POST", ts.URL+"/api/websites/"+id+"/domains/"+strconv.Itoa(added.Id)+"/dane/republish", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dane-republish status=%d body=%s", resp.StatusCode, b)
	}
	var dan DomainDANERepublishResponse
	if err := json.Unmarshal(b, &dan); err != nil {
		t.Fatal(err)
	}
	if dan.TlsaRdata == nil || *dan.TlsaRdata != "3 1 1 ab12cd34ef56" {
		t.Fatalf("dane-republish should return tlsa rdata, got %+v", dan.TlsaRdata)
	}
	if !dan.PublishedToManagedZone {
		t.Fatal("dane-republish should report published_to_managed_zone=true")
	}

	// patch (update) the secondary domain's dns_hosting_enabled
	patchBody := `{"dns_hosting_enabled":false}`
	resp, b = do(t, "PATCH", ts.URL+"/api/websites/"+id+"/domains/"+strconv.Itoa(added.Id), tok, strings.NewReader(patchBody))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch domain status=%d body=%s", resp.StatusCode, b)
	}
	var patched DomainResponse
	if err := json.Unmarshal(b, &patched); err != nil {
		t.Fatal(err)
	}
	if patched.DnsHostingEnabled {
		t.Fatalf("expected dns_hosting_enabled=false after patch, got %+v", patched)
	}

	// delete the secondary domain
	resp, _ = do(t, "DELETE", ts.URL+"/api/websites/"+id+"/domains/"+strconv.Itoa(added.Id), tok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete domain status=%d want 204", resp.StatusCode)
	}
	resp, b = do(t, "GET", ts.URL+"/api/websites/"+id+"/domains", tok, nil)
	_ = json.Unmarshal(b, &dl)
	if dl.Total != 1 {
		t.Fatalf("expected 1 domain after delete, got %+v", dl)
	}

	// the original apex domain is still intact
	resp, b = do(t, "GET", ts.URL+"/api/websites/"+id+"/domains", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-list domains status=%d", resp.StatusCode)
	}
	var dl2 DomainListResponse
	_ = json.Unmarshal(b, &dl2)
	found := false
	for _, dd := range dl2.Data {
		if strconv.Itoa(dd.Id) == domainID {
			found = true
		}
	}
	if !found {
		t.Fatalf("apex domain id=%s should remain after secondary delete, got %+v", domainID, dl2.Data)
	}
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
