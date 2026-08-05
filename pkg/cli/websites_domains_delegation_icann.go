package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// icannDelegationDriver renders the delegation UX for the ICANN namespace,
// where the parent side is configured at the user's registrar.
type icannDelegationDriver struct{}

func (i *icannDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation
	renderDelegationInstructions(output, d)
	printDelegationRecords(output, "Parent records (configure at your registrar)", d.ParentRecords)
	// When DNS is platform-managed, the platform is authoritative for the
	// zone (its PowerDNS serves the records); only an unmanaged setup has
	// the user serve the authoritative records on their own DNS server.
	title := "Authoritative records"
	if managed {
		title = "Authoritative records (hosted by Pinner)"
	}
	printDelegationRecords(output, title, d.AuthoritativeRecords)
	renderDelegationNameservers(output, d)
}
