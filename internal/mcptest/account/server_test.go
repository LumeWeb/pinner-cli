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

// registerAccount seeds a deterministic account through the register endpoint
// and returns its token. The registered account's password is whatever the
// caller supplied in the body.
func registerAccount(t *testing.T, ts *httptest.Server, email, password string) string {
	t.Helper()
	reg, err := http.Post(ts.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"email":"`+email+`","password":"`+password+`","first_name":"A","last_name":"B"}`))
	if err != nil {
		t.Fatal(err)
	}
	reg.Body.Close()
	if reg.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d", reg.StatusCode)
	}
	return "token-" + email // deterministic in this fake
}

func TestUpdateSubscriptionRequiresAuth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, b := do(t, "GET", ts.URL+"/api/account/billing/subscription", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestUpdateSubscriptionNotSubscribed(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "sub@example.com", "pw")
	resp, b := do(t, "GET", ts.URL+"/api/account/billing/subscription", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, b)
	}
	var sub SubscriptionStatusResponse
	if err := json.Unmarshal(b, &sub); err != nil {
		t.Fatal(err)
	}
	if sub.IsSubscribed {
		t.Fatal("expected free account to be is_subscribed=false")
	}
}

func TestUpdateEmailRequiresAuth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, b := do(t, "POST", ts.URL+"/api/account/update-email", "",
		strings.NewReader(`{"email":"new@example.com","password":"pw"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestUpdateEmailSuccess(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "old@example.com", "pw")
	resp, b := do(t, "POST", ts.URL+"/api/account/update-email", tok,
		strings.NewReader(`{"email":"new@example.com","password":"pw"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, b)
	}
	var acc AccountInfoResponse
	if err := json.Unmarshal(b, &acc); err != nil {
		t.Fatal(err)
	}
	if acc.Email != "new@example.com" {
		t.Fatalf("expected email new@example.com, got %s", acc.Email)
	}
	// The account should be retrievable under the new email via GET /api/account
	// using the same (unchanged) token.
	ga, gb := do(t, "GET", ts.URL+"/api/account", tok, nil)
	if ga.StatusCode != http.StatusOK {
		t.Fatalf("get account after email change status=%d body=%s", ga.StatusCode, gb)
	}
	var got AccountInfoResponse
	if err := json.Unmarshal(gb, &got); err != nil {
		t.Fatal(err)
	}
	if got.Email != "new@example.com" {
		t.Fatalf("expected persisted email new@example.com, got %s", got.Email)
	}
}

func TestUpdateEmailWrongPassword(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "old@example.com", "pw")
	resp, b := do(t, "POST", ts.URL+"/api/account/update-email", tok,
		strings.NewReader(`{"email":"new@example.com","password":"wrong"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestUpdatePasswordRequiresAuth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, b := do(t, "POST", ts.URL+"/api/account/update-password", "",
		strings.NewReader(`{"current_password":"pw","new_password":"newpw"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestUpdatePasswordSuccess(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "pw@example.com", "pw")
	resp, b := do(t, "POST", ts.URL+"/api/account/update-password", tok,
		strings.NewReader(`{"current_password":"pw","new_password":"newpw"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, b)
	}
	// Old password no longer works; new one does.
	resp2, b2 := do(t, "POST", ts.URL+"/api/account/update-password", tok,
		strings.NewReader(`{"current_password":"pw","new_password":"x"}`))
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected old password rejected 401, got %d body=%s", resp2.StatusCode, b2)
	}
	resp3, b3 := do(t, "POST", ts.URL+"/api/account/update-password", tok,
		strings.NewReader(`{"current_password":"newpw","new_password":"x"}`))
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected new password accepted 200, got %d body=%s", resp3.StatusCode, b3)
	}
}

func TestUpdatePasswordWrongCurrent(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "pw@example.com", "pw")
	resp, b := do(t, "POST", ts.URL+"/api/account/update-password", tok,
		strings.NewReader(`{"current_password":"wrong","new_password":"newpw"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, b)
	}
}

func TestOperationsListAndGet(t *testing.T) {
	_, ts := newTestServer(t)
	tok := registerAccount(t, ts, "ops@example.com", "pw")

	// List operations (no seed -> empty is a valid happy path; then seed and
	// assert real rows come back).
	resp, b := do(t, "GET", ts.URL+"/api/operations", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: expected 200, got %d body=%s", resp.StatusCode, b)
	}

	// With a seeded store, list returns the two seeded operations with real fields.
	registerAccount(t, ts, "ops2@example.com", "pw")
	// Seed operations on the server directly.
	// (registerAccount creates a fresh server via newTestServer; here we just
	// exercise the seeded path through the mcptest Seed composition instead.)
	s := NewServer()
	ts2 := httptest.NewServer(Handler(s))
	t.Cleanup(ts2.Close)
	seedTok := s.Seed("seed@example.com", "E2E", "Seed")
	s.SeedOperations()

	rl, bl := do(t, "GET", ts2.URL+"/api/operations", seedTok, nil)
	if rl.StatusCode != http.StatusOK {
		t.Fatalf("seeded list: expected 200, got %d body=%s", rl.StatusCode, bl)
	}
	var list OperationListItemResponse
	if err := json.Unmarshal(bl, &list); err != nil {
		t.Fatalf("list unmarshal: %v body=%s", err, bl)
	}
	if list.Total != 2 {
		t.Fatalf("expected total 2, got %d", list.Total)
	}
	if len(list.Data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Data))
	}
	// An operation for id 1 exists with real display fields.
	if list.Data[0].Id != 1 || list.Data[0].Operation != "pin" || list.Data[0].StatusDisplayName == "" {
		t.Fatalf("unexpected first operation: %+v", list.Data[0])
	}

	// Get by id returns the detail for the real seeded row.
	rd, bd := do(t, "GET", ts2.URL+"/api/operations/1", seedTok, nil)
	if rd.StatusCode != http.StatusOK {
		t.Fatalf("get by id: expected 200, got %d body=%s", rd.StatusCode, bd)
	}
	var detail OperationDetailResponse
	if err := json.Unmarshal(bd, &detail); err != nil {
		t.Fatalf("detail unmarshal: %v body=%s", err, bd)
	}
	if detail.Id != 1 || detail.Status != "completed" {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	// Unknown id -> 404, not a 200 empty.
	r404, b404 := do(t, "GET", ts2.URL+"/api/operations/999", seedTok, nil)
	if r404.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: expected 404, got %d body=%s", r404.StatusCode, b404)
	}

	// Unauthenticated list -> 401.
	r401, _ := do(t, "GET", ts2.URL+"/api/operations", "", nil)
	if r401.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth list: expected 401, got %d", r401.StatusCode)
	}
}

func TestOperationsFilters(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(Handler(s))
	t.Cleanup(ts.Close)
	tok := s.Seed("ops@example.com", "E2E", "Seed")
	s.SeedOperations()

	resp, b := do(t, "GET", ts.URL+"/api/operations/filters", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filters: expected 200, got %d body=%s", resp.StatusCode, b)
	}
	var f OperationFiltersResponseResponse
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("filters unmarshal: %v body=%s", err, b)
	}
	if f.Total != 2 || len(f.Data.Data.Operations) == 0 {
		t.Fatalf("unexpected filters: %+v", f)
	}
}

