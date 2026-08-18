package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// Resource URI scheme and well-known URIs.
const (
	ResourceScheme       = "pinner://"
	AccountStatusURI     = "pinner://account/status"
	VaultStatusURI       = "pinner://vault/status"
	DNSRequirementsTmpl  = "pinner://websites/{domain}/dns-requirements"
	ValidationStatusTmpl = "pinner://websites/{id}/validation-status"
	WizardStateTmpl      = "pinner://wizard/{session_id}/state"
)

// DNSRecord describes a single DNS record the user must create.
type DNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// DNSRequirements holds the DNS records and nameservers a user needs to
// configure for a website, plus whether DNS hosting is enabled.
type DNSRequirements struct {
	Domain            string      `json:"domain"`
	DNSHostingEnabled bool        `json:"dns_hosting_enabled"`
	Records           []DNSRecord `json:"records"`
	Nameservers       []string    `json:"nameservers,omitempty"`
	GatewayDomain     string      `json:"gateway_domain,omitempty"`
}

// ValidationStatus holds the live validation result for a website.
type ValidationStatus struct {
	ID     int    `json:"id"`
	Domain string `json:"domain"`
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
	Status string `json:"status,omitempty"`
}

// WizardSessionState holds the current state of a wizard session for the
// pinner://wizard/{session_id}/state resource.
type WizardSessionState struct {
	SessionID  string             `json:"session_id"`
	Current    string             `json:"current_step"`
	Complete   bool               `json:"complete"`
	NextSchema *jsonschema.Schema `json:"next_schema,omitempty"`
	Expired    bool               `json:"expired,omitempty"`
}

// AccountStatusProvider supplies the data shown in pinner://account/status.
// It abstracts the auth service so resource handlers can be tested without a
// live API. Implementations should return ErrNotAuthenticated when no token
// is stored; the resource handler treats that as a non-error "unauthenticated"
// state.
type AccountStatusProvider interface {
	// IsAuthenticated reports whether an auth token is configured.
	IsAuthenticated() bool
	// AuthStatus calls the API to validate the stored token. Returns an
	// error if the token is invalid or the API is unreachable; the resource
	// handler surfaces that as the "unverified" state.
	AuthStatus(ctx context.Context) error
	// APIKey returns a masked hint of the configured auth token (e.g. last 4
	// chars). Returns "" if no token is configured.
	APIKey() string
	// Quota returns the account quota summary, or nil if the API is
	// unreachable or the account is not authenticated.
	Quota(ctx context.Context) map[string]any
	// ConfigSummary returns a masked summary of the live config (endpoint,
	// secure flag, etc.) reflecting the latest state at request time.
	ConfigSummary() map[string]any
}

