package cli

import (
	"strings"

	"github.com/samber/lo"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// renderDelegationInstructions prints the provider-authored, namespace-aware
// guidance verbatim.
func renderDelegationInstructions(output Output, d *ipfs.DNSDelegation) {
	if d.Instructions == nil || *d.Instructions == "" {
		return
	}
	output.Printfln("")
	output.Printfln("Instructions:")
	output.Printfln("%s", *d.Instructions)
}

// renderDelegationMode prints the delegation mode when present.
func renderDelegationMode(output Output, d *ipfs.DNSDelegation) {
	if mode := lo.FromPtr(d.Mode); mode != "" {
		output.Printfln("")
		output.Printfln("Delegation mode: %s", mode)
	}
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
