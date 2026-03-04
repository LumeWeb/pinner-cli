package cli

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

func newDNSCommand() *cli.Command {
	return &cli.Command{
		Name:  "dns",
		Usage: "Manage DNS zones and records",
		Description: `Manage DNS zones and records for your domains. DNS hosting allows you to
control DNS configuration for IPFS-hosted websites.

Zone operations:
  - List all DNS zones
  - Create a new DNS zone
  - Get zone details
  - Delete a DNS zone

Record operations:
  - List DNS records for a zone
  - Create DNS records
  - Get record details
  - Update DNS records
  - Delete DNS records

Examples:
  pinner dns zones list
  pinner dns zones create --domain example.com
  pinner dns zones get 1
  pinner dns zones delete 1
  pinner dns records list --zone-id 1
  pinner dns records create --zone-id 1 --name www --type CNAME --content example.com
  pinner dns records delete --zone-id 1 --name www --type CNAME`,
		Commands: []*cli.Command{
			newDNSZonesCommand(),
			newDNSRecordsCommand(),
		},
	}
}

// ===== ZONES =====

func newDNSZonesCommand() *cli.Command {
	return &cli.Command{
		Name:  "zones",
		Usage: "Manage DNS zones",
		Description: `Manage DNS zones for your domains.

Examples:
  pinner dns zones list
  pinner dns zones list --status active
  pinner dns zones create --domain example.com
  pinner dns zones create --domain example.com --nameservers ns1.example.com,ns2.example.com
  pinner dns zones get 1
  pinner dns zones delete 1`,
		Commands: []*cli.Command{
			newDNSZonesListCommand(),
			newDNSZonesCreateCommand(),
			newDNSZonesGetCommand(),
			newDNSZonesDeleteCommand(),
		},
	}
}

func newDNSZonesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all DNS zones",
		Description: `List all DNS zones for the authenticated user.

Examples:
  pinner dns zones list
  pinner dns zones list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsZonesList(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSZonesCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new DNS zone",
		Description: `Create a new DNS zone for a domain.

Examples:
  pinner dns zones create --domain example.com
  pinner dns zones create --domain example.com --nameservers ns1.example.com,ns2.example.com
  pinner dns zones create --domain example.com --json`,
		Flags: []cli.Flag{
			DomainFlag(),
			NameserversFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsZonesCreate(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSZonesGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get a DNS zone by ID",
		Description: `Get details of a specific DNS zone by its ID.

Examples:
  pinner dns zones get 1
  pinner dns zones get 1 --json`,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsZonesGet(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSZonesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a DNS zone",
		Description: `Delete a DNS zone and all its records.

Examples:
  pinner dns zones delete 1
  pinner dns zones delete 1 --force`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			ForceFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsZonesDelete(ctx, cmd, output, cfgMgr)
		},
	}
}

// ===== RECORDS =====

func newDNSRecordsCommand() *cli.Command {
	return &cli.Command{
		Name:  "records",
		Usage: "Manage DNS records",
		Description: `Manage DNS records for zones.

Examples:
  pinner dns records list --zone-id 1
  pinner dns records create --zone-id 1 --name www --type CNAME --content example.com
  pinner dns records get --zone-id 1 --name www --type CNAME
  pinner dns records update --zone-id 1 --name www --type CNAME --content new.example.com
  pinner dns records delete --zone-id 1 --name www --type CNAME`,
		Commands: []*cli.Command{
			newDNSRecordsListCommand(),
			newDNSRecordsCreateCommand(),
			newDNSRecordsGetCommand(),
			newDNSRecordsUpdateCommand(),
			newDNSRecordsDeleteCommand(),
		},
	}
}

func newDNSRecordsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List DNS records for a zone",
		Description: `List all DNS records for a specific zone.

Examples:
  pinner dns records list --zone-id 1
  pinner dns records list --zone-id 1 --json`,
		Flags: []cli.Flag{
			ZoneIDFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsRecordsList(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSRecordsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a DNS record",
		Description: `Create a new DNS record in a zone.

Examples:
  pinner dns records create --zone-id 1 --name www --type CNAME --content example.com
  pinner dns records create --zone-id 1 --name _dnslink --type TXT --content "/ipfs/QmHash" --ttl 3600
  pinner dns records create --zone-id 1 --name @ --type A --content 192.168.1.1 --json`,
		Flags: []cli.Flag{
			ZoneIDFlag(),
			NameFlag("DNS record name"),
			TypeFlag(),
			ContentFlag(),
			TTLFlag(),
			DisabledFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsRecordsCreate(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSRecordsGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get a DNS record",
		Description: `Get details of a specific DNS record.

