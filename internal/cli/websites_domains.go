package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// The `websites domains` command tree (list/add/remove/verify/
// dns-requirements/dane republish/update) is now compiled from the operation
// catalog (see newWebsitesCatalogCommands in catalog_websites_wiring.go); only
// the interactive `websites domains wizard` subcommand stays hand-written (see
// websites_domains_wizard.go). This file retains the domain rendering helper
// shared by the domains feature.

// renderDomainDelegation prints the DNS delegation bundle the server computes
// for a domain. Rendering is driver-based: the namespace selects a
// context-specific driver (HNS, ICANN, ...) with a neutral generic fallback,
// matching the server's per-namespace DomainProvider design. managed indicates
// whether Pinner manages the domain's DNS, so drivers can omit authoritative
// records the user does not need to configure.
func renderDomainDelegation(output Output, result *ipfs.DomainResponse, managed bool) {
	output.Printfln("DNS requirements for %s", result.Domain)

	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	fields := []Field{
		{"Domain", result.Domain},
		{"Namespace", result.Namespace},
		{"Status", status},
	}
	// Surface the explicit DNSSEC state (enabled/disabled/error) + reason so an
	// absent DS on a managed namespace is diagnosable, not a silent gap.
	if result.Delegation != nil && result.Delegation.Dnssec != nil {
		fields = append(fields, Field{"DNSSEC", *result.Delegation.Dnssec})
		if result.Delegation.DnssecError != nil && *result.Delegation.DnssecError != "" {
			fields = append(fields, Field{"DNSSEC Error", *result.Delegation.DnssecError})
		}
	}
	output.PrintFields(FieldGroup{Fields: fields})

	if result.Delegation == nil {
		output.Printfln("No delegation records are available for %s.", result.Domain)
		return
	}

	defaultDelegationDriver.Render(output, result, managed)
}
