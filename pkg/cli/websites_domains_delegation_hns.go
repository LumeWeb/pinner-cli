package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// hnsDelegationDriver renders the delegation UX for the HNS namespace. Parent
// records (NS + DS) are published on-chain in the HNS wallet's DNS/records
// area; there is no separate DNS server for HNS. When Pinner manages the DNS
// (managed == true) it also serves the authoritative side, so those records are
// not shown. In inline mode the authoritative side is served via Pinner's
// synthetic nameserver names.
type hnsDelegationDriver struct{}

func (h *hnsDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation

	parentTitle := "Parent records (publish in your HNS wallet)"
	authTitle := "Authoritative records (configure on your DNS server)"

	inline := d != nil && d.Mode != nil && *d.Mode == "inline"

	switch {
	case inline:
		output.Printfln("")
		output.Printfln("Publish the records below in the DNS/records area of your HNS wallet (on-chain).")
		output.Printfln("The authoritative side is served via Pinner's synthetic nameservers.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
		printDelegationRecords(output, "Authoritative records (served via synthetic nameservers)", d.AuthoritativeRecords)
	case managed:
		output.Printfln("")
		output.Printfln("Publish the records below in the DNS/records area of your HNS wallet (on-chain).")
		output.Printfln("Pinner manages your DNS, so the authoritative side is handled for you.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
	default:
		output.Printfln("")
		output.Printfln("Publish the parent records in the DNS/records area of your HNS wallet (on-chain),")
		output.Printfln("then point your own DNS server at the authoritative records below.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
		printDelegationRecords(output, authTitle, d.AuthoritativeRecords)
	}

	renderDelegationNameservers(output, d)
}