// WebsitesResourceProvider supplies the website data for the DNS-requirements
// and validation-status resources. It is a subset of cli.WebsitesService that
// the resource layer needs, kept narrow for testability.
type WebsitesResourceProvider interface {
	// GetByDomain resolves a domain to a website and returns it.
	GetByDomain(ctx context.Context, domain string) (*ipfs.WebsiteItem, error)
	// GetByID resolves a website by numeric ID (string-encoded).
	GetByID(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	// Validate triggers a live validation of a website by ID.
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	// GetConfig returns the website hosting config (nameservers, gateway domain).
	GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
}

// VaultStatusProvider supplies the data shown in pinner://vault/status.
// It abstracts vault state so resource handlers can be tested without a
// live Sia connection.
type VaultStatusProvider interface {
	// IsInitialized reports whether the local vault database exists.
	IsInitialized() bool
	// IsSiaConfigured reports whether a Sia app key is configured.
	IsSiaConfigured() bool
	// IndexerURL returns the derived Sia indexer URL.
	IndexerURL() string
	// FileCount returns the number of files in the local vault cache.
	FileCount(ctx context.Context) (int64, error)
	// AccountBalance returns the Sia account balance, or 0 if unavailable.
	AccountBalance(ctx context.Context) (float64, error)
}

// ResourceProviders bundles the dependencies that build the pinner://
// resources and resource templates. The session store is *SessionStore from
// this package; the other two are injected interfaces so the resource
// handlers can call live APIs or mocks.
type ResourceProviders struct {
	Account  AccountStatusProvider
	Websites WebsitesResourceProvider
	Vault    VaultStatusProvider
	Sessions *session.SessionStore
}

// ResourceDescriptors builds the SDK-neutral pinner:// resource and
// resource-template descriptors from the given providers. The adapter
// registers them on the protocol server.
//
// Static resources: pinner://account/status, pinner://vault/status
// Resource templates:
//   - pinner://websites/{domain}/dns-requirements
//   - pinner://websites/{id}/validation-status
//   - pinner://wizard/{session_id}/state
func ResourceDescriptors(provs ResourceProviders) ([]model.ResourceDescriptor, []model.ResourceTemplateDescriptor) {
	resources := []model.ResourceDescriptor{
		{
			URI:         AccountStatusURI,
			Name:        "account-status",
			Description: "Current auth state, quota, and config summary for the authenticated account",
			MIMEType:    "application/json",
			Handler:     accountStatusHandler(provs.Account),
		},
		{
			URI:         VaultStatusURI,
			Name:        "vault-status",
			Description: "Vault state: initialization, Sia connection, file count, and account balance",
			MIMEType:    "application/json",
			Handler:     vaultStatusHandler(provs.Vault),
		},
	}

	templates := []model.ResourceTemplateDescriptor{
		{
			URITemplate: DNSRequirementsTmpl,
			Name:        "website-dns-requirements",
			Description: "DNS records needed for a website, resolved by domain name",
			MIMEType:    "application/json",
			Handler:     dnsRequirementsHandler(provs.Websites),
		},
		{
			URITemplate: ValidationStatusTmpl,
			Name:        "website-validation-status",
			Description: "Live validation state for a website, resolved by numeric ID",
			MIMEType:    "application/json",
			Handler:     validationStatusHandler(provs.Websites),
		},
		{
			URITemplate: WizardStateTmpl,
			Name:        "wizard-session-state",
			Description: "Current state of a wizard session, resolved by session ID",
			MIMEType:    "application/json",
			Handler:     wizardStateHandler(provs.Sessions),
		},
	}

	return resources, templates
}

// --- Handlers ---

// accountStatusHandler returns the auth state, quota, and config summary as
// JSON. Auth state is derived from the live AccountStatusProvider (which reads
// cfgMgr at request time) plus an optional AuthStatus call for token
// verification. Quota is the raw map returned by the provider.
func accountStatusHandler(acct AccountStatusProvider) model.ResourceHandler {
	return func(ctx context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
		status := map[string]any{
			"authenticated": false,
		}

		// Auth state from the live provider first (reflects latest cfgMgr state).
		if acct != nil && acct.IsAuthenticated() {
			status["authenticated"] = true
			status["api_key"] = acct.APIKey()
		}

		// Live verification if a provider is wired in.
		if acct != nil {
			if err := acct.AuthStatus(ctx); err != nil {
				status["token_valid"] = false
				status["token_error"] = err.Error()
			} else {
				status["token_valid"] = true
			}

			if q := acct.Quota(ctx); q != nil {
				status["quota"] = q
			}
		}

		// Config summary from the live provider (reflects latest cfgMgr state).
		if acct != nil {
			if s := acct.ConfigSummary(); s != nil {
				status["config"] = s
			}
		}

		raw, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("marshal account status: %w", err)
		}
		return model.ResourceResult{URI: AccountStatusURI, MIMEType: "application/json", Text: string(raw)}, nil
	}
}

