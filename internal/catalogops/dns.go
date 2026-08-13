// Package catalogops implements the DNS domain operations for the operation
// catalog: zone and record CRUD driving the core DNS service directly and
// returning typed data.
package catalogops

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/dns"
)

// DNSDeps are the dependencies the DNS operations need at construction time.
// All are lazy getters (never package-init values) so service construction
// always uses fresh config / auth for the current invocation.
type DNSDeps struct {
	// CfgMgr returns a live config manager for the current invocation. When
	// nil, service() passes nil to the factories.
	CfgMgr func() config.Manager
	// Secure reports whether to use the secure (HTTPS) endpoint.
	Secure func() bool
	// ServiceFactory builds a DNS Service (newDNSAPI equivalent).
	ServiceFactory dns.ServiceFactoryFunc
	// NewAuthenticated builds a service with an explicit auth token; nil means
	// tokens are read from config via ServiceFactory.
	NewAuthenticated func(cfgMgr config.Manager, secure bool, token string) dns.Service
	// GetAuthToken returns an auth token override for the current command
	// context (empty = none).
	GetAuthToken func() string
}

// config returns the live config manager for this invocation, or nil.
func (d DNSDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// service builds the DNS Service from the deps. A per-invocation auth-token
// override from input takes precedence over the deps.GetAuthToken() config
// fallback; otherwise the plain ServiceFactory path is used.
func (d DNSDeps) service(input map[string]any) (dns.Service, error) {
	cfgMgr := d.config()
	if cfgMgr == nil {
		return nil, fmt.Errorf("catalogops: no config manager available")
	}
	secure := false
	if d.Secure != nil {
		secure = d.Secure()
	}
	if d.NewAuthenticated != nil {
		if t := authTokenFromInput(input); t != "" {
			return d.NewAuthenticated(cfgMgr, secure, t), nil
		}
		if tok := d.GetAuthToken; tok != nil {
			if t := tok(); t != "" {
				return d.NewAuthenticated(cfgMgr, secure, t), nil
			}
		}
	}
	return d.ServiceFactory(cfgMgr, secure), nil
}

// DNSZoneDeleteResult is the data returned by the zones delete handler.
type DNSZoneDeleteResult struct {
	Zone string // the domain/ID argument that was deleted
}

// DNSRecordDeleteResult is the data returned by the records delete handler.
type DNSRecordDeleteResult struct {
	ZoneID string
	Name   string
	Type   string
}

// DNSOperations returns the catalog operations for the DNS domain (the
// existing `dns` command tree: zones + records CRUD), each driving the core
// DNS Service.
func DNSOperations(d DNSDeps) []catalog.Operation {
	return []catalog.Operation{
		dnsZonesList(d),
		dnsZonesCreate(d),
		dnsZonesGet(d),
		dnsZonesDelete(d),
		dnsZonesValidate(d),
		dnsRecordsList(d),
		dnsRecordsCreate(d),
		dnsRecordsGet(d),
		dnsRecordsUpdate(d),
		dnsRecordsDelete(d),
	}
}

// ---- Zones ----

// dnsZonesList is the `dns zones list` operation.
func dnsZonesList(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_zones_list",
		Title:       "List DNS zones",
		Summary:     "List all DNS zones",
		Description: "List all DNS zones for the authenticated user. Returns each zone's ID, domain, status, optional PowerDNS zone ID and created timestamp.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			zones, err := svc.ListZones(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list zones: %w", err)
			}
			return zones, nil
		}),
	})
}

// dnsZonesCreate is the `dns zones create` operation. Splits the
// comma-separated nameservers and validates the domain before creating.
func dnsZonesCreate(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_zones_create",
		Title:       "Create a DNS zone",
		Summary:     "Create a new DNS zone",
		Description: "Create a new DNS zone for a domain (the container that holds that domain's DNS records). Requires a domain; optionally supply nameservers as a comma-separated list. Returns the created zone including its numeric ID and PowerDNS zone ID.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "domain", Type: catalog.ArgTypeString, Required: true, Help: "Domain to create the zone for"},
			{Name: "nameservers", Type: catalog.ArgTypeString, Help: "Comma-separated list of custom nameservers"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			domain := catalog.StrArg(input, "domain", "")
			if err := validateDomain(domain); err != nil {
				return nil, err
			}
			nameservers := parseCommaSeparated(catalog.StrArg(input, "nameservers", ""))
			zone, err := svc.CreateZone(ctx, domain, nameservers)
			if err != nil {
				return nil, fmt.Errorf("failed to create zone: %w", err)
			}
			return zone, nil
		}),
	})
}

