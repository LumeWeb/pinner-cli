package account

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer boots the fake account API and returns it + a client helper.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	return s, ts
}

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

func TestLoginReturnsToken(t *testing.T) {
	_, ts := newTestServer(t)
	// seed an account
	reg, err := http.Post(ts.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"email":"a@b.c","password":"pw","first_name":"A","last_name":"B"}`))
	if err != nil {
		t.Fatal(err)
	}
	reg.Body.Close()
	if reg.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d", reg.StatusCode)
	}

	resp, b := do(t, "POST", ts.URL+"/api/auth/login", "", strings.NewReader(`{"email":"a@b.c","password":"pw"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", resp.StatusCode, b)
	}
	var lr LoginResponse
	if err := json.Unmarshal(b, &lr); err != nil {
		t.Fatal(err)
	}
	if lr.Token == "" {
		t.Fatal("expected a token")
	}
}

func TestGetAccountRequiresAuth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, b := do(t, "GET", ts.URL+"/api/account", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestGetAccountWithToken(t *testing.T) {
	s, ts := newTestServer(t)
	// seed + remember token
	reg, _ := http.Post(ts.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"email":"a@b.c","password":"pw","first_name":"A","last_name":"B"}`))
	reg.Body.Close()
	tok := "token-a@b.c" // deterministic in this fake

	resp, b := do(t, "GET", ts.URL+"/api/account", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, b)
	}
	var acc AccountInfoResponse
	if err := json.Unmarshal(b, &acc); err != nil {
		t.Fatal(err)
	}
	if acc.Email != "a@b.c" {
		t.Fatalf("expected a@b.c, got %s", acc.Email)
	}
	_ = s
}

func TestUnimplementedEndpointReturns501(t *testing.T) {
	_, ts := newTestServer(t)
	// PATCH /api/account is not overridden -> clean 501, no panic
	resp, b := do(t, "PATCH", ts.URL+"/api/account", "", strings.NewReader(`{}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", resp.StatusCode, b)
	}
}
