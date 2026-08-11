package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// genericDelegationDriver is the neutral fallback for namespaces without a
// dedicated driver. It presents the record groups as-is with no
// namespace-specific copy.
type genericDelegationDriver struct{}

func (g *genericDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation
	printDelegationRecords(output, "Parent records", d.ParentRecords)
	if !managed {
		printDelegationRecords(output, "Authoritative records", d.AuthoritativeRecords)
	}
	renderDelegationNameservers(output, d)
}
