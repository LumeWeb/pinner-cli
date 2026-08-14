package cli

import (
	"context"
	"fmt"
	"strconv"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/dnsutil"
)

// NOTE: the DNS command tree (newDNSCommand) is catalog-driven and lives in
// dns_wiring.go. The files below hold the DNS handler functions and validation
// helpers, exercised directly by the DNS handler tests. No urfave/cli command
// construction lives here; that presentation is owned by the catalog wiring
// layer.

// ===== HANDLERS =====

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

// validateDomain validates a domain string (non-empty, parseable address).

func validateDomain(domain string) error { return dnsutil.ValidateDomain(domain) }

// validateDNSRecord validates a record type/content pair before sending to the
// API. Implementation is shared with the catalog layer via internal/dnsutil.

func validateDNSRecord(recordType, content string) error {
	return dnsutil.ValidateDNSRecord(recordType, content)
}

func isValidIPv4(ip string) bool { return dnsutil.IsValidIPv4(ip) }

func isValidIPv6(ip string) bool { return dnsutil.IsValidIPv6(ip) }

func isValidDomain(domain string) bool { return dnsutil.IsValidDomain(domain) }

// validateDNSRecordName validates a DNS record name before sending to the API.
// @ is only valid as the sole character (apex shorthand).

func validateDNSRecordName(name string) error { return dnsutil.ValidateDNSRecordName(name) }

// parseCommaSeparated splits a comma-separated string into a trimmed,
// non-empty []string (used for nameservers).

func parseCommaSeparated(input string) []string { return dnsutil.ParseCommaSeparated(input) }
