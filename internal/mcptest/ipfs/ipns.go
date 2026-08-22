package ipfs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ipnsNameFor derives the IPNS name (peer-id style base58 identifier) for a
// key. It is deliberately deterministic so resolve against the seeded key has
// stable data.
func ipnsNameFor(id int) string {
	return "k51qzi5uqu5dgv" + fmt.Sprintf("%08d", id) + "seed"
}

// newIPNSKeyLocked allocates and stores a key. The caller must hold s.mu.
func (s *Server) newIPNSKeyLocked(name string) *IPNSKeyResponse {
	s.keySeq++
	now := time.Now().UTC()
	key := &IPNSKeyResponse{
		Created:  now,
		Id:       s.keySeq,
		IpnsName: ipnsNameFor(s.keySeq),
		Name:     name,
		PeerId:   ipnsNameFor(s.keySeq),
	}
	s.ipnsKeys[key.Id] = key
	return key
}

// SeedIPNSKey creates and stores an IPNS key without going through the HTTP
// API, so list/get return data for the seeded default token. Returns the
// created key.
func (s *Server) SeedIPNSKey(name string) *IPNSKeyResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newIPNSKeyLocked(name)
}

// GetApiIpnsKeys lists IPNS keys for the authenticated user
// (GET /api/ipns/keys). The portal endpoint is a queryutil list endpoint, so
// the list tool's server-side name search arrives as filters[name][contains].
func (s *Server) GetApiIpnsKeys(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	filter := r.URL.Query().Get("filters[name][contains]")
	s.mu.Lock()
	data := make([]IPNSKeyListResponse, 0, len(s.ipnsKeys))
	for _, k := range s.ipnsKeys {
		if filter != "" && !strings.Contains(k.Name, filter) {
			continue
		}
		data = append(data, IPNSKeyListResponse{
			Created:         k.Created,
			Id:              k.Id,
			IpnsName:        k.IpnsName,
			LastPublishedAt: k.LastPublishedAt,
			Name:            k.Name,
			PeerId:          k.PeerId,
			Value:           k.Value,
		})
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, IPNSKeyListResponseResponse{Data: data, Total: total})
}

// PostApiIpnsKeys creates a new IPNS key (POST /api/ipns/keys).
func (s *Server) PostApiIpnsKeys(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body IPNSKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	s.mu.Lock()
	key := s.newIPNSKeyLocked(body.Name)
	// An imported key carries its private key value through to the response.
	if body.Key != nil && *body.Key != "" {
		v := *body.Key
		key.Value = &v
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, key)
}

// ipnsKeyByID resolves a key by numeric id path param, returning a notFound
// bool when the raw value is non-numeric or unknown.
func (s *Server) ipnsKeyByID(idParam string) (*IPNSKeyResponse, bool) {
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.ipnsKeys[id]
	return k, ok
}

// GetApiIpnsKeysId returns a single IPNS key (GET /api/ipns/keys/{id}).
func (s *Server) GetApiIpnsKeysId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	key, ok := s.ipnsKeyByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, key)
}

// DeleteApiIpnsKeysId deletes an IPNS key (DELETE /api/ipns/keys/{id}).
func (s *Server) DeleteApiIpnsKeysId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	key, ok := s.ipnsKeyByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	delete(s.ipnsKeys, key.Id)
	delete(s.ipnsRecords, key.IpnsName)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// PostApiIpnsPublish publishes a CID under an IPNS key
// (POST /api/ipns/publish).
func (s *Server) PostApiIpnsPublish(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body IPNSPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Cid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cid is required"})
		return
	}
	s.mu.Lock()
	key, ok := s.ipnsKeys[body.KeyId]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}
	now := time.Now().UTC()
	key.LastPublishedAt = &now
	v := body.Cid
	key.Value = &v
	s.ipnsRecords[key.IpnsName] = body.Cid
	seq := key.Id
	s.mu.Unlock()

	name := key.IpnsName
	var validity time.Time
	if body.Ttl != nil && *body.Ttl != "" {
		if d, err := time.ParseDuration(*body.Ttl); err == nil {
			validity = now.Add(d)
		}
	}
	if validity.IsZero() {
		validity = now.Add(24 * time.Hour)
	}
	writeJSON(w, http.StatusOK, IPNSPublishResponse{
		Name:      name,
		Published: now,
		Sequence:  seq * 1000,
		Validity:  validity,
		Value:     body.Cid,
	})
}

// PostApiIpnsKeysIdRepublish republishes an existing IPNS record for a key
// (POST /api/ipns/keys/{id}/republish).
func (s *Server) PostApiIpnsKeysIdRepublish(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	key, ok := s.ipnsKeyByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	existing, hasRecord := s.ipnsRecords[key.IpnsName]
	now := time.Now().UTC()
	key.LastPublishedAt = &now
	s.mu.Unlock()
	if !hasRecord || existing == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key has no record to republish"})
		return
	}
	writeJSON(w, http.StatusOK, IPNSRepublishResponse{
		Count:   1,
		Message: fmt.Sprintf("successfully republished key %s", key.IpnsName),
	})
}

// GetApiIpnsResolveName resolves an IPNS name to a CID
// (GET /api/ipns/resolve/{name}).
func (s *Server) GetApiIpnsResolveName(w http.ResponseWriter, r *http.Request, name string, params GetApiIpnsResolveNameParams) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	cid, ok := s.ipnsRecords[name]
	if !ok {
		s.mu.Unlock()
		writeNotFound(w)
		return
	}
	s.mu.Unlock()
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, IPNSResolveResponse{
		Expired:  false,
		Expires:  now.Add(24 * time.Hour),
		Name:     name,
		Path:     "/ipns/" + name,
		Sequence: 1,
		Value:    cid,
	})
}
