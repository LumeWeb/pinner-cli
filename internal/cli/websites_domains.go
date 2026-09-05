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

// renderDomainVerifyResult turns a `websites domains verify` response into an
// outcome the user can act on, instead of the generic binding field table.
// valid mirrors the wizard's notion of a fully validated binding (active, or
// on-chain managed via the namespace TXT token). A nil response means the
// backend could not resolve the domain's DNS yet — still not verified.
func renderDomainVerifyResult(output Output, r *ipfs.DomainResponse) {
	if r == nil {
		output.Printfln("⏳ not verified yet")
		output.Printfln("  The domain's DNS could not be resolved, so validation didn't run.")
		output.Printfln("  DNS can take a while to propagate. Re-check with:")
		output.Printfln("    pinner websites domains verify <domain>")
		return
	}

	status := ""
	if r.Status != nil {
		status = string(*r.Status)
	}

	if domainStatusIsValid(statusOf(r)) {
		output.Printfln("✅ %s verified", r.Domain)
		output.Printfln("  Status: %s", status)
		output.Printfln("  Your site will be served at https://%s", r.Domain)
		if statusOnchainManaged(r) {
			// Verification only proves ownership through the on-chain TXT
			// token — it does not confirm the TLSA was published, and the
			// site won't load over HTTPS without it.
			output.Printfln("  Make sure the TLSA record is published so the site loads")
			output.Printfln("  over HTTPS:")
			output.Printfln("    pinner websites domains dns-requirements %s", r.Domain)
		}
		return
	}

	output.Printfln("⏳ %s is not verified yet", r.Domain)
	output.Printfln("  Status: %s", status)
	output.Printfln("  DNS can take a while to propagate. Re-check with:")
	output.Printfln("    pinner websites domains verify %s", r.Domain)
	output.Printfln("  Check the records the domain needs:")
	output.Printfln("    pinner websites domains dns-requirements %s", r.Domain)
}

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
		status = string(*result.Status)
	}
	fields := []Field{
		{"Domain", result.Domain},
		{"Namespace", string(result.Namespace)},
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

	// The registry renders even without a delegation bundle: a nil Delegation is
	// meaningful per-namespace (e.g. an HNS on-chain managed binding
	// serves its DNS from an external contract and has no records to publish),
	// so the driver owns the explanation instead of a generic miss here.
	defaultDelegationDriver.Render(output, result, managed)
}
