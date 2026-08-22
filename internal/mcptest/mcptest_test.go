package mcptest

import (
	"io"
	"net/http"
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

func TestCombinedDispatcher(t *testing.T) {
	s := New()
	ts := s.Start()
	defer ts.Close()

	// Seed an account that both account and content doubles accept.
	tok := s.Seed("seed@x.c", "S", "D")

	// account route: register -> a distinct deterministic token (POST /api/auth/register)
	resp, b := do(t, "POST", ts.URL+"/api/auth/register", "",
		strings.NewReader(`{"email":"a@b.c","password":"pw","first_name":"A","last_name":"B"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "token") {
		t.Fatalf("expected token in register response: %s", b)
	}

	// content route: pin listing (GET /pins) with the seeded, authorized token
	resp, b = do(t, "GET", ts.URL+"/pins", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pins status=%d body=%s", resp.StatusCode, b)
	}

	// a content request with an unknown token must be rejected (401)
	resp, b = do(t, "GET", ts.URL+"/pins", tok+"-bogus", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pins with unknown token status=%d body=%s", resp.StatusCode, b)
	}

	// bare /api/account (no trailing slash) must route to the account double
	resp, b = do(t, "GET", ts.URL+"/api/account", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("account status=%d body=%s", resp.StatusCode, b)
	}
}
