package ipfs

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// dnsRecord is the stored representation of a DNS record. It mirrors the
// ipfs-sdk client's RecordResponse contract, including the `id` field, which
// the local generated server.gen.go RecordResponse omits (stale) but which the
// SDK decodes from the wire. Emitting `id` keeps record-id-bearing tools
// (e.g. dns_records_delete --id, dns records list) functional.
type dnsRecord struct {
	Content  string `json:"content"`
	Disabled bool   `json:"disabled"`
	Id       string `json:"id"`
	Name     string `json:"name"`
	Ttl      int    `json:"ttl"`
	Type     string `json:"type"`
	ZoneId   int    `json:"zone_id"`
}

// recordKey builds the composite map key for a stored record.
func recordKey(name, recordType, content string) string {
	return name + "\x00" + recordType + "\x00" + content
}

// zoneByID resolves a numeric zone id from a path parameter, returning a
// notFound bool when the raw value is non-numeric or unknown.
func (s *Server) zoneByID(idParam string) (*ZoneResponse, bool) {
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[id]
	return z, ok
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// newZone allocates a ZoneResponse for the store.
func (s *Server) newZone(domain string, nameservers []string) *ZoneResponse {
	s.zoneSeq++
	now := time.Now().UTC()
	return &ZoneResponse{
		Id:        s.zoneSeq,
		Domain:    domain,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// GetApiDnsZones lists all DNS zones for the authenticated user
// (GET /api/dns/zones).
func (s *Server) GetApiDnsZones(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	data := make([]ZoneListResponse, 0, len(s.zones))
	for _, z := range s.zones {
		data = append(data, ZoneListResponse{
			CreatedAt:      z.CreatedAt,
			Domain:         z.Domain,
			Id:             z.Id,
			PowerdnsZoneId: z.PowerdnsZoneId,
			Status:         z.Status,
			UpdatedAt:      z.UpdatedAt,
			UserId:         z.UserId,
		})
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, ZoneListResponseResponse{Data: data, Total: total})
}

// PostApiDnsZones creates a new DNS zone (POST /api/dns/zones).
func (s *Server) PostApiDnsZones(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body ZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}
	s.mu.Lock()
	z := s.newZone(body.Domain, derefNS(body.Nameservers))
	s.zones[z.Id] = z
	s.records[z.Id] = map[string]*dnsRecord{}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, z)
}

func derefNS(ns *[]string) []string {
	if ns == nil {
		return nil
	}
	return *ns
}

// GetApiDnsZonesId returns a single DNS zone (GET /api/dns/zones/{id}).
func (s *Server) GetApiDnsZonesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	z, ok := s.zoneByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	// zoneByID releases the lock on return, but it hands back a pointer aliased
	// by concurrent handlers (a PUT can mutate it). Serialize a copy taken under
	// the lock so the handler never reads the zone mid-write.
	s.mu.Lock()
	cp := *z
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, &cp)
}

// DeleteApiDnsZonesId deletes a DNS zone and its records
// (DELETE /api/dns/zones/{id}).
func (s *Server) DeleteApiDnsZonesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	if _, ok := s.zones[zid]; !ok {
		s.mu.Unlock()
		writeNotFound(w)
		return
	}
	delete(s.zones, zid)
	delete(s.records, zid)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// PutApiDnsZonesId updates a DNS zone's domain (PUT /api/dns/zones/{id}).
func (s *Server) PutApiDnsZonesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body ZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	z, ok := s.zoneByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	// Re-acquire the lock around the mutation: zoneByID returns a pointer that
	// is aliased by every handler (GET/DELETE/PUT), so mutating it here without
	// the lock would race concurrent readers/writers on the same zone. Copy the
	// zone under the lock and serialize the copy AFTER unlocking, so serialization
	// never reads a zone a concurrent handler could mutate mid-write.
	s.mu.Lock()
	if body.Domain != "" {
		z.Domain = body.Domain
	}
	z.UpdatedAt = time.Now().UTC()
	cp := *z
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, &cp)
}

// PostApiDnsZonesIdValidate validates a DNS zone's nameserver delegation
// (POST /api/dns/zones/{id}/validate). The fake always reports success.
func (s *Server) PostApiDnsZonesIdValidate(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	if _, ok := s.zoneByID(id); !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, ValidationResponse{
		CheckedAt:   time.Now().UTC(),
		Message:     "zone is valid",
		Nameservers: &[]string{"ns1.example.com", "ns2.example.com"},
		Valid:       true,
	})
}

// zoneRecords returns the records map for a zone under the lock, or nil.
func (s *Server) zoneRecords(zid int) map[string]*dnsRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[zid]
}

