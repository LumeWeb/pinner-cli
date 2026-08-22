package ipfs

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// websiteDomain is the stored representation of a domain bound to a website.
// It carries the fields the generated DomainResponse/DomainDANERepublishResponse
// expose plus internal state (zone name, TLSa rdata) used by the dns-requirements
// and dane-republish happy paths.
type websiteDomain struct {
	Delegation        *DNSDelegation `json:"delegation,omitempty"`
	DnsHostingEnabled bool           `json:"dns_hosting_enabled"`
	Domain            string         `json:"domain"`
	GatewayHost       *string        `json:"gateway_host,omitempty"`
	Id                int            `json:"id"`
	Namespace         string         `json:"namespace"`
	OwnerName         *string        `json:"owner_name,omitempty"`
	Ssl               *SSLStatusInfo `json:"ssl,omitempty"`
	Status            *string        `json:"status,omitempty"`
	TlsaRdata         *string        `json:"tlsa_rdata,omitempty"`
	ZoneName          *string        `json:"zone_name,omitempty"`
}

// websiteSite is the stored representation of a website. It mirrors the
// WebsiteResponse/WebsiteItem wire contract plus its bound domains.
type websiteSite struct {
	ActiveCid            *string          `json:"active_cid,omitempty"`
	Created              time.Time        `json:"created"`
	DnsHostingEnabled    bool             `json:"dns_hosting_enabled"`
	DnsZoneId            *int             `json:"dns_zone_id,omitempty"`
	Domain               string           `json:"domain"`
	Expired              bool             `json:"expired"`
	GatewayDomain        *string          `json:"gateway_domain,omitempty"`
	Id                   int              `json:"id"`
	IpnsKeyId            *int             `json:"ipns_key_id,omitempty"`
	IsSubdomain          bool             `json:"is_subdomain"`
	LastCheckedAt        *time.Time       `json:"last_checked_at,omitempty"`
	Ssl                  *SSLStatusInfo   `json:"ssl,omitempty"`
	Status               string           `json:"status"`
	TargetHash           string           `json:"target_hash"`
	TargetType           string           `json:"target_type"`
	Updated              time.Time        `json:"updated"`
	ValidationExpiresAt  *time.Time       `json:"validation_expires_at,omitempty"`
	ValidationRecordHost *string          `json:"validation_record_host,omitempty"`
	ValidationToken      string           `json:"validation_token"`
	Domains              []*websiteDomain `json:"-"`
}

// toResponse converts a stored website to the public WebsiteResponse shape.
func (w *websiteSite) toResponse() WebsiteResponse {
	ssl := w.Ssl
	if ssl == nil {
		ssl = &SSLStatusInfo{Status: "active"}
	}
	return WebsiteResponse{
		ActiveCid:            w.ActiveCid,
		Created:              w.Created,
		DnsHostingEnabled:    w.DnsHostingEnabled,
		DnsZoneId:            w.DnsZoneId,
		Domain:               w.Domain,
		Expired:              w.Expired,
		GatewayDomain:        w.GatewayDomain,
		Id:                   w.Id,
		IpnsKeyId:            w.IpnsKeyId,
		IsSubdomain:          w.IsSubdomain,
		LastCheckedAt:        w.LastCheckedAt,
		Ssl:                  ssl,
		Status:               w.Status,
		TargetHash:           w.TargetHash,
		TargetType:           w.TargetType,
		Updated:              w.Updated,
		ValidationExpiresAt:  w.ValidationExpiresAt,
		ValidationRecordHost: w.ValidationRecordHost,
		ValidationToken:      w.ValidationToken,
	}
}

// toItem converts a stored website to the WebsiteItem list shape.
func (w *websiteSite) toItem() WebsiteItem {
	r := w.toResponse()
	return WebsiteItem{
		ActiveCid:            r.ActiveCid,
		Created:              r.Created,
		DnsHostingEnabled:    r.DnsHostingEnabled,
		DnsZoneId:            r.DnsZoneId,
		Domain:               r.Domain,
		Expired:              r.Expired,
		GatewayDomain:        r.GatewayDomain,
		Id:                   r.Id,
		IpnsKeyId:            r.IpnsKeyId,
		IsSubdomain:          r.IsSubdomain,
		LastCheckedAt:        r.LastCheckedAt,
		Ssl:                  r.Ssl,
		Status:               r.Status,
		TargetHash:           r.TargetHash,
		TargetType:           r.TargetType,
		Updated:              r.Updated,
		ValidationExpiresAt:  r.ValidationExpiresAt,
		ValidationRecordHost: r.ValidationRecordHost,
		ValidationToken:      r.ValidationToken,
	}
}

