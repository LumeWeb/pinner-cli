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