Examples:
  pinner dns records get --zone-id 1 --name www --type CNAME
  pinner dns records get --zone-id 1 --name www --type CNAME --json`,
		Flags: []cli.Flag{
			ZoneIDFlag(),
			NameFlag("DNS record name"),
			TypeFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsRecordsGet(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSRecordsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a DNS record",
		Description: `Update an existing DNS record.

Examples:
  pinner dns records update --zone-id 1 --name www --type CNAME --content new.example.com
  pinner dns records update --zone-id 1 --name www --type CNAME --content new.example.com --ttl 7200
  pinner dns records update --zone-id 1 --name www --type CNAME --json`,
		Flags: []cli.Flag{
			ZoneIDFlag(),
			NameFlag("DNS record name"),
			TypeFlag(),
			ContentFlag(),
			TTLFlag(),
			DisabledFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsRecordsUpdate(ctx, cmd, output, cfgMgr)
		},
	}
}

func newDNSRecordsDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a DNS record",
		Description: `Delete a DNS record from a zone.

Examples:
  pinner dns records delete --zone-id 1 --name www --type CNAME`,
		Flags: []cli.Flag{
			ZoneIDFlag(),
			NameFlag("DNS record name"),
			TypeFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return dnsRecordsDelete(ctx, cmd, output, cfgMgr)
		},
	}
}

// ===== HANDLERS =====

func dnsZonesList(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	zones, err := dnsService.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(zones)
	} else {
		if len(zones) == 0 {
			output.Printf("No DNS zones found")
			return nil
		}

		output.Printf("DNS Zones:")
		for _, zone := range zones {
			output.Printf("  ID: %d", zone.Id)
			output.Printf("  Domain: %s", zone.Domain)
			output.Printf("  Status: %s", zone.Status)
			if zone.PowerdnsZoneId != nil {
				output.Printf("  PowerDNS Zone ID: %s", *zone.PowerdnsZoneId)
			}
			output.Printf("  Created: %s", zone.CreatedAt.Format("2006-01-02 15:04:05"))
			output.Printf("")
		}
	}

	return nil
}

func dnsZonesCreate(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	domain := cmd.String(FlagDomain)
	if domain == "" {
		return fmt.Errorf("--domain is required")
	}

	if err := validateDomain(domain); err != nil {
		return err
	}

	nameserversStr := cmd.String(FlagNameservers)
	var nameservers []string
	if nameserversStr != "" {
		nameservers = parseCommaSeparated(nameserversStr)
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	zone, err := dnsService.CreateZone(ctx, domain, nameservers)
	if err != nil {
		return fmt.Errorf("failed to create zone: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(zone)
	} else {
		output.Printf("DNS zone created successfully:")
		output.Printf("  ID: %d", zone.Id)
		output.Printf("  Domain: %s", zone.Domain)
		output.Printf("  Status: %s", zone.Status)
		if zone.PowerdnsZoneId != nil {
			output.Printf("  PowerDNS Zone ID: %s", *zone.PowerdnsZoneId)
		}
	}

	return nil
}

func dnsZonesGet(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	if cmd.NArg() < 1 {
		return fmt.Errorf("zone ID is required")
	}

	id := cmd.Args().Get(0)

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	zone, err := dnsService.GetZone(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get zone: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(zone)
	} else {
		output.Printf("DNS Zone Details:")
		output.Printf("  ID: %d", zone.Id)
		output.Printf("  Domain: %s", zone.Domain)
		output.Printf("  Status: %s", zone.Status)
		if zone.PowerdnsZoneId != nil {
			output.Printf("  PowerDNS Zone ID: %s", *zone.PowerdnsZoneId)
		}
		output.Printf("  Created: %s", zone.CreatedAt.Format("2006-01-02 15:04:05"))
		output.Printf("  Updated: %s", zone.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

func dnsZonesDelete(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	if cmd.NArg() < 1 {
		return fmt.Errorf("zone ID is required")
	}

	id := cmd.Args().Get(0)

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	err := dnsService.DeleteZone(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete zone: %w", err)
	}

	output.Printf("DNS zone deleted successfully")
	return nil
}

func dnsRecordsList(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	zoneID := cmd.String(FlagZoneID)
	if zoneID == "" {
		return fmt.Errorf("--zone-id is required")
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	records, err := dnsService.ListRecords(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("failed to list records: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(records)
	} else {
		if len(records) == 0 {
			output.Printf("No DNS records found")
			return nil
		}

		output.Printf("DNS Records:")
		for _, record := range records {
			output.Printf("  ID: %d", record.Id)
			output.Printf("  Name: %s", record.Name)
			output.Printf("  Type: %s", record.Type)
			output.Printf("  Content: %s", record.Content)
			output.Printf("  TTL: %d", record.Ttl)
			if record.Disabled {
				output.Printf("  Status: disabled")
			}
			output.Printf("")
		}
	}

	return nil
}

func dnsRecordsCreate(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	zoneID := cmd.String(FlagZoneID)
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)
	content := cmd.String(FlagContent)

	if zoneID == "" {
		return fmt.Errorf("--zone-id is required")
	}
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if recordType == "" {
		return fmt.Errorf("--type is required")
	}
	if content == "" {
		return fmt.Errorf("--content is required")
	}

	if err := validateDNSRecord(recordType, content); err != nil {
		return err
	}

	ttlVal := int(cmd.Uint(FlagTTL))
	if ttlVal == 0 {
		ttlVal = 3600
	}

	disabled := cmd.Bool(FlagDisabled)

	record := ipfsclient.RecordRequest{
		Name:     name,
		Type:     recordType,
		Content:  content,
		Ttl:      &ttlVal,
		Disabled: &disabled,
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	created, err := dnsService.CreateRecord(ctx, zoneID, record)
	if err != nil {
		return fmt.Errorf("failed to create record: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(created)
	} else {
		output.Printf("DNS record created successfully:")
		output.Printf("  ID: %d", created.Id)
		output.Printf("  Name: %s", created.Name)
		output.Printf("  Type: %s", created.Type)
		output.Printf("  Content: %s", created.Content)
		output.Printf("  TTL: %d", created.Ttl)
	}

	return nil
}

func dnsRecordsGet(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	zoneID := cmd.String(FlagZoneID)
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)

	if zoneID == "" {
		return fmt.Errorf("--zone-id is required")
	}
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if recordType == "" {
		return fmt.Errorf("--type is required")
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	record, err := dnsService.GetRecord(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to get record: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(record)
	} else {
		output.Printf("DNS Record Details:")
		output.Printf("  ID: %d", record.Id)
		output.Printf("  Name: %s", record.Name)
		output.Printf("  Type: %s", record.Type)
		output.Printf("  Content: %s", record.Content)
		output.Printf("  TTL: %d", record.Ttl)
		if record.Disabled {
			output.Printf("  Status: disabled")
		}
		output.Printf("  Created: %s", record.CreatedAt.Format("2006-01-02 15:04:05"))
		output.Printf("  Updated: %s", record.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

func dnsRecordsUpdate(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	zoneID := cmd.String(FlagZoneID)
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)
	content := cmd.String(FlagContent)

	if zoneID == "" {
		return fmt.Errorf("--zone-id is required")
	}
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if recordType == "" {
		return fmt.Errorf("--type is required")
	}
	if content == "" {
		return fmt.Errorf("--content is required")
	}

	if err := validateDNSRecord(recordType, content); err != nil {
		return err
	}

	ttlVal := int(cmd.Uint(FlagTTL))
	if ttlVal == 0 {
		ttlVal = 3600
	}

	disabled := cmd.Bool(FlagDisabled)

	record := ipfsclient.RecordRequest{
		Name:     name,
		Type:     recordType,
		Content:  content,
		Ttl:      &ttlVal,
		Disabled: &disabled,
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	updated, err := dnsService.UpdateRecord(ctx, zoneID, name, recordType, record)
	if err != nil {
		return fmt.Errorf("failed to update record: %w", err)
	}

	if output.IsJSON() {
		output.PrintJSON(updated)
	} else {
		output.Printf("DNS record updated successfully:")
		output.Printf("  ID: %d", updated.Id)
		output.Printf("  Name: %s", updated.Name)
		output.Printf("  Type: %s", updated.Type)
		output.Printf("  Content: %s", updated.Content)
		output.Printf("  TTL: %d", updated.Ttl)
	}

	return nil
}

func dnsRecordsDelete(ctx context.Context, cmd *cli.Command, output Output, cfgMgr config.Manager) error {
	zoneID := cmd.String(FlagZoneID)
	name := cmd.String(FlagName)
	recordType := cmd.String(FlagType)

	if zoneID == "" {
		return fmt.Errorf("--zone-id is required")
	}
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if recordType == "" {
		return fmt.Errorf("--type is required")
	}

	var dnsService DNSService

	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	if authToken != "" {
		dnsService = NewDNSService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		dnsService = defaultDNSServiceFactory(cfgMgr, output)
	}

	if err := dnsService.RequireAuthenticated(); err != nil {
		return err
	}

	err := dnsService.DeleteRecord(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}

	output.Printf("DNS record deleted successfully")
	return nil
}

// ===== HELPER FUNCTIONS =====

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

	parts := strings.Split(domain, ".")
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
