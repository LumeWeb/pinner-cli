package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// genericDelegationDriver is the neutral fallback for namespaces without a
// dedicated driver. It presents the provider-emitted guidance and record groups
// as-is.
type genericDelegationDriver struct{}

func (g *genericDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	renderDelegationInstructions(output, result.Delegation)
	renderDelegationMode(output, result.Delegation)
	printDelegationRecords(output, "Parent records", result.Delegation.ParentRecords)
	title := "Authoritative records"
	if managed {
		title = "Authoritative records (hosted by Pinner)"
	}
	printDelegationRecords(output, title, result.Delegation.AuthoritativeRecords)
	renderDelegationNameservers(output, result.Delegation)
}
