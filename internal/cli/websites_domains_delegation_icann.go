package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// icannDelegationDriver renders the delegation UX for the ICANN namespace.
// Parent records (NS + DS) are configured at the user's registrar. When Pinner
// manages the DNS (managed == true) it serves the authoritative side, so only
// the parent (registrar) records are shown.
type icannDelegationDriver struct{}

func (i *icannDelegationDriver) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	d := result.Delegation
	if d == nil {
		// An ICANN binding with no bundle means Pinner holds nothing for this
		// name yet (e.g. not bound as a managed domain); there is nothing to
		// publish from here.
		output.Printfln("")
		output.Printfln("No delegation records are available for %s.", result.Domain)
		return
	}

	parentTitle := "Parent records (configure at your registrar)"

	if managed {
		output.Printfln("")
		output.Printfln("Point your registrar's nameservers to the records below.")
		output.Printfln("Pinner manages your DNS, so the authoritative side is handled for you.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
	} else {
		output.Printfln("")
		output.Printfln("Configure the parent records at your registrar, then point your DNS")
		output.Printfln("server at the authoritative records below.")
		printDelegationRecords(output, parentTitle, d.ParentRecords)
		printDelegationRecords(output, "Authoritative records (configure on your DNS server)", d.AuthoritativeRecords)
	}

	renderDelegationNameservers(output, d)
}