func (s *Server) nextDomainID() int {
	s.domainSeq++
	return s.domainSeq
}

// websiteByID resolves a website by numeric id path param, returning a
// notFound bool when the raw value is non-numeric or unknown.
func (s *Server) websiteByID(idParam string) (*websiteSite, bool) {
	id, err := strconv.Atoi(idParam)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.websites[id]
	return w, ok
}

// SeedWebsite creates and stores a website without going through the HTTP API,
// so list/get return data for the seeded default token. Returns the created
// website. domain is the site's primary (apex) domain, targetHash the pinned
// content CID, targetType "ipfs" or "ipns".
func (s *Server) SeedWebsite(domain, targetHash, targetType string) *WebsiteResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.websiteSeq++
	now := time.Now().UTC()
	host := "gateway.internal"
	w := &websiteSite{
		ActiveCid:         &targetHash,
		Created:           now,
		DnsHostingEnabled: true,
		Domain:            domain,
		GatewayDomain:     &host,
		Id:                s.websiteSeq,
		IsSubdomain:       false,
		Ssl:               &SSLStatusInfo{Status: "ready"},
		Status:            "active",
		TargetHash:        targetHash,
		TargetType:        targetType,
		Updated:           now,
		ValidationToken:   "seed-token",
		Domains:           []*websiteDomain{},
	}
	// A website's apex domain doubles as its first bound domain binding.
	w.Domains = append(w.Domains, s.newDomainLocked(w, domain, "icann", true))
	s.websites[w.Id] = w
	resp := w.toResponse()
	return &resp
}

// newDomainLocked allocates a bound-domain binding for a website. The caller
// must hold s.mu.
func (s *Server) newDomainLocked(w *websiteSite, domain, namespace string, primary bool) *websiteDomain {
	dnsHost := "active"
	d := &websiteDomain{
		DnsHostingEnabled: primary,
		Domain:            domain,
		GatewayHost:       w.GatewayDomain,
		Id:                s.nextDomainID(),
		Namespace:         namespace,
		OwnerName:         nil,
		Ssl:               &SSLStatusInfo{Status: "ready"},
		Status:            &dnsHost,
		ZoneName:          &domain,
	}
	if !primary {
		// Secondary bindings carry delegation guidance.
		ns := []string{"ns1.hosting.internal", "ns2.hosting.internal"}
		mode := "dnssec"
		d.Delegation = &DNSDelegation{
			Nameservers: &ns,
			Mode:        &mode,
		}
	}
	return d
}

// domainByID returns a bound domain by id within a website, or false.
func (s *Server) domainByID(w *websiteSite, domainIDParam string) (*websiteDomain, bool) {
	did, err := strconv.Atoi(domainIDParam)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range w.Domains {
		if d.Id == did {
			return d, true
		}
	}
	return nil, false
}

// GetApiWebsites lists websites for the authenticated user
// (GET /api/websites).
func (s *Server) GetApiWebsites(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	s.mu.Lock()
	data := make([]WebsiteItem, 0, len(s.websites))
	for _, ws := range s.websites {
		data = append(data, ws.toItem())
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, WebsiteItemResponse{Data: data, Total: total})
}

// PostApiWebsites creates a new website (POST /api/websites).
func (s *Server) PostApiWebsites(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	var body WebsiteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}
	if body.TargetHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_hash is required"})
		return
	}
	targetType := body.TargetType
	if targetType == "" {
		targetType = "ipfs"
	}
	namespace := "icann"
	if body.Namespace != nil && *body.Namespace != "" {
		namespace = *body.Namespace
	}
	dnsHosting := body.DnsHostingEnabled != nil && *body.DnsHostingEnabled

	s.mu.Lock()
	s.websiteSeq++
	now := time.Now().UTC()
	host := "gateway-shared.internal"
	ws := &websiteSite{
		ActiveCid:         &body.TargetHash,
		Created:           now,
		DnsHostingEnabled: dnsHosting,
		Domain:            body.Domain,
		GatewayDomain:     &host,
		Id:                s.websiteSeq,
		IsSubdomain:       false,
		Ssl:               &SSLStatusInfo{Status: "pending"},
		Status:            "pending",
		TargetHash:        body.TargetHash,
		TargetType:        targetType,
		Updated:           now,
		ValidationToken:   "tok-" + strconv.Itoa(s.websiteSeq),
		Domains:           []*websiteDomain{},
	}
	ws.Domains = append(ws.Domains, s.newDomainLocked(ws, body.Domain, namespace, true))
	s.websites[ws.Id] = ws
	resp := ws.toResponse()
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, resp)
}

