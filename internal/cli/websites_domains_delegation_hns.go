package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// hnsDelegationDriver renders the delegation UX for the HNS namespace. Parent
// records (NS + DS) are published on-chain in the HNS wallet's DNS/records
// area; there is no separate DNS server for HNS.
//
// The authoritative side is never configured by the user in HNS: it is either
// served by Pinner (managed DNS, or inline mode via synthetic nameservers) or
// by the user's own DNS server (self-managed delegated mode). Authoritative
// records are therefore shown only in the last case.
type hnsDelegationDriver struct{}

func (h *hnsDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation

	// On-chain managed or otherwise delegation-less HNS binding: the domain is
	// held on-chain and its DNS records are set on-chain, outside any
	// Pinner-managed zone. The crucial record to surface here is the TLSA —
	// without it published on-chain the site won't load over HTTPS.
	if statusOnchainManaged(result) {
		output.Printfln("")
		output.Printfln("%s is on-chain managed: this domain is held on-chain, so its", result.Domain)
		output.Printfln("DNS records are set on-chain rather than in a Pinner-managed zone.")
		output.Printfln("Set up the domain's on-chain DNS wherever you manage it.")
		renderOnchainTLSA(output, result, d)
		return
	}

	// Any other delegation-less HNS binding also has nothing to publish; the
	// status header above already identifies the binding to the user.
	if d == nil {
		output.Printfln("")
		output.Printfln("No delegation records are available for %s.", result.Domain)
		return
	}

	parentTitle := "Parent records (publish in your HNS wallet)"

	inline := d.Mode != nil && *d.Mode == "inline"

	// Inline mode serves the authoritative side via Pinner's synthetic
	// nameserver names; the user only publishes the parent (SYNTH) records.
	if inline {
		output.Printfln("")
		output.Printfln("Publish the records below in the DNS/records area of your HNS wallet (on-chain).")
		output.Printfln("The authoritative side is served via Pinner's synthetic nameservers.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
		renderDelegationNameservers(output, d)
		return
	}

	// Pinner-managed DNS: Pinner serves the authoritative side, so only the
	// parent records need publishing.
	if managed {
		output.Printfln("")
		output.Printfln("Publish the records below in the DNS/records area of your HNS wallet (on-chain).")
		output.Printfln("Pinner manages your DNS, so the authoritative side is handled for you.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
		renderDelegationNameservers(output, d)
		return
	}

	// Self-managed delegated: publish the parent records on-chain, then point
	// the user's own DNS server at the authoritative records.
	output.Printfln("")
	output.Printfln("Publish the parent records in the DNS/records area of your HNS wallet (on-chain),")
	output.Printfln("then point your own DNS server at the authoritative records below.")
	printDelegationRecords(output, parentTitle, d.ParentRecords)
	printDelegationRecords(output, "Authoritative records (configure on your DNS server)", d.AuthoritativeRecords)
	renderDelegationNameservers(output, d)
}
