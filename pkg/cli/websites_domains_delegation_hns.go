package cli

import (
	"github.com/samber/lo"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// hnsDelegationDriver renders the delegation UX for the HNS namespace, where
// parent records and the DS record are published in the HNS wallet/resource.
type hnsDelegationDriver struct{}

func (h *hnsDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation
	renderDelegationInstructions(output, d)
	renderDelegationMode(output, d)
	printDelegationRecords(output, "Parent records (publish in your HNS wallet / resource)", d.ParentRecords)

	// Authoritative records: when DNS is platform-managed, the platform is
	// authoritative for the zone (its PowerDNS serves the records) so the
	// user only publishes the parent records on-chain. There is no "your own
	// nameserver" to configure. Inlined mode serves them via synthetic
	// nameserver names; only an unmanaged delegated setup requires the user
	// to configure their own authoritative DNS server.
	title := "Authoritative records"
	switch {
	case managed:
		title = "Authoritative records (hosted by Pinner)"
	case lo.FromPtr(d.Mode) == "inline":
		title = "Authoritative records (served via synthetic nameservers)"
	case lo.FromPtr(d.Mode) == "delegated":
		title = "Authoritative records (configure on your nameserver)"
	}
	printDelegationRecords(output, title, d.AuthoritativeRecords)
	renderDelegationNameservers(output, d)
}
