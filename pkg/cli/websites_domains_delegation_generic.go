package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// genericDelegationDriver is the neutral fallback for namespaces without a
// dedicated driver. It presents the provider-emitted guidance and record groups
// as-is.
type genericDelegationDriver struct{}

func (g *genericDelegationDriver) Render(output Output, result *ipfs.DomainResponse) {
	renderDelegationInstructions(output, result.Delegation)
	renderDelegationMode(output, result.Delegation)
	printDelegationRecords(output, "Parent records", result.Delegation.ParentRecords)
	renderDelegationDS(output, result.Delegation, "with your DNS provider")
	printDelegationRecords(output, "Authoritative records", result.Delegation.AuthoritativeRecords)
	renderDelegationNameservers(output, result.Delegation)
}
