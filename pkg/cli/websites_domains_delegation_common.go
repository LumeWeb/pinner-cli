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

// renderDelegationDS prints the DS record when present.
//
// The Ds field is a DS record string whose leading tokens vary by source (some
// include owner/TTL/class, e.g. "mydomain. 3600 IN DS ...", others are
// briefer). A DS record's rdata is always the final four fields
// <key tag> <algorithm> <digest type> <digest>, so they are rendered as a
// labeled table. The table uses non-splitting wrap so a long digest stays on
// one line and remains copyable. label describes where the value is pasted
// (e.g. the HNS wallet vs a registrar).
func renderDelegationDS(output Output, d *ipfs.DNSDelegation, label string) {
	if d.Ds == nil || *d.Ds == "" {
		return
	}
	output.Printfln("")
	output.Printfln("DS record (paste %s):", label)
	tokens := strings.Fields(*d.Ds)
	if len(tokens) < 4 {
		// Not parseable as DS rdata; show the raw value in a single column.
		output.PrintTableUnwrapped([]string{"DS RECORD"}, [][]string{{*d.Ds}})
		return
	}
	keyTag := tokens[len(tokens)-4]
	algorithm := tokens[len(tokens)-3]
	digestType := tokens[len(tokens)-2]
	digest := tokens[len(tokens)-1]
	output.PrintTableUnwrapped([]string{"KEY TAG", "ALGORITHM", "DIGEST TYPE", "DIGEST"}, [][]string{
		{keyTag, algorithm, digestType, digest},
	})
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
		rows = append(rows, []string{r.Type, value})
	}
	output.Printfln("")
	output.Printfln("%s", title)
	output.PrintTable([]string{"TYPE", "VALUE"}, rows)
}