// recordListResponse is the envelope for listing DNS records. It reuses the
// contract field names (data/total) but carries []dnsRecord (with the `id`
// field) rather than the stale generated RecordResponse which omits `id`.
type recordListResponse struct {
	Data  []dnsRecord `json:"data"`
	Total int         `json:"total"`
}

// GetApiDnsZonesIdRecords lists records for a zone
// (GET /api/dns/zones/{id}/records).
func (s *Server) GetApiDnsZonesIdRecords(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	if _, ok := s.zoneByID(id); !ok {
		writeNotFound(w)
		return
	}
	rm := s.zoneRecords(zid)
	if rm == nil {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	data := make([]dnsRecord, 0, len(rm))
	for _, rec := range rm {
		data = append(data, *rec)
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, recordListResponse{Data: data, Total: total})
}

// PostApiDnsZonesIdRecords creates a record in a zone
// (POST /api/dns/zones/{id}/records).
func (s *Server) PostApiDnsZonesIdRecords(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	if _, ok := s.zoneByID(id); !ok {
		writeNotFound(w)
		return
	}
	var body RecordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Name == "" || body.Type == "" || body.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, type and content are required"})
		return
	}
	name := body.Name
	if name == "@" {
		name = ""
	}
	recordType := strings.ToUpper(body.Type)
	ttl := body.Ttl
	if ttl == nil {
		d := 3600
		ttl = &d
	}
	disabled := body.Disabled != nil && *body.Disabled
	rec := &dnsRecord{
		Content:  body.Content,
		Disabled: disabled,
		Id:       s.nextRecordID(),
		Name:     name,
		Ttl:      *ttl,
		Type:     recordType,
		ZoneId:   zid,
	}
	s.mu.Lock()
	s.records[zid][recordKey(name, recordType, body.Content)] = rec
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) nextRecordID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordSeq++
	return "rec-" + strconv.Itoa(s.recordSeq)
}

// findRecordByNameType returns the first stored record matching name+type and
// its content, or ("", false).
func (s *Server) findRecordByNameType(zid int, name, recordType string) (*dnsRecord, string, bool) {
	rm := s.zoneRecords(zid)
	if rm == nil {
		return nil, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, rec := range rm {
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 {
			continue
		}
		if parts[0] == name && parts[1] == recordType {
			cp := *rec
			return &cp, parts[2], true
		}
	}
	return nil, "", false
}

// GetApiDnsZonesIdRecordsNameType returns a single record
// (GET /api/dns/zones/{id}/records/{name}/{type}).
func (s *Server) GetApiDnsZonesIdRecordsNameType(w http.ResponseWriter, r *http.Request, id string, name string, pType string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	rec, _, ok := s.findRecordByNameType(zid, name, strings.ToUpper(pType))
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// PutApiDnsZonesIdRecordsNameType updates a record's content/ttl
// (PUT /api/dns/zones/{id}/records/{name}/{type}).
func (s *Server) PutApiDnsZonesIdRecordsNameType(w http.ResponseWriter, r *http.Request, id string, name string, pType string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	recordType := strings.ToUpper(pType)
	rec, oldContent, ok := s.findRecordByNameType(zid, name, recordType)
	if !ok {
		writeNotFound(w)
		return
	}
	var body RecordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	newContent := body.Content
	if newContent == "" {
		newContent = oldContent
	}
	if body.Ttl != nil {
		rec.Ttl = *body.Ttl
	}
	if body.Disabled != nil {
		rec.Disabled = *body.Disabled
	}
	rec.Content = newContent

	s.mu.Lock()
	delete(s.records[zid], recordKey(name, recordType, oldContent))
	s.records[zid][recordKey(name, recordType, newContent)] = rec
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, rec)
}

// DeleteApiDnsZonesIdRecordsNameType deletes a record (or entire RRSet)
// (DELETE /api/dns/zones/{id}/records/{name}/{type}). The optional JSON body
// carries a content selector ({content: "..."}) to delete a single rdata
// value; without it, every record for name+type is removed.
func (s *Server) DeleteApiDnsZonesIdRecordsNameType(w http.ResponseWriter, r *http.Request, id string, name string, pType string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	zid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	recordType := strings.ToUpper(pType)

	// Optional content selector from the body.
	content := ""
	if r.Body != nil {
		var del struct {
			Content *string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&del)
		if del.Content != nil {
			content = *del.Content
		}
	}

	rm := s.zoneRecords(zid)
	if rm == nil {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if content != "" {
		// Delete a single record matching name+type+content.
		delete(rm, recordKey(name, recordType, content))
	} else {
		// Delete the whole RRSet (every record with that name+type).
		for key := range rm {
			parts := strings.Split(key, "\x00")
			if len(parts) == 3 && parts[0] == name && parts[1] == recordType {
				delete(rm, key)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
