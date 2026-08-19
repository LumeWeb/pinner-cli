// Package dnsutil holds pure, dependency-free DNS string validators shared by
// the catalog operation layer (internal/catalogops) and the CLI layer
// (internal/cli). Keeping these in one home prevents drift: before this package
// existed, validateDNSRecord was duplicated and diverged (the CLI copy rejected
// SRV/CAA/PTR/SOA while the catalog copy accepted them).
package dnsutil

import (
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"

	"go.lumeweb.com/ipfs-sdk/dnsname"
)

// ParseCommaSeparated splits a comma-separated string into a trimmed,
// non-empty []string (used for nameservers).
func ParseCommaSeparated(input string) []string {
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

// ValidateDomain validates a domain string (non-empty, parseable address).
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if _, err := mail.ParseAddress("user@" + domain); err != nil {
		return fmt.Errorf("invalid domain format: %w", err)
	}
	return nil
}

// ValidateDNSRecord validates a record type/content pair before the record is
// sent to the API.
func ValidateDNSRecord(recordType, content string) error {
	switch strings.ToUpper(recordType) {
	case "A":
		if !IsValidIPv4(content) {
			return fmt.Errorf("invalid IPv4 address for A record")
		}
	case "AAAA":
		if !IsValidIPv6(content) {
			return fmt.Errorf("invalid IPv6 address for AAAA record")
		}
	case "CNAME", "MX", "NS", "PTR":
		if !IsValidDomain(content) {
			return fmt.Errorf("invalid domain for %s record", recordType)
		}
	case "TXT":
		// RFC 1035 limits each TXT *string* to 255 octets, but a TXT record
		// value is a list of such strings — DKIM1 keys and long SPF records
		// legitimately exceed 255 bytes, and PowerDNS (the backend) chunks
		// them automatically. So there is no single-value 255 cap here: the
		// only guard is an absurd sanity cap against typos (bytes, matching
		// the wire's octet semantics). A value over this is almost certainly
		// garbage, not a real record.
		if len(content) > 65535 {
			return fmt.Errorf("TXT record content too long (max 65535 bytes)")
		}
	case "SRV":
		if err := validateSRV(content); err != nil {
			return err
		}
	case "CAA":
		if err := validateCAA(content); err != nil {
			return err
		}
	case "SOA":
		if err := validateSOA(content); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported record type: %s", recordType)
	}
	return nil
}

// IsValidIPv4 reports whether s is a valid IPv4 address.
func IsValidIPv4(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.To4() != nil
}

// IsValidIPv6 reports whether s is a valid IPv6 address.
func IsValidIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.To4() == nil && parsedIP.To16() != nil
}

// IsValidDomain reports whether s is a valid DNS host/record target domain. A
// single trailing dot denotes an absolute/FQDN name and is valid; the
// terminating dot is the DNS root separator, not an empty label.
func IsValidDomain(domain string) bool {
	if domain == "" {
		return false
	}

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

// ValidateDNSRecordName validates a DNS record name. "@" is only valid as the
// sole character (apex shorthand).
func ValidateDNSRecordName(name string) error {
	name = dnsname.TrimDot(name)
	if name == "" || name == "@" {
		return nil
	}
	if strings.Contains(name, "@") {
		return fmt.Errorf("invalid DNS record name: \"@\" must be used alone for apex records; did you mean to omit --name or use --name @?")
	}
	return nil
}

// validateSRV validates SRV record content: "<priority> <weight> <port> <target>"
// in whitespace-separated form (as PowerDNS stores it).
func validateSRV(content string) error {
	fields := strings.Fields(content)
	if len(fields) != 4 {
		return fmt.Errorf("SRV record content must be \"priority weight port target\" (e.g. \"10 60 5060 sip.example.com\")")
	}
	priority, e1 := strconv.ParseUint(fields[0], 10, 16)
	weight, e2 := strconv.ParseUint(fields[1], 10, 16)
	port, e3 := strconv.ParseUint(fields[2], 10, 16)
	if e1 != nil || e2 != nil || e3 != nil {
		return fmt.Errorf("SRV priority, weight, and port must be integers (0-65535)")
	}
	if priority > 65535 || weight > 65535 {
		return fmt.Errorf("SRV priority and weight must be between 0 and 65535")
	}
	if port == 0 || port > 65535 {
		return fmt.Errorf("SRV port must be between 1 and 65535")
	}
	if !IsValidDomain(fields[3]) {
		return fmt.Errorf("SRV target must be a domain (e.g. sip.example.com)")
	}
	return nil
}

// validateCAA validates CAA record content: "<flags> <tag> [value]". Only the
// flags and tag are required per RFC 8659; the value is optional (a value-less
// "0 issue" record blocks all CA issuance).
func validateCAA(content string) error {
	fields := strings.Fields(content)
	if len(fields) < 2 {
		return fmt.Errorf("CAA record content must be \"flags tag [value]\" (e.g. \"0 issue letsencrypt.org\")")
	}
	// ParseUint with bitSize 8 already rejects values outside 0-255, so no
	// separate range check is needed.
	if _, err := strconv.ParseUint(fields[0], 10, 8); err != nil {
		return fmt.Errorf("CAA flags must be an integer between 0 and 255")
	}
	tag := strings.ToLower(strings.TrimSuffix(fields[1], "."))
	switch tag {
	case "issue", "issuewild", "iodef":
	default:
		return fmt.Errorf("CAA tag must be one of issue, issuewild, or iodef (got %q)", fields[1])
	}
	return nil
}

// validateSOA validates SOA record content:
// "<mname> <rname> <serial> <refresh> <retry> <expire> <minimum>".
func validateSOA(content string) error {
	fields := strings.Fields(content)
	if len(fields) != 7 {
		return fmt.Errorf("SOA record content must be \"mname rname serial refresh retry expire minimum\" (7 fields)")
	}
	if !IsValidDomain(fields[0]) {
		return fmt.Errorf("SOA primary nameserver (mname) must be a domain")
	}
	if !IsValidDomain(fields[1]) {
		return fmt.Errorf("SOA responsible party (rname) must be a domain")
	}
	for _, f := range fields[2:] {
		if _, err := strconv.ParseUint(f, 10, 32); err != nil {
			return fmt.Errorf("SOA serial/refresh/retry/expire/minimum must be non-negative integers")
		}
	}
	return nil
}
