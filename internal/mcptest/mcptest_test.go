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

func TestRouteTargetOf(t *testing.T) {
	cases := []struct {
		path string
		want routeTarget
	}{
		{"/api/account", targetAccount},
		{"/api/account/", targetAccount},
		{"/api/account/123", targetAccount},
		{"/api/auth/register", targetAccount},
		{"/api/auth/login", targetAccount},
		{"/api/billing/plan", targetAccount},
		{"/api/operations", targetAccount},
		{"/api/operations/42", targetAccount},
		{"/api/upload-limit", targetAccount},
		{"/api/upload-limit/", targetAccount},
		// content routes stay content even if they share a prefix with account.
		{"/pins", targetContent},
		{"/pins/123", targetContent},
		{"/api/ipfs", targetContent},
		{"/api/dns/zones", targetContent},
		{"/api/websites", targetContent},
		{"/api/operations-export", targetContent}, // not the account ops route
		{"/api/account-settings", targetContent},  // not the account area
		{"/", targetContent},
		{"", targetContent},
	}
	for _, c := range cases {
		if got := routeTargetOf(c.path); got != c.want {
			t.Errorf("routeTargetOf(%q) = %v, want %v", c.path, got, c.want)
		}
	}
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
