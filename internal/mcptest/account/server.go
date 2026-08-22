package account

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the contract-accurate test double for the Pinner account/portal
// API. It embeds serverStub (every endpoint returns 501 by default) and
// overrides the endpoints pinner-cli's MCP/CLI actually exercises with
// in-memory fake data.
//
// It enforces a bearer-token auth gate: endpoints that require auth return
// 401 when the Authorization header is absent or the token is unknown. Create
// a valid token via Login (or inject one directly into Tokens).
type Server struct {
	serverStub

	mu sync.Mutex

	// Tokens maps a valid bearer token -> the account it authenticates.
	Tokens map[string]*AccountInfoResponse
	// accounts stores registered accounts keyed by email.
	accounts map[string]*AccountInfoResponse
	// passwords stores each account's current password keyed by email, so the
	// update-email and update-password endpoints can verify the current
	// password before mutating the account (mirrors the real API contract).
	passwords map[string]string
	// operations holds seeded account operations (GET /api/operations).
	operations []OperationDetailResponse
	// nextID is the next account id.
	nextID int
}

// DefaultPassword is the password assigned to accounts created via Seed (which
// takes no password argument). The e2e harness references it when driving the
// account_update_email / account_update_password tools against the seeded
// account. Accounts registered via the register endpoint store the password
// supplied in the request body instead.
const DefaultPassword = "password"

// NewServer returns a fake account API double with empty state.
func NewServer() *Server {
	return &Server{
		Tokens:    map[string]*AccountInfoResponse{},
		accounts:  map[string]*AccountInfoResponse{},
		passwords: map[string]string{},
		nextID:    1,
	}
}

// authorize returns the account for the request's bearer token, or nil if the
// token is missing/unknown.
func (s *Server) authorize(r *http.Request) *AccountInfoResponse {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Tokens[strings.TrimPrefix(auth, prefix)]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// PostApiAuthRegister registers a new account and returns it.
func (s *Server) PostApiAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[body.Email]; exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		return
	}
	acc := &AccountInfoResponse{
		Id:        s.nextID,
		Email:     body.Email,
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Otp:       false,
		Verified:  true,
		CreatedAt: timePtr(time.Now()),
	}
	s.nextID++
	s.accounts[acc.Email] = acc
	s.passwords[acc.Email] = body.Password
	// give the new account a token
	tok := "token-" + acc.Email
	s.Tokens[tok] = acc
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(LoginResponse{Token: tok})
}

// PostApiAuthLogin logs in and returns a bearer token.
func (s *Server) PostApiAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.accounts[body.Email]
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	tok := "token-" + acc.Email
	s.Tokens[tok] = acc
	writeJSON(w, http.StatusOK, LoginResponse{Token: tok})
}

// GetApiAccount returns the authenticated account's info.
func (s *Server) GetApiAccount(w http.ResponseWriter, r *http.Request) {
	acc := s.authorize(r)
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	// authorize() hands back the aliased pointer that concurrent
	// PostApiAccountUpdateEmail mutates under the lock. Serialize a copy taken
	// under the lock so this handler never reads the account mid-mutation.
	s.mu.Lock()
	cp := *acc
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, &cp)
}

// PostApiAuthPing checks that the request is authenticated and returns a pong
// response. pinner's auth_status op pings this endpoint to confirm the stored
// token is valid, so it must be implemented for the status contract to hold.
func (s *Server) PostApiAuthPing(w http.ResponseWriter, r *http.Request) {
	acc := s.authorize(r)
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	token := strings.TrimPrefix(auth, prefix)
	writeJSON(w, http.StatusOK, PongResponse{Ping: "pong", Token: token})
}

// GetApiAccountKeys lists the authenticated account's API keys.
func (s *Server) GetApiAccountKeys(w http.ResponseWriter, r *http.Request, params GetApiAccountKeysParams) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	// In-memory fake: return an empty list matching the APIKeyListResponse
	// contract (keys are created dynamically by PostApiAccountKeys).
	writeJSON(w, http.StatusOK, APIKeyListResponse{Data: []APIKeyResponse{}, Total: 0})
}

// PostApiAccountKeys creates an API key for the authenticated account.
func (s *Server) PostApiAccountKeys(w http.ResponseWriter, r *http.Request) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body APIKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, APIKeyResponse{
		CreatedAt: time.Now(),
		Name:      body.Name,
		Uuid:      BinaryUUID{},
	})
}

// GetApiAccountBillingSubscription returns the authenticated account's
// subscription status. The fake models a deterministic "not subscribed"
// account (no active plan period, no gateway) so account_subscription reports
// the free tier.
func (s *Server) GetApiAccountBillingSubscription(w http.ResponseWriter, r *http.Request) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	writeJSON(w, http.StatusOK, SubscriptionStatusResponse{
		IsSubscribed: false,
	})
}

// PostApiAccountUpdateEmail changes the authenticated account's email,
// verifying the current password first (mirroring the real API, which sends a
// verification email to the new address). On success the stored email is
// updated and the updated account is returned.
func (s *Server) PostApiAccountUpdateEmail(w http.ResponseWriter, r *http.Request) {
	acc := s.authorize(r)
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body UpdateEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}
	if !s.verifyPassword(acc.Email, body.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldEmail := acc.Email
	if s.accounts[body.Email] != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		return
	}
	// Move the account under its new email key, carry over the password, and
	// mark the address changed (preserve the account id/pointer so existing
	// bearer tokens keep authenticating).
	delete(s.accounts, acc.Email)
	acc.Email = body.Email
	s.accounts[acc.Email] = acc
	s.passwords[acc.Email] = s.passwords[oldEmail]
	delete(s.passwords, oldEmail)
	// acc is aliased (its pointer lives in s.accounts and s.Tokens); serialize a
	// copy so a concurrent GetApiAccount reading it never races with this write.
	cp := *acc
	writeJSON(w, http.StatusOK, &cp)
}