// dnsZonesGet is the `dns zones get` operation.
//
// The positional <domain> may be a domain name or a numeric zone ID; both are
// resolved to a full ZoneResponse via resolveZoneByArg (read-only ListZones
// + GetZone).
func dnsZonesGet(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_zones_get",
		Title:       "Get a DNS zone",
		Summary:     "Get a DNS zone by domain or ID",
		Description: "Get details of one DNS zone, selected by domain name or numeric zone ID. Returns the zone's ID, domain, status, PowerDNS zone ID and created/updated timestamps. This returns the zone header only.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			zone, err := resolveZoneByArg(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			return zone, nil
		}),
	})
}

// dnsZonesDelete is the `dns zones delete` operation.
//
// Destructive and irreversible. Confirmation is enforced here (the confirm
// arg, mapped from the CLI's --force); the handler resolves the domain/ID to
// a numeric zone ID and deletes it, returning a confirmation result.
func dnsZonesDelete(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_zones_delete",
		Title:       "Delete a DNS zone",
		Summary:     "Delete a DNS zone",
		Description: "Delete a DNS zone and the records inside it, selected by domain name or numeric zone ID. DESTRUCTIVE and irreversible: there is no undo, and every record in the zone is removed. Does NOT remove the domain's website binding.",
		Category:    "core",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the zone; this is destructive and cannot be undone."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("dns_zones_delete: confirmation is required to delete a zone")
			}
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			id, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			if err := svc.DeleteZone(ctx, id); err != nil {
				return nil, fmt.Errorf("failed to delete zone: %w", err)
			}
			return &DNSZoneDeleteResult{Zone: arg}, nil
		}),
	})
}

// dnsZonesValidate is the `dns zones validate` operation (nameserver
// delegation check).
func dnsZonesValidate(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_zones_validate",
		Title:       "Validate DNS zone",
		Summary:     "Validate DNS zone nameserver delegation",
		Description: "Validate that a DNS zone's nameservers are properly delegated (point to the expected Pinner.xyz nameservers). Selects the zone by domain name or numeric ID.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			zone, err := resolveZoneByArg(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			result, err := svc.ValidateZone(ctx, strconv.Itoa(zone.Id))
			if err != nil {
				return nil, fmt.Errorf("failed to validate zone: %w", err)
			}
			return result, nil
		}),
	})
}

// ---- Records ----

// dnsRecordsList is the `dns records list` operation. Resolves the zone by
// domain/ID.
func dnsRecordsList(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_records_list",
		Title:       "List DNS records",
		Summary:     "List DNS records for a zone",
		Description: "List all DNS records for a zone, given the zone's domain (or numeric ID). Returns each record's name/type/content/TTL and disabled state.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			zoneID, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			records, err := svc.ListRecords(ctx, zoneID)
			if err != nil {
				return nil, fmt.Errorf("failed to list records: %w", err)
			}
			return records, nil
		}),
	})
}

// dnsRecordsCreate is the `dns records create` operation. --name is optional
// (apex when empty or "@"), --ttl defaults to 3600, and the record
// type/content are validated before create.
func dnsRecordsCreate(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_records_create",
		Title:       "Create a DNS record",
		Summary:     "Create a DNS record",
		Description: "Create a DNS record (A/AAAA/CNAME/MX/NS/TXT) in the specified zone. name is optional (omit or use @ for the apex); type and content are required; ttl defaults to 3600. Returns the created record.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: catalog.ArgTypeString, Help: "Record name (omit or use @ for apex)"},
			{Name: "type", Type: catalog.ArgTypeString, Required: true, Help: "Record type (A, AAAA, CNAME, MX, NS, TXT)"},
			{Name: "content", Type: catalog.ArgTypeString, Required: true, Help: "Record content (IP, domain, or text)"},
			{Name: "ttl", Type: catalog.ArgTypeInt, Default: "3600", Help: "TTL in seconds (default 3600)"},
			{Name: "disabled", Type: catalog.ArgTypeBool, Default: "false", Help: "Disable the record"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			name := catalog.StrArg(input, "name", "")
			recordType := catalog.StrArg(input, "type", "")
			content := catalog.StrArg(input, "content", "")

			if err := validateDNSRecord(recordType, content); err != nil {
				return nil, err
			}
			if err := validateDNSRecordName(name); err != nil {
				return nil, err
			}

			ttlVal := catalog.IntArg(input, "ttl", 3600)
			if ttlVal == 0 {
				ttlVal = 3600
			}
			disabled := catalog.BoolArg(input, "disabled", false)

			record := ipfs.RecordRequest{
				Name:     name,
				Type:     recordType,
				Content:  content,
				Ttl:      &ttlVal,
				Disabled: &disabled,
			}

			zoneID, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			created, err := svc.CreateRecord(ctx, zoneID, record)
			if err != nil {
				return nil, fmt.Errorf("failed to create record: %w", err)
			}
			return created, nil
		}),
	})
}

