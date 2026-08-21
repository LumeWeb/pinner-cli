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

	// account route: register -> token (POST /api/auth/register)
	resp, b := do(t, "POST", ts.URL+"/api/auth/register", "",
		strings.NewReader(`{"email":"a@b.c","password":"pw","first_name":"A","last_name":"B"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "token") {
		t.Fatalf("expected token in register response: %s", b)
	}

	// content route: pin listing (GET /pins) with the token from register
	tok := "token-a@b.c" // deterministic in the account fake
	resp, b = do(t, "GET", ts.URL+"/pins", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pins status=%d body=%s", resp.StatusCode, b)
	}
}
