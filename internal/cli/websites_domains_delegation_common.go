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

// renderOnchainTLSA renders the TLSA record the user must publish alongside
// their on-chain records — browsers use it to verify the gateway's HTTPS
// certificate for on-chain names, so without it the site won't load over
// HTTPS. The record comes from the delegation bundle's TLSA entries, falling
// back to the response's tlsa_rdata field (schema v0.1.96). TLSA-bearing
// groups are rendered wherever they appear; on-chain domains get the record
// called out explicitly so it is never missed.
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
	output.Printfln("")
	output.Printfln("TLSA — publish this alongside your on-chain records so your site")
	output.Printfln("loads over HTTPS:")
	rows := make([][]string, 0, len(records))
	for _, r := range records {
		value := ""
		if r.Value != nil {
			value = *r.Value
		}
		rows = append(rows, []string{tlsaRecordType, value})
	}
	output.PrintTable([]string{"TYPE", "VALUE"}, rows)
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
