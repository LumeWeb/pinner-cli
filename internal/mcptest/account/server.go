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
	// nextID is the next account id.
	nextID int
}

// NewServer returns a fake account API double with empty state.
func NewServer() *Server {
	return &Server{
		Tokens:   map[string]*AccountInfoResponse{},
		accounts: map[string]*AccountInfoResponse{},
		nextID:   1,
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
	writeJSON(w, http.StatusOK, acc)
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
	}
	tok := "token-" + acc.Email
	s.Tokens[tok] = acc
	return tok
}

func timePtr(t time.Time) *time.Time { return &t }