// GetApiWebsitesId returns a single website (GET /api/websites/{id}).
func (s *Server) GetApiWebsitesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, ws.toResponse())
}

// PutApiWebsitesId updates an existing website (PUT /api/websites/{id}). This
// backs both websites_update and websites_enable_ipns (the latter sends
// target_type=ipns to switch IPFS -> IPNS targeting).
func (s *Server) PutApiWebsitesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	var body WebsiteUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	if body.Domain != nil && *body.Domain != "" {
		ws.Domain = *body.Domain
	}
	if body.TargetHash != nil && *body.TargetHash != "" {
		ws.TargetHash = *body.TargetHash
		ws.ActiveCid = body.TargetHash
	}
	if body.TargetType != nil && *body.TargetType != "" {
		ws.TargetType = *body.TargetType
		// Enabling IPNS targeting allocates an IPNS key id and flips the
		// website to active.
		if ws.TargetType == "ipns" && ws.IpnsKeyId == nil {
			k := ws.Id + 1000
			ws.IpnsKeyId = &k
			if ws.Status == "pending" {
				ws.Status = "active"
			}
		}
	}
	if body.DnsHostingEnabled != nil {
		ws.DnsHostingEnabled = *body.DnsHostingEnabled
	}
	ws.Updated = time.Now().UTC()
	resp := ws.toResponse()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// DeleteApiWebsitesId deletes a website (DELETE /api/websites/{id}).
func (s *Server) DeleteApiWebsitesId(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	wid, err := strconv.Atoi(id)
	if err != nil {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	if _, ok := s.websites[wid]; !ok {
		s.mu.Unlock()
		writeNotFound(w)
		return
	}
	delete(s.websites, wid)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// GetApiWebsitesConfig returns website hosting configuration (gateway domain
// and nameservers) (GET /api/websites/config).
func (s *Server) GetApiWebsitesConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	gateway := "gateway.hosting.internal"
	ns := []string{"ns1.hosting.internal", "ns2.hosting.internal"}
	writeJSON(w, http.StatusOK, WebsiteConfigResponse{
		GatewayDomain: &gateway,
		Nameservers:   &ns,
	})
}

// GetApiWebsitesDomainSslStatus returns the SSL status for a website's domain
// (GET /api/websites/{domain}/ssl-status). The website is resolved by its apex
// domain or any bound domain.
func (s *Server) GetApiWebsitesDomainSslStatus(w http.ResponseWriter, r *http.Request, domain string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByDomain(domain)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, ws.toResponse())
}

// websiteByDomain looks up a website whose apex domain or a bound domain
// matches domain.
func (s *Server) websiteByDomain(domain string) (*websiteSite, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.websites {
		if strings.EqualFold(w.Domain, domain) {
			return w, true
		}
		for _, d := range w.Domains {
			if strings.EqualFold(d.Domain, domain) {
				return w, true
			}
		}
	}
	return nil, false
}

// PostApiWebsitesIdValidate validates a website's DNS configuration
// (POST /api/websites/{id}/validate). The fake always reports success.
func (s *Server) PostApiWebsitesIdValidate(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, WebsiteValidateResponse{
		Domain:  ws.Domain,
		Id:      ws.Id,
		Message: "website is valid",
		Reason:  "validated",
		Valid:   true,
	})
}

// GetApiWebsitesIdDomains lists the domains bound to a website
// (GET /api/websites/{id}/domains).
func (s *Server) GetApiWebsitesIdDomains(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	data := make([]DomainResponse, 0, len(ws.Domains))
	for _, d := range ws.Domains {
		data = append(data, d.toResponse())
	}
	total := len(data)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, DomainListResponse{Data: data, Total: total})
}

func (d *websiteDomain) toResponse() DomainResponse {
	var ssl *SSLStatusInfo
	if d.Ssl != nil {
		c := *d.Ssl
		ssl = &c
	}
	return DomainResponse{
		Delegation:        d.Delegation,
		DnsHostingEnabled: d.DnsHostingEnabled,
		Domain:            d.Domain,
		GatewayHost:       d.GatewayHost,
		Id:                d.Id,
		Namespace:         d.Namespace,
		Ssl:               ssl,
		Status:            d.Status,
		ZoneName:          d.ZoneName,
	}
}