// PostApiAccountUpdatePassword changes the authenticated account's password,
// verifying the current password first.
func (s *Server) PostApiAccountUpdatePassword(w http.ResponseWriter, r *http.Request) {
	acc := s.authorize(r)
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password is required"})
		return
	}
	if !s.verifyPassword(acc.Email, body.CurrentPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid current password"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.passwords[acc.Email] = body.NewPassword
	writeJSON(w, http.StatusOK, map[string]string{"message": "password updated"})
}

// verifyPassword reports whether pw matches the account's stored password.
func (s *Server) verifyPassword(email, pw string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.passwords[email] == pw
}

// Seed registers a deterministic account (if not already present) and returns
// its bearer token. It lets an e2e harness pre-provision a valid session so
// pinner boots with a ready-made auth_token against the fake API.
func (s *Server) Seed(email, firstName, lastName string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, exists := s.accounts[email]
	if !exists {
		acc = &AccountInfoResponse{
			Id:        s.nextID,
			Email:     email,
			FirstName: firstName,
			LastName:  lastName,
			Otp:       false,
			Verified:  true,
			CreatedAt: timePtr(time.Now()),
		}
		s.nextID++
		s.accounts[acc.Email] = acc
		s.passwords[acc.Email] = DefaultPassword
	}
	tok := "token-" + acc.Email
	s.Tokens[tok] = acc
	return tok
}

// SeedOperations seeds a small deterministic set of account operations so the
// operations_* tools have real data to read (GET /api/operations).
func (s *Server) SeedOperations() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.operations = []OperationDetailResponse{
		{
			Id:                    1,
			Operation:             "pin",
			OperationDisplayName:  "Pin",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			Status:                "completed",
			StatusDisplayName:     "Completed",
			StatusMessage:         "Pinned successfully",
			ProgressPercent:       100,
			StartedAt:             now.Add(-2 * time.Hour),
			UpdatedAt:             now.Add(-90 * time.Minute),
			CurrentStep:           intPtr(4),
			TotalSteps:            intPtr(4),
		},
		{
			Id:                    2,
			Operation:             "upload",
			OperationDisplayName:  "Upload",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			Status:                "running",
			StatusDisplayName:     "Running",
			StatusMessage:         "Uploading file",
			ProgressPercent:       45,
			StartedAt:             now.Add(-10 * time.Minute),
			UpdatedAt:             now,
			CurrentStep:           intPtr(2),
			TotalSteps:            intPtr(5),
		},
	}
}

// GetApiOperations lists account operations (GET /api/operations).
func (s *Server) GetApiOperations(w http.ResponseWriter, r *http.Request, params GetApiOperationsParams) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	data := make([]OperationListItem, 0, len(s.operations))
	for _, op := range s.operations {
		item := OperationListItem{
			Cid:                  op.Cid,
			CurrentStep:          op.CurrentStep,
			Error:                op.Error,
			EstimatedCompletionAt: op.EstimatedCompletionAt,
			Id:                   op.Id,
			Operation:            op.Operation,
			OperationDisplayName: op.OperationDisplayName,
			ProgressPercent:      op.ProgressPercent,
			Protocol:             op.Protocol,
			ProtocolDisplayName:  op.ProtocolDisplayName,
			StartedAt:            op.StartedAt,
			Status:               OperationListItemStatus(op.Status),
			StatusDisplayName:    op.StatusDisplayName,
			StatusMessage:        op.StatusMessage,
			TotalSteps:           op.TotalSteps,
			UpdatedAt:            op.UpdatedAt,
		}
		data = append(data, item)
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, OperationListItemResponse{Data: data, Total: total})
}

// GetApiOperationsId returns a single operation's detail
// (GET /api/operations/{id}).
func (s *Server) GetApiOperationsId(w http.ResponseWriter, r *http.Request, id int) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	var found *OperationDetailResponse
	for i := range s.operations {
		if s.operations[i].Id == id {
			cp := s.operations[i]
			found = &cp
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, *found)
}

// GetApiOperationsFilters returns the filter dims for operations
// (GET /api/operations/filters).
func (s *Server) GetApiOperationsFilters(w http.ResponseWriter, r *http.Request) {
	if s.authorize(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	resp := OperationFiltersResponseResponse{
		Data: OperationFiltersResponse{
			Data: OperationFiltersResponseData{
				Operations: []OperationFilterItem{
					{Name: "pin", Value: "pin", Description: strPtr("Pin operation")},
					{Name: "upload", Value: "upload", Description: strPtr("Upload operation")},
				},
				Protocols: []OperationFilterItem{
					{Name: "ipfs", Value: "ipfs", Description: strPtr("IPFS protocol")},
				},
				Statuses: []OperationFilterItem{
					{Name: "completed", Value: "completed", Description: strPtr("Completed")},
					{Name: "running", Value: "running", Description: strPtr("Running")},
				},
			},
		},
		Total: 2,
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

func timePtr(t time.Time) *time.Time { return &t }