// vaultStatusHandler builds the vault status resource response.
func vaultStatusHandler(prov VaultStatusProvider) model.ResourceHandler {
	return func(ctx context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
		status := map[string]any{
			"initialized":    false,
			"sia_configured": false,
		}

		if prov != nil {
			status["initialized"] = prov.IsInitialized()
			status["sia_configured"] = prov.IsSiaConfigured()
			status["indexer_url"] = prov.IndexerURL()

			if prov.IsInitialized() {
				if count, err := prov.FileCount(ctx); err == nil {
					status["file_count"] = count
				}
			}

			if prov.IsSiaConfigured() {
				if balance, err := prov.AccountBalance(ctx); err == nil {
					status["account_balance"] = balance
				}
			}
		}

		raw, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("marshal vault status: %w", err)
		}
		return model.ResourceResult{URI: VaultStatusURI, MIMEType: "application/json", Text: string(raw)}, nil
	}
}

// dnsRequirementsHandler builds the list of DNS records the user must add for
// a website, mirroring the CLI's showDNSRecordInstructions output.
func dnsRequirementsHandler(ws WebsitesResourceProvider) model.ResourceHandler {
	return func(ctx context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
		domain := req.Arguments["domain"]
		if domain == "" {
			return model.ResourceResult{}, fmt.Errorf("missing or invalid domain parameter")
		}

		if ws == nil {
			return model.ResourceResult{}, fmt.Errorf("websites provider not configured")
		}

		website, err := ws.GetByDomain(ctx, domain)
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("resolve website %q: %w", domain, err)
		}
		if website == nil {
			return model.ResourceResult{}, fmt.Errorf("website %q not found", domain)
		}

		reqs := buildDNSRequirements(website)

		// For DNS-hosted websites, fetch nameservers from the config.
		if website.DnsHostingEnabled {
			cfg, cfgErr := ws.GetConfig(ctx)
			if cfgErr != nil {
				return model.ResourceResult{}, fmt.Errorf("fetch nameserver config: %w", cfgErr)
			}
			if cfg != nil && cfg.Nameservers != nil && len(*cfg.Nameservers) > 0 {
				reqs.Nameservers = *cfg.Nameservers
				nsRecords := make([]DNSRecord, 0, len(reqs.Nameservers))
				for _, ns := range reqs.Nameservers {
					nsRecords = append(nsRecords, DNSRecord{Name: website.Domain, Type: "NS", Value: ns})
				}
				reqs.Records = nsRecords
			}
			// If no nameservers were returned, keep the placeholder NS record
			// from buildDNSRequirements so the user knows NS delegation is required.
		}

		raw, err := json.MarshalIndent(reqs, "", "  ")
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("marshal dns requirements: %w", err)
		}
		return model.ResourceResult{URI: req.URI, MIMEType: "application/json", Text: string(raw)}, nil
	}
}

// validationStatusHandler calls the live validate API for a website.
func validationStatusHandler(ws WebsitesResourceProvider) model.ResourceHandler {
	return func(ctx context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
		id := req.Arguments["id"]
		if id == "" {
			return model.ResourceResult{}, fmt.Errorf("missing or invalid id parameter")
		}

		if ws == nil {
			return model.ResourceResult{}, fmt.Errorf("websites provider not configured")
		}

		// Resolve the website first so we can include domain + status.
		website, err := ws.GetByID(ctx, id)
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("resolve website id %q: %w", id, err)
		}
		var domain, status string
		if website != nil {
			domain = website.Domain
			status = website.Status
		}

		result, err := ws.Validate(ctx, id)
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("validate website %q: %w", id, err)
		}

		vs := ValidationStatus{Status: status}
		if result != nil {
			vs.ID = result.Id
			vs.Domain = result.Domain
			vs.Valid = result.Valid
			vs.Reason = result.Reason
		}
		// Fall back to website info if API didn't return domain/ID.
		if vs.Domain == "" {
			vs.Domain = domain
		}

		raw, err := json.MarshalIndent(vs, "", "  ")
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("marshal validation status: %w", err)
		}
		return model.ResourceResult{URI: req.URI, MIMEType: "application/json", Text: string(raw)}, nil
	}
}

