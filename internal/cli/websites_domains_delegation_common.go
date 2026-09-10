package cli

import (
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// statusOf returns the domain response's status string, "" when unset.
func statusOf(r *ipfs.DomainResponse) ipfs.DomainResponseStatus {
	if r == nil || r.Status == nil {
		return ""
	}
	return *r.Status
}

// statusOnchainManaged reports whether an HNS domain binding is on-chain
// managed: its DNS is served by an external contract, so there is no portal
// delegation to publish.
func statusOnchainManaged(r *ipfs.DomainResponse) bool {
	return statusOf(r) == ipfs.DomainResponseStatusOnchainManaged
}

// renderDelegationNameservers prints the nameservers shortcut when present.
func renderDelegationNameservers(output Output, d *ipfs.DNSDelegation) {
	if d.Nameservers == nil || len(*d.Nameservers) == 0 {
		return
	}
	output.Printfln("")
	output.Printfln("Nameservers:")
	output.PrintList(*d.Nameservers)
}

// printDelegationRecords renders a group of DNS records as a TYPE/VALUE table.
func printDelegationRecords(output Output, title string, records *[]ipfs.DNSDelegationRecord) {
	if records == nil || len(*records) == 0 {
		return
	}
	rows := make([][]string, 0, len(*records))
	for _, r := range *records {
		value := ""
		if r.Value != nil {
			value = *r.Value
		}
		// A nameserver record may carry multiple nameservers comma-joined in a
		// single value (e.g. "ns1.pinner.xyz,ns2.pinner.xyz"). Render each on
		// its own row so all nameservers are visible rather than packed into
		// one cell.
		if r.Type == "NS" && strings.Contains(value, ",") {
			for _, ns := range strings.Split(value, ",") {
				rows = append(rows, []string{r.Type, strings.TrimSpace(ns)})
			}
			continue
		}
		rows = append(rows, []string{r.Type, value})
	}
	output.Printfln("")
	output.Printfln("%s", title)
	output.PrintTable([]string{"TYPE", "VALUE"}, rows)
}

// renderValidationChecks turns the per-record checks the backend computes for
// a domain or website (validation token, dnslink, TLSA, delegation...) into
// an actionable to-do list: a short summary of how many are fine, then the
// ones needing attention with the exact record value to publish. Passing
// checks are summarized rather than listed — a wall of green rows does not
// help the user act; the missing/incorrect records do.
func renderValidationChecks(output Output, checks *[]ipfs.ValidationCheck) {
	if checks == nil || len(*checks) == 0 {
		return
	}
	var failing []ipfs.ValidationCheck
	passing := 0
	for _, c := range *checks {
		if c.Ok {
			passing++
			continue
		}
		failing = append(failing, c)
	}
	switch {
	case len(failing) == 0:
		output.Printfln("")
		output.Printfln("All %d record checks passed.", len(*checks))
		return
	case passing > 0:
		output.Printfln("")
		output.Printfln("%d of %d record checks passed. Fix the %d below:", passing, len(*checks), len(failing))
	default:
		output.Printfln("")
		output.Printfln("%d records need attention:", len(failing))
	}
	// Deliberately NOT a table: expected record values (dnslink, TLSA...) can
	// exceed the table wrap width, and a hard-wrapped value is neither
	// copyable nor honest. The expected value is printed on its own line,
	// verbatim, so it can be copied straight into the user's DNS.
	for _, c := range failing {
		output.Printfln("  • %s", c.Name)
		if c.Message != nil && *c.Message != "" {
			output.Printfln("      %s", *c.Message)
		}
		if c.Expected != nil && *c.Expected != "" {
			output.Printfln("      Publish this record:")
			output.Printfln("        %s", *c.Expected)
		}
		if c.Found != nil && *c.Found != "" {
			output.Printfln("      Found instead: %s", *c.Found)
		}
	}
}

// tlsaRecordType is the DNS resource-record type of the DANE TLSA record.
// The SDK models delegation record types as plain strings, so the constant
// lives here next to the only logic that filters on it.
const tlsaRecordType = "TLSA"

// tlsaRecords collects the TLSA records the user must publish on-chain, from
// the delegation bundle (authoritative group first).
func tlsaRecords(d *ipfs.DNSDelegation) []ipfs.DNSDelegationRecord {
	if d == nil {
		return nil
	}
	var out []ipfs.DNSDelegationRecord
	seen := map[string]bool{}
	add := func(records *[]ipfs.DNSDelegationRecord) {
		if records == nil {
			return
		}
		for _, r := range *records {
			if r.Type != tlsaRecordType {
				continue
			}
			value := ""
			if r.Value != nil {
				value = *r.Value
			}
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, r)
		}
	}
	add(d.AuthoritativeRecords)
	add(d.ParentRecords)
	return out
}

// tlsaOwnerName is the record name a DANE TLSA record for HTTPS lives under:
// the TCP port 443 service on the domain ("_443._tcp.<domain>"). Passing the
// bare rdata alone leaves the user to guess this; TLSA is never published at
// the domain apex.
func tlsaOwnerName(result *ipfs.DomainResponse) string {
	if result != nil && result.OwnerName != nil && *result.OwnerName != "" {
		return *result.OwnerName
	}
	if result != nil {
		return "_443._tcp." + result.Domain
	}
	return "_443._tcp."
}

// renderOnchainTLSA renders the TLSA record the user must publish alongside
// their on-chain records — browsers use it to verify the gateway's HTTPS
// certificate for on-chain names, so without it the site won't load over
// HTTPS. The record comes from the delegation bundle's TLSA entries, falling
// back to the response's owner_name/tlsa_rdata fields (schema v0.1.96+). The
// owner name (where the record goes) is always shown: bare rdata like
// "3 1 1 <digest>" is not publishable without knowing it belongs at
// _443._tcp.<domain>.
func renderOnchainTLSA(output Output, result *ipfs.DomainResponse, d *ipfs.DNSDelegation) {
	records := tlsaRecords(d)
	// The bundle frequently comes back nil on on-chain Managed bindings, so
	// the response-level tlsa_rdata (schema v0.1.96) is the usual source here.
	if len(records) == 0 && result != nil && result.TlsaRdata != nil && *result.TlsaRdata != "" {
		records = []ipfs.DNSDelegationRecord{{Type: tlsaRecordType, Value: result.TlsaRdata}}
	}
	if len(records) == 0 {
		// TODO: backend - return tlsa_rdata on on-chain bindings so the
		// onboarding story is complete. Until then, point the user at the
		// DANE republish command instead of leaving a silent gap.
		output.Printfln("")
		output.Printfln("This domain also needs a TLSA record published (it lets your")
		output.Printfln("site load over HTTPS). If it is missing, you can regenerate it")
		output.Printfln("with:")
		output.Printfln("  pinner websites domains dane republish <domain>")
		return
	}
	owner := tlsaOwnerName(result)
	output.Printfln("")
	output.Printfln("TLSA — publish this record at %s (the TCP port 443 service", owner)
	output.Printfln("of your domain) so your site loads over HTTPS:")
	// NAME/TYPE/VALUE so the owner name (_443._tcp.<domain>) is part of the
	// copyable record itself; keepWholeValue treats the digest in the VALUE
	// column of this layout as whole-token so it is never hard-wrapped.
	rows := make([][]string, 0, len(records))
	for _, r := range records {
		value := ""
		if r.Value != nil {
			value = *r.Value
		}
		rows = append(rows, []string{owner, tlsaRecordType, value})
	}
	output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, rows)
}
