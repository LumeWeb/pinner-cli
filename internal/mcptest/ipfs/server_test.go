package ipfs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, method, url, token string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestPinRequiresAuth(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	resp, b := do(t, "GET", ts.URL+"/pins", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestAddAndListPin(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	tok := "token-x"

	// add a pin
	resp, b := do(t, "POST", ts.URL+"/pins", tok,
		strings.NewReader(`{"cid":"QmX","name":"my-pin"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", resp.StatusCode, b)
	}
	var created PinStatusResponse
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "pinned" || created.Pin.Cid != "QmX" {
		t.Fatalf("bad created: %+v", created)
	}

	// list pins
	resp, b = do(t, "GET", ts.URL+"/pins", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, b)
	}
	var list PinResultsResponse
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || len(list.Results) != 1 {
		t.Fatalf("expected 1 pin, got count=%d results=%d", list.Count, len(list.Results))
	}
	_ = s
}

func TestUnimplementedReturns501(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()
	// /api/dns/zones is not overridden -> 501, no panic
	resp, b := do(t, "GET", ts.URL+"/api/dns/zones", "", nil)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", resp.StatusCode, b)
	}
}