// wizardStateHandler returns the current FSM state + next-step schema for a
// wizard session.
func wizardStateHandler(store *session.SessionStore) model.ResourceHandler {
	return func(ctx context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
		sessionID := req.Arguments["session_id"]
		if sessionID == "" {
			return model.ResourceResult{}, fmt.Errorf("missing or invalid session_id parameter")
		}

		if store == nil {
			return model.ResourceResult{}, fmt.Errorf("session store not configured")
		}

		uri := req.URI
		state := WizardSessionState{SessionID: sessionID}

		sess, err := store.Get(sessionID)
		if err != nil {
			if err == session.ErrSessionNotFound {
				return model.ResourceResult{}, fmt.Errorf("wizard session %q not found", sessionID)
			}
			if err == session.ErrSessionExpired {
				state.Expired = true
				state.Complete = true
				raw, _ := json.MarshalIndent(state, "", "  ")
				return model.ResourceResult{URI: uri, MIMEType: "application/json", Text: string(raw)}, nil
			}
			return model.ResourceResult{}, fmt.Errorf("lookup session %q: %w", sessionID, err)
		}

		state.Current = sess.FSM.Current()
		state.NextSchema = sess.NextSchema()
		if state.Current == "complete" {
			state.Complete = true
		}

		raw, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return model.ResourceResult{}, fmt.Errorf("marshal wizard state: %w", err)
		}
		return model.ResourceResult{URI: uri, MIMEType: "application/json", Text: string(raw)}, nil
	}
}

// --- Helpers ---

// buildDNSRequirements computes the DNS records for a website based on its
// hosting mode, mirroring the logic in pkg/cli/websites.go showDNSRecordInstructions.
func buildDNSRequirements(website *ipfs.WebsiteItem) DNSRequirements {
	reqs := DNSRequirements{
		Domain:            website.Domain,
		DNSHostingEnabled: website.DnsHostingEnabled,
	}

	if website.DnsHostingEnabled {
		// NS delegation: use gateway domain if known.
		if website.GatewayDomain != nil && *website.GatewayDomain != "" {
			reqs.GatewayDomain = *website.GatewayDomain
		}
		reqs.Records = []DNSRecord{
			{Name: website.Domain, Type: "NS", Value: "<nameserver>"},
		}
		return reqs
	}

	// Self-managed DNS: TXT validation + dnslink + optional CNAME.
	validationHost := website.Domain
	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		validationHost = *website.ValidationRecordHost
	}

	// The TXT record value the server validates is "<key>=<token>", where <key>
	// is the server-provided verification key embedded as the first DNS label
	// of the validation record host (e.g. host "pinner-verify.example.com"
	// carries key "pinner-verify"). It is never hardcoded; fall back to the
	// token when no host is available to derive the key from. The token is
	// normalized first — only an actual "<key>=" prefix is stripped, so the
	// result is always "<key>=<token>", never a doubled "<key>=<key>=<token>",
	// and a token containing "=" as content is never corrupted.
	validationValue := website.ValidationToken
	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		if key, _, _ := strings.Cut(*website.ValidationRecordHost, "."); key != "" {
			raw := website.ValidationToken
			if strings.HasPrefix(raw, key+"=") {
				raw = raw[len(key)+1:]
			}
			validationValue = key + "=" + raw
		}
	}

	records := []DNSRecord{
		{Name: validationHost, Type: "TXT", Value: validationValue},
		{Name: "_dnslink." + website.Domain, Type: "TXT", Value: "dnslink=/" + website.TargetType + "/" + website.TargetHash},
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, DNSRecord{Name: website.Domain, Type: "CNAME", Value: *website.GatewayDomain})
	}
	reqs.Records = records
	return reqs
}