// dnsRecordsGet is the `dns records get` operation. Identified by zone
// (positional domain/ID) + --name + --type.
func dnsRecordsGet(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_records_get",
		Title:       "Get a DNS record",
		Summary:     "Get a DNS record",
		Description: "Get one DNS record, uniquely identified by the zone's domain plus name (label, or @ for apex) and type. Returns the record's content, TTL and disabled state.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Record name (or @ for apex)"},
			{Name: "type", Type: catalog.ArgTypeString, Required: true, Help: "Record type"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			name := catalog.StrArg(input, "name", "")
			recordType := catalog.StrArg(input, "type", "")
			if name == "" || recordType == "" {
				return nil, fmt.Errorf("record name (--name) and type (--type) are required")
			}
			zoneID, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			record, err := svc.GetRecord(ctx, zoneID, name, recordType)
			if err != nil {
				return nil, fmt.Errorf("failed to get record: %w", err)
			}
			return record, nil
		}),
	})
}

// dnsRecordsUpdate is the `dns records update` operation. Identified by zone
// + --name + --type; changes --content/--ttl/--disabled; fields not provided
// are left unchanged.
func dnsRecordsUpdate(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_records_update",
		Title:       "Update a DNS record",
		Summary:     "Update a DNS record",
		Description: "Update an existing DNS record, identified by the zone's domain plus name and type. Change its content, ttl, or disabled state; fields not provided are left unchanged. Returns the updated record.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Record name (or @ for apex)"},
			{Name: "type", Type: catalog.ArgTypeString, Required: true, Help: "Record type"},
			{Name: "content", Type: catalog.ArgTypeString, Help: "New record content"},
			{Name: "ttl", Type: catalog.ArgTypeInt, Default: "3600", Help: "New TTL in seconds (default 3600)"},
			{Name: "disabled", Type: catalog.ArgTypeBool, Default: "false", Help: "New disabled state"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			name := catalog.StrArg(input, "name", "")
			recordType := catalog.StrArg(input, "type", "")
			content := catalog.StrArg(input, "content", "")

			if err := validateDNSRecord(recordType, content); err != nil {
				return nil, err
			}
			if err := validateDNSRecordName(name); err != nil {
				return nil, err
			}

			ttlVal := catalog.IntArg(input, "ttl", 3600)
			if ttlVal == 0 {
				ttlVal = 3600
			}
			disabled := catalog.BoolArg(input, "disabled", false)

			record := ipfs.RecordRequest{
				Name:     name,
				Type:     recordType,
				Content:  content,
				Ttl:      &ttlVal,
				Disabled: &disabled,
			}

			zoneID, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			updated, err := svc.UpdateRecord(ctx, zoneID, name, recordType, record)
			if err != nil {
				return nil, fmt.Errorf("failed to update record: %w", err)
			}
			return updated, nil
		}),
	})
}

