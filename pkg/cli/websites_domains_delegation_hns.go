package cli

import (
	"github.com/samber/lo"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// hnsDelegationDriver renders the delegation UX for the HNS namespace, where
// parent records and the DS record are published in the HNS wallet/resource.
type hnsDelegationDriver struct{}

func (h *hnsDelegationDriver) Render(output Output, result *ipfs.DomainResponse) {
	d := result.Delegation
	renderDelegationInstructions(output, d)
	renderDelegationMode(output, d)
	printDelegationRecords(output, "Parent records (publish in your HNS wallet / resource)", d.ParentRecords)
	renderDelegationDS(output, d, "into your HNS wallet")

	// Authoritative records are only self-managed in delegated mode; in inline
	// mode they are served via synthetic nameserver names.
	title := "Authoritative records"
	switch lo.FromPtr(d.Mode) {
	case "delegated":
		title = "Authoritative records (configure on your nameserver)"
	case "inline":
		title = "Authoritative records (served via synthetic nameservers)"
	}
	printDelegationRecords(output, title, d.AuthoritativeRecords)
	renderDelegationNameservers(output, d)
}
