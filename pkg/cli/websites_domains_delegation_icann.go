package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// icannDelegationDriver renders the delegation UX for the ICANN namespace,
// where the parent side is configured at the user's registrar.
type icannDelegationDriver struct{}

func (i *icannDelegationDriver) Render(output Output, result *ipfs.DomainResponse) {
	d := result.Delegation
	renderDelegationInstructions(output, d)
	printDelegationRecords(output, "Parent records (configure at your registrar)", d.ParentRecords)
	printDelegationRecords(output, "Authoritative records", d.AuthoritativeRecords)
	renderDelegationNameservers(output, d)
}
