package ipfs

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// SeedPin stores a pin directly in the store without going through the HTTP
// API, so list/fetch/status have data to answer for the seeded pin. This is
// the pins-domain analogue of SeedIPNSKey; the e2e MCP harness can seed known
// pins before driving tools. Returns the stored pin.
func (s *Server) SeedPin(cid, name string) *PinStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newPinLocked(cid, name)
}

// newPinLocked allocates and stores a pin. The caller must hold s.mu. The
// request id is derived from the CID so a given CID maps to a stable id, which
// keeps the boxo client's fetch-by-cid → replace-by-id round-trip coherent.
func (s *Server) newPinLocked(cid, name string) *PinStatusResponse {
	reqID := "req-" + cid
	pin := &PinStatusResponse{
		Created:   time.Now(),
		Requestid: reqID,
		Status:    "pinned",
		Pin: PinRequest{
			Cid:  cid,
			Name: strPtr(name),
			Meta: &map[string]string{},
		},
	}
	s.pins[reqID] = pin
	return pin
}

func strPtr(s string) *string {
	return &s
}

// GetPins lists pins (IPFS Pinning Service API), honoring the query filters:
// cid, status, name (exact or partial per match), limit, before/after (created
// window) and meta (JSON key/value subset). Previously this ignored every
// filter and returned the whole store, which made pinner-cli's fetch-by-cid
// Status()/Unpin()/UpdatePin() take results[0] — the wrong pin once more than
// one existed. Now each filter narrows the result set.
func (s *Server) GetPins(w http.ResponseWriter, r *http.Request, params GetPinsParams) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	results := make([]PinStatusResponse, 0, len(s.pins))
	for _, p := range s.pins {
		if !pinMatches(p, params) {
			continue
		}
		results = append(results, *p)
	}
	s.mu.Unlock()

	// Stable output order so tests (and clients) see deterministic ordering.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Requestid < results[j].Requestid
	})

	// Apply limit after filtering (default: no cap when absent).
	if params.Limit != nil && *params.Limit >= 0 && *params.Limit < len(results) {
		results = results[:*params.Limit]
	}
	writeJSON(w, http.StatusOK, PinResultsResponse{Count: len(results), Results: results})
}

// pinMatches reports whether a stored pin satisfies the GetPins query filters.
// The IPFS Pinning Services spec treats unspecified filters as wildcards; a
// filter only excludes pins when it is present.
func pinMatches(p *PinStatusResponse, params GetPinsParams) bool {
	// cid: pin must carry at least one of the requested CIDs.
	if params.Cid != nil && len(*params.Cid) > 0 {
		matched := false
		for _, c := range *params.Cid {
			if c != "" && c == p.Pin.Cid {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// status: pin status must be one of the requested statuses.
	if params.Status != nil && len(*params.Status) > 0 {
		matched := false
		for _, st := range *params.Status {
			if st == p.Status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// name: exact by default, partial when match=partial (the spec's
	// substring strategy used by pinner-cli's pins_list --search).
	if params.Name != nil && *params.Name != "" {
		name := ""
		if p.Pin.Name != nil {
			name = *p.Pin.Name
		}
		if params.Match != nil && *params.Match == "partial" {
			if !strings.Contains(name, *params.Name) {
				return false
			}
		} else if name != *params.Name {
			return false
		}
	}

	// before/after: created window (ISO 8601 timestamps).
	if params.Before != nil {
		if t, err := time.Parse(time.RFC3339, *params.Before); err == nil && p.Created.After(t) {
			return false
		}
	}
	if params.After != nil {
		if t, err := time.Parse(time.RFC3339, *params.After); err == nil && p.Created.Before(t) {
			return false
		}
	}

	// meta: requested key/value pairs must all be present on the pin.
	if params.Meta != nil && *params.Meta != "" {
		var want map[string]string
		if err := json.Unmarshal([]byte(*params.Meta), &want); err == nil {
			meta := map[string]string{}
			if p.Pin.Meta != nil {
				meta = *p.Pin.Meta
			}
			for k, v := range want {
				if meta[k] != v {
					return false
				}
			}
		}
	}

	return true
}

// DeletePinsRequestid removes a pin by request id (IPFS Pinning Service API).
// This backs the boxo client's DeleteByID used by pinner-cli's pins_rm.
func (s *Server) DeletePinsRequestid(w http.ResponseWriter, r *http.Request, requestid string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	if _, ok := s.pins[requestid]; !ok {
		s.mu.Unlock()
		writeNotFound(w)
		return
	}
	delete(s.pins, requestid)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// PostPinsRequestid updates a pin's name and/or metadata by request id
// (IPFS Pinning Service API). This backs the boxo client's Replace, used by
// pinner-cli's pins_update. The body is a PinRequest whose name/meta replace
// the stored values; a nil/empty field leaves the existing value untouched,
// mirroring the client's merge-on-update semantics.
func (s *Server) PostPinsRequestid(w http.ResponseWriter, r *http.Request, requestid string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body PinRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pins[requestid]
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pin not found"})
		return
	}
	if body.Name != nil {
		p.Pin.Name = body.Name
	}
	if body.Meta != nil {
		// Replace the metadata wholesale, matching boxo's AddMeta semantics:
		// the client sends the full merged map, so the fake stores it as-is.
		p.Pin.Meta = body.Meta
	}
	if body.Cid != "" {
		p.Pin.Cid = body.Cid
	}
	writeJSON(w, http.StatusOK, p)
}