func (d *websiteDomain) toRepublishResponse() DomainDANERepublishResponse {
	tlsa := "_443._tcp." + d.Domain
	rdata := "3 1 1 ab12cd34ef56"
	return DomainDANERepublishResponse{
		Delegation:  d.Delegation,
		Domain:      d.Domain,
		GatewayHost: d.GatewayHost,
		Id:          d.Id,
		Namespace:   d.Namespace,
		OwnerName:   d.OwnerName,
		Ssl:         d.Ssl,
		Status:      d.Status,
		TlsaRdata:   &rdata,
		TlsaRecord:  &tlsa,
		ZoneName:    d.ZoneName,
	}
}

// PostApiWebsitesIdDomains binds a domain to a website
// (POST /api/websites/{id}/domains).
func (s *Server) PostApiWebsitesIdDomains(w http.ResponseWriter, r *http.Request, id string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	var body DomainRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Domain == "" || body.Namespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain and namespace are required"})
		return
	}
	s.mu.Lock()
	d := s.newDomainLocked(ws, body.Domain, body.Namespace, false)
	ws.Domains = append(ws.Domains, d)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, d.toResponse())
}

// DeleteApiWebsitesIdDomainsDomainId unbinds a domain from a website
// (DELETE /api/websites/{id}/domains/{domain_id}).
func (s *Server) DeleteApiWebsitesIdDomainsDomainId(w http.ResponseWriter, r *http.Request, id string, domainId string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	d, ok := s.domainByID(ws, domainId)
	if !ok {
		writeNotFound(w)
		return
	}
	s.mu.Lock()
	filtered := ws.Domains[:0]
	for _, dd := range ws.Domains {
		if dd.Id != d.Id {
			filtered = append(filtered, dd)
		}
	}
	ws.Domains = filtered
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// PatchApiWebsitesIdDomainsDomainId updates a bound domain's per-domain DNS
// control (dns_hosting_enabled / primary) (PATCH /api/websites/{id}/domains/{domain_id}).
func (s *Server) PatchApiWebsitesIdDomainsDomainId(w http.ResponseWriter, r *http.Request, id string, domainId string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	d, ok := s.domainByID(ws, domainId)
	if !ok {
		writeNotFound(w)
		return
	}
	var body DomainUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	if body.DnsHostingEnabled != nil {
		d.DnsHostingEnabled = *body.DnsHostingEnabled
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, d.toResponse())
}

// PostApiWebsitesIdDomainsDomainIdVerify verifies a bound domain's delegation
// (POST /api/websites/{id}/domains/{domain_id}/verify). Always reports success.
func (s *Server) PostApiWebsitesIdDomainsDomainIdVerify(w http.ResponseWriter, r *http.Request, id string, domainId string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	d, ok := s.domainByID(ws, domainId)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, d.toResponse())
}

// GetApiWebsitesIdDomainsDomainIdDnsRequirements returns the DNS records a
// user must publish to complete delegation for a bound domain
// (GET /api/websites/{id}/domains/{domain_id}/dns-requirements).
func (s *Server) GetApiWebsitesIdDomainsDomainIdDnsRequirements(w http.ResponseWriter, r *http.Request, id string, domainId string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	d, ok := s.domainByID(ws, domainId)
	if !ok {
		writeNotFound(w)
		return
	}
	resp := d.toResponse()
	// Attach delegation guidance for the DNSSEC requirements happy path.
	ns := []string{"ns1.hosting.internal", "ns2.hosting.internal"}
	mode := "dnssec"
	resp.Delegation = &DNSDelegation{
		Nameservers: &ns,
		Mode:        &mode,
	}
	writeJSON(w, http.StatusOK, resp)
}

// PostApiWebsitesIdDomainsDomainIdDaneRepublish forces re-publication of a
// bound domain's DANE TLSA records
// (POST /api/websites/{id}/domains/{domain_id}/dane-republish).
func (s *Server) PostApiWebsitesIdDomainsDomainIdDaneRepublish(w http.ResponseWriter, r *http.Request, id string, domainId string) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	ws, ok := s.websiteByID(id)
	if !ok {
		writeNotFound(w)
		return
	}
	d, ok := s.domainByID(ws, domainId)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, d.toRepublishResponse())
}
