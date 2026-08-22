package ipfs

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the contract-accurate test double for the Pinner content API
// (IPFS pinning service + a few gateway endpoints). It embeds serverStub
// (every endpoint returns 501 by default) and overrides the endpoints
// pinner-cli's MCP/CLI actually exercises with in-memory fake data.
//
// It enforces a bearer-token auth gate: endpoints that require auth return
// 401 when the Authorization header is absent or the token is unknown. Seed a
// valid token with AuthorizeToken (the account double is seeded first, and the
// harness propagates the same token here).
type Server struct {
	serverStub

	mu sync.Mutex

	// pins is an in-memory pin store keyed by request id.
	pins map[string]*PinStatusResponse
	// tokens is the set of bearer tokens accepted by the auth gate.
	tokens map[string]struct{}
	// zones is the in-memory DNS zone store keyed by numeric zone id.
	zones map[int]*ZoneResponse
	// zoneSeq is the monotonic zone id allocator.
	zoneSeq int
	// records holds per-zone DNS records keyed by a composite
	// name|type|content key so an RRSet can carry multiple rdata values.
	records map[int]map[string]*dnsRecord
	// recordSeq is the monotonic record id allocator.
	recordSeq int
	// websites is the in-memory website store keyed by numeric website id.
	websites map[int]*websiteSite
	// websiteSeq is the monotonic website id allocator.
	websiteSeq int
	// domainSeq is the monotonic bound-domain id allocator.
	domainSeq int
}

// NewServer returns a fake content API double with empty state.
func NewServer() *Server {
	return &Server{
		pins:       map[string]*PinStatusResponse{},
		tokens:     map[string]struct{}{},
		zones:      map[int]*ZoneResponse{},
		records:    map[int]map[string]*dnsRecord{},
		zoneSeq:    0,
		recordSeq:  0,
		websites:   map[int]*websiteSite{},
		websiteSeq: 0,
		domainSeq:  0,
	}
}

// AuthorizeToken adds a bearer token to the accepted set. The harness calls
// this with the same seeded account token so content calls authenticate
// through the same bearer token as account calls.
func (s *Server) AuthorizeToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = struct{}{}
}

func (s *Server) authorized(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tokens[strings.TrimPrefix(auth, prefix)]
	return ok
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// GetApiInfo returns basic node/peer info.
func (s *Server) GetApiInfo(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	writeJSON(w, http.StatusOK, InfoResponse{
		AnnouncementAddresses: []string{},
		ConnectionAddresses:   []string{},
		PeerId:                "fake-peer",
	})
}

// PostPins adds a pin (IPFS Pinning Service API).
func (s *Server) PostPins(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body PinRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Cid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cid is required"})
		return
	}
	reqID := "req-" + body.Cid
	s.mu.Lock()
	s.pins[reqID] = &PinStatusResponse{
		Created:   time.Now(),
		Requestid: reqID,
		Status:    "pinned",
		Pin:       body,
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, s.pin(reqID))
}

func (s *Server) pin(reqID string) *PinStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pins[reqID]
}

// GetPins lists pins (IPFS Pinning Service API).
func (s *Server) GetPins(w http.ResponseWriter, r *http.Request, params GetPinsParams) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	results := make([]PinStatusResponse, 0, len(s.pins))
	for _, p := range s.pins {
		results = append(results, *p)
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, PinResultsResponse{Count: len(results), Results: results})
}

// GetPinsRequestid returns a single pin (IPFS Pinning Service API).
func (s *Server) GetPinsRequestid(w http.ResponseWriter, r *http.Request, requestid string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	p := s.pin(requestid)
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pin not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}
