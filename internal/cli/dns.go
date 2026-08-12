package cli

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// NOTE: the DNS command tree (newDNSCommand) is catalog-driven and lives in
// dns_wiring.go. The files below hold the DNS handler functions and validation
// helpers, exercised directly by the DNS handler tests. No urfave/cli command
// construction lives here; that presentation is owned by the catalog wiring
// layer.

// ===== HANDLERS =====

func dnsZonesList(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zones, err := dnsService.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(zones); err != nil {
			return err
		}
	} else {
		if len(zones) == 0 {
			output.Printfln("No DNS zones found")
			return nil
		}

		output.Printfln("DNS Zones:")
		for i, zone := range zones {
			fields := []Field{
				{"ID", fmt.Sprintf("%d", zone.Id)},
				{"Domain", zone.Domain},
				{"Status", zone.Status},
			}
			if zone.PowerdnsZoneId != nil {
				fields = append(fields, Field{"PowerDNS Zone ID", *zone.PowerdnsZoneId})
			}
			fields = append(fields, Field{"Created", zone.CreatedAt.Format("2006-01-02 15:04:05")})
			output.PrintFields(FieldGroup{Fields: fields, PadTop: i})
		}
	}

	return nil
}

func dnsZonesCreate(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	domain := cmd.String(FlagDomain)

	if err := validateDomain(domain); err != nil {
		return err
	}

	nameserversStr := cmd.String(FlagNameservers)
	var nameservers []string
	if nameserversStr != "" {
		nameservers = parseCommaSeparated(nameserversStr)
	}

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zone, err := dnsService.CreateZone(ctx, domain, nameservers)
	if err != nil {
		return fmt.Errorf("failed to create zone: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(zone); err != nil {
			return err
		}
	} else {
		fields := []Field{
			{"ID", fmt.Sprintf("%d", zone.Id)},
			{"Domain", zone.Domain},
			{"Status", zone.Status},
		}
		if zone.PowerdnsZoneId != nil {
			fields = append(fields, Field{"PowerDNS Zone ID", *zone.PowerdnsZoneId})
		}
		output.PrintFields(FieldGroup{Title: "DNS zone created successfully:", Fields: fields})
	}

	return nil
}

func dnsZonesGet(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zone, err := resolveZoneByArg(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		if err := output.PrintJSON(zone); err != nil {
			return err
		}
	} else {
		fields := []Field{
			{"ID", fmt.Sprintf("%d", zone.Id)},
			{"Domain", zone.Domain},
			{"Status", zone.Status},
		}
		if zone.PowerdnsZoneId != nil {
			fields = append(fields, Field{"PowerDNS Zone ID", *zone.PowerdnsZoneId})
		}
		fields = append(fields,
			Field{"Created", zone.CreatedAt.Format("2006-01-02 15:04:05")},
			Field{"Updated", zone.UpdatedAt.Format("2006-01-02 15:04:05")},
		)
		output.PrintFields(FieldGroup{Title: "DNS Zone Details:", Fields: fields})
	}

	return nil
}

func dnsZonesDelete(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	id, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	err = dnsService.DeleteZone(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete zone: %w", err)
	}

	output.Printfln("DNS zone %s deleted successfully", arg)
	return nil
}

func dnsZonesValidate(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zone, err := resolveZoneByArg(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	result, err := dnsService.ValidateZone(ctx, fmt.Sprintf("%d", zone.Id))
	if err != nil {
		return fmt.Errorf("failed to validate zone: %w", err)
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	statusIcon := "⏳"
	if result.Valid {
		statusIcon = "✅"
	}

	output.Printfln("DNS Zone Validation for %s", zone.Domain)

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Valid", fmt.Sprintf("%s %t", statusIcon, result.Valid)},
			{"Message", result.Message},
			{"Checked At", result.CheckedAt.Format("2006-01-02 15:04:05")},
		},
	})

	if !result.Valid {
		output.Printfln("")
		output.Printfln("Next steps:")
		if result.Nameservers != nil && len(*result.Nameservers) > 0 {
			output.Printfln("  Update your domain's nameservers at your registrar to:")
			for _, ns := range *result.Nameservers {
				output.Printfln("    - %s", ns)
			}
		} else {
			output.Printfln("  Check that your domain's nameservers are properly delegated to Pinner.xyz")
		}
		output.Printfln("  Then re-run: pinner dns zones validate %s", zone.Domain)
	}

	return nil
}

func dnsRecordsList(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zoneID, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	records, err := dnsService.ListRecords(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("failed to list records: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(records); err != nil {
			return err
		}
	} else {
		if len(records) == 0 {
			output.Printfln("No DNS records found")
			return nil
		}

		output.Printfln("DNS Records:")
		headers, rows := dnsRecordsTable(records)
		output.PrintTable(headers, rows)
	}

	return nil
}

// dnsRecordsTable builds the header and row data for the DNS records list
// table. Rendered by the human formatter via Output.PrintTable; kept separate
// so the row mapping is testable without depending on pterm rendering.
func dnsRecordsTable(records []ipfs.RecordResponse) ([]string, [][]string) {
	headers := []string{"NAME", "TYPE", "CONTENT", "TTL", "STATUS"}
	rows := make([][]string, 0, len(records))
	for _, record := range records {
		name := record.Name
		if name == "" {
			// blank name denotes the zone apex record
			name = "@"
		}
		status := ""
		if record.Disabled {
			status = "disabled"
		}
		rows = append(rows, []string{
			name,
			record.Type,
			record.Content,
			fmt.Sprintf("%d", record.Ttl),
			status,
		})
	}
	return headers, rows
}