// dnsRecordsDelete is the `dns records delete` operation. Identified by zone
// + --name + --type. Destructive; the handler enforces the confirmation gate
// locally so all callers are covered.
func dnsRecordsDelete(d DNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "dns_records_delete",
		Title:       "Delete a DNS record",
		Summary:     "Delete a DNS record",
		Description: "Delete a DNS record, identified by the zone's domain plus name and type. DESTRUCTIVE and irreversible. Deletes one record only; to remove the whole zone use dns_zones_delete.",
		Category:    "core",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<domain>",
		Args: []catalog.OperationArg{
			{Name: "zone", Type: catalog.ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Record name (or @ for apex)"},
			{Name: "type", Type: catalog.ArgTypeString, Required: true, Help: "Record type"},
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Help: "Confirm the destructive operation (CLI maps --force here)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("dns_records_delete: confirmation is required to delete a record")
			}
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			arg := catalog.StrArg(input, "zone", "")
			if arg == "" {
				return nil, fmt.Errorf("domain or zone ID is required")
			}
			name := catalog.StrArg(input, "name", "")
			recordType := catalog.StrArg(input, "type", "")
			if name == "" || recordType == "" {
				return nil, fmt.Errorf("record name (--name) and type (--type) are required")
			}
			zoneID, err := resolveZoneID(ctx, svc, arg)
			if err != nil {
				return nil, err
			}
			if err := svc.DeleteRecord(ctx, zoneID, name, recordType); err != nil {
				return nil, fmt.Errorf("failed to delete record: %w", err)
			}
			return &DNSRecordDeleteResult{ZoneID: zoneID, Name: name, Type: recordType}, nil
		}),
	})
}

// ---- Domain/zone resolution helpers ----

// resolveZoneID resolves a domain name or numeric ID to a zone ID string. If
// arg is numeric, it is returned as-is; otherwise it searches by domain via
// the service's read-only ListZones.
func resolveZoneID(ctx context.Context, dnsService dns.Service, arg string) (string, error) {
	if _, err := strconv.Atoi(arg); err == nil {
		return arg, nil
	}

	zones, err := dnsService.ListZones(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up zone by domain: %w", err)
	}

	for _, z := range zones {
		if z.Domain == arg {
			return strconv.Itoa(z.Id), nil
		}
	}

	return "", fmt.Errorf("zone not found for domain %q", arg)
}

// resolveZoneByArg resolves a domain name or numeric ID to a full ZoneResponse.
func resolveZoneByArg(ctx context.Context, dnsService dns.Service, arg string) (*ipfs.ZoneResponse, error) {
	id, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return nil, err
	}
	return dnsService.GetZone(ctx, id)
}

// validateDomain validates a domain string (non-empty, parseable address).
func validateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if _, err := mail.ParseAddress("user@" + domain); err != nil {
		return fmt.Errorf("invalid domain format: %w", err)
	}
	return nil
}

// validateDNSRecord validates a record type/content pair before the record
// is sent to the API.
func validateDNSRecord(recordType, content string) error {
	switch strings.ToUpper(recordType) {
	case "A":
		if !isValidIPv4(content) {
			return fmt.Errorf("invalid IPv4 address for A record")
		}
	case "AAAA":
		if !isValidIPv6(content) {
			return fmt.Errorf("invalid IPv6 address for AAAA record")
		}
	case "CNAME", "MX", "NS":
		if !isValidDomain(content) {
			return fmt.Errorf("invalid domain for %s record", recordType)
		}
	case "TXT":
		if len(content) > 255 {
			return fmt.Errorf("TXT record content too long (max 255 characters)")
		}
	default:
		return fmt.Errorf("unsupported record type: %s", recordType)
	}
	return nil
}

func isValidIPv4(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.To4() != nil
}

func isValidIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.To4() == nil && parsedIP.To16() != nil
}

func isValidDomain(domain string) bool {
	if domain == "" {
		return false
	}

	// A single trailing dot denotes an absolute/FQDN name and is valid.
	trimmed := dnsname.TrimDot(domain)
	if trimmed == "" {
		return false
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}

// validateDNSRecordName validates a DNS record name. "@" is only valid as the
// sole character (apex shorthand).
func validateDNSRecordName(name string) error {
	name = dnsname.TrimDot(name)
	if name == "" || name == "@" {
		return nil
	}
	if strings.Contains(name, "@") {
		return fmt.Errorf("invalid DNS record name: \"@\" must be used alone for apex records; did you mean to omit --name or use --name @?")
	}
	return nil
}

// parseCommaSeparated splits a comma-separated string into a trimmed,
// non-empty []string (used for nameservers).
func parseCommaSeparated(input string) []string {
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