func dnsRecordsCreate(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)
	content := cmd.String(FlagContent)

	if err := validateDNSRecord(recordType, content); err != nil {
		return err
	}

	if err := validateDNSRecordName(name); err != nil {
		return err
	}

	ttlVal := int(cmd.Uint(FlagTTL))
	if ttlVal == 0 {
		ttlVal = 3600
	}

	disabled := cmd.Bool(FlagDisabled)

	record := ipfs.RecordRequest{
		Name:     name,
		Type:     recordType,
		Content:  content,
		Ttl:      &ttlVal,
		Disabled: &disabled,
	}

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zoneID, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	created, err := dnsService.CreateRecord(ctx, zoneID, record)
	if err != nil {
		return fmt.Errorf("failed to create record: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(created); err != nil {
			return err
		}
	} else {
		output.PrintFields(FieldGroup{
			Title: "DNS record created successfully:",
			Fields: []Field{
				{"Zone ID", fmt.Sprintf("%d", created.ZoneId)},
				{"Name", created.Name},
				{"Type", created.Type},
				{"Content", created.Content},
				{"TTL", fmt.Sprintf("%d", created.Ttl)},
			},
		})
	}

	return nil
}

func dnsRecordsGet(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zoneID, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	record, err := dnsService.GetRecord(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to get record: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(record); err != nil {
			return err
		}
	} else {
		fields := []Field{
			{"Zone ID", fmt.Sprintf("%d", record.ZoneId)},
			{"Name", record.Name},
			{"Type", record.Type},
			{"Content", record.Content},
			{"TTL", fmt.Sprintf("%d", record.Ttl)},
		}
		if record.Disabled {
			fields = append(fields, Field{"Status", "disabled"})
		}
		output.PrintFields(FieldGroup{Title: "DNS Record Details:", Fields: fields})
	}

	return nil
}

func dnsRecordsUpdate(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)
	content := cmd.String(FlagContent)

	if err := validateDNSRecord(recordType, content); err != nil {
		return err
	}

	if err := validateDNSRecordName(name); err != nil {
		return err
	}

	ttlVal := int(cmd.Uint(FlagTTL))
	if ttlVal == 0 {
		ttlVal = 3600
	}

	disabled := cmd.Bool(FlagDisabled)

	record := ipfs.RecordRequest{
		Name:     name,
		Type:     recordType,
		Content:  content,
		Ttl:      &ttlVal,
		Disabled: &disabled,
	}

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zoneID, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	updated, err := dnsService.UpdateRecord(ctx, zoneID, name, recordType, record)
	if err != nil {
		return fmt.Errorf("failed to update record: %w", err)
	}

	if output.IsJSON() {
		if err := output.PrintJSON(updated); err != nil {
			return err
		}
	} else {
		output.PrintFields(FieldGroup{
			Title: "DNS record updated successfully:",
			Fields: []Field{
				{"Zone ID", fmt.Sprintf("%d", updated.ZoneId)},
				{"Name", updated.Name},
				{"Type", updated.Type},
				{"Content", updated.Content},
				{"TTL", fmt.Sprintf("%d", updated.Ttl)},
			},
		})
	}

	return nil
}

func dnsRecordsDelete(ctx context.Context, cmd dnsCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain or zone ID is required")
	}

	arg := args.First()
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)

	dnsService, err := newDNSAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	zoneID, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return err
	}

	err = dnsService.DeleteRecord(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}

	output.Printfln("DNS record deleted successfully")
	return nil
}

// ===== HELPERS =====

// resolveZoneID resolves a domain name or numeric ID to a zone ID string.
// If arg is numeric, it's returned as-is. Otherwise, it searches by domain via ListZones.
func resolveZoneID(ctx context.Context, dnsService DNSService, arg string) (string, error) {
	if _, err := strconv.Atoi(arg); err == nil {
		return arg, nil
	}

	zones, err := dnsService.ListZones(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up zone by domain: %w", err)
	}

	for _, z := range zones {
		if z.Domain == arg {
			return fmt.Sprintf("%d", z.Id), nil
		}
	}

	return "", fmt.Errorf("zone not found for domain %q", arg)
}

// resolveZoneByArg resolves a domain name or numeric ID to a full ZoneResponse.
func resolveZoneByArg(ctx context.Context, dnsService DNSService, arg string) (*ipfs.ZoneResponse, error) {
	id, err := resolveZoneID(ctx, dnsService, arg)
	if err != nil {
		return nil, err
	}
	return dnsService.GetZone(ctx, id)
}

func validateCID(cid string) error {
	if cid == "" {
		return fmt.Errorf("CID cannot be empty")
	}
	return nil
}

func validateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	if _, err := mail.ParseAddress("user@" + domain); err != nil {
		return fmt.Errorf("invalid domain format: %w", err)
	}

	return nil
}

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

	// A single trailing dot denotes an absolute/FQDN name and is valid
	// (e.g. "ns1.example.com."). The terminating dot is the DNS root
	// separator, not an empty label. Strip it before validating labels.
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

// validateDNSRecordName validates a DNS record name before sending to the API.
// @ is only valid as the sole character (apex shorthand).
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
