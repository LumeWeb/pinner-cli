package cli

import (
	"context"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// validationRecordValue returns the full DNS TXT record value the server
// validates a website against: "<key>=<token>". The verification key is
// server-provided — the server embeds it as the first DNS label of the
// validation record host (e.g. host "pinner-verify.example.com" carries the
// key "pinner-verify") and validates the TXT record value as
// "<key>=<token>". It is never hardcoded here.
//
// Some server builds return ValidationToken already carrying the "key="
// prefix (see websitecore.StripValidationPrefix); strip any such prefix before
// prepending the derived key so the result is always "<key>=<token>", never a
// doubled "<key>=<key>=<token>". When no validation record host is available
// to derive a key from, the token is returned as-is.
func validationRecordValue(website *ipfs.WebsiteItem) string {
	token := website.ValidationToken
	if website.ValidationRecordHost == nil || *website.ValidationRecordHost == "" {
		return token
	}
	key, _, _ := strings.Cut(*website.ValidationRecordHost, ".")
	if key == "" {
		return token
	}
	// Normalize the token: only strip the "key=" prefix when the token
	// actually starts with the derived key, so we never emit a doubled prefix
	// (e.g. "pinner-verify=pinner-verify=abc123") AND never corrupt a token
	// that legitimately contains "=" as content (e.g. base64/URL-encoded
	// padding) but has no "key=" prefix.
	if strings.HasPrefix(token, key+"=") {
		token = token[len(key)+1:]
	}
	return key + "=" + token
}

func newWebsitesCommand() *cli.Command {
	// The websites parent is catalog-driven: the core website CRUD + status
	// subcommands (list, create, get, update, enable-ipns, delete, validate,
	// ssl status, config) and the domains tree (list/add/remove/verify/
	// dns-requirements/dane republish/update) are compiled from the canonical
	// operation catalog (internal/catalogops) — see catalog_websites_wiring.go.
	// The commands that are fundamentally interactive/IO are NOT representable
	// as pure data-returning handlers and remain hand-written:
	//   - websites wizard    — interactive stepwise creation session, mounted at
	//                          the top level of the websites parent.
	//   - domains wizard     — interactive domain-addition session, mounted
	//                          under the catalog-emitted `domains` parent.
	cmds := newWebsitesCatalogCommands()
	cmds = append(cmds, newWebsitesWizardCommand())

	// The domains tree is now catalog-compiled, but the interactive domains
	// wizard stays hand-written: find the catalog `domains` parent and mount it
	// there.
	for _, c := range cmds {
		if c.Name == "domains" {
			c.Commands = append(c.Commands, newWebsitesDomainsWizardCommand())
			break
		}
	}

	return &cli.Command{
		Name:     "websites",
		Category: "Management",
		Aliases:  []string{"website"},
		Usage:    "Manage websites",
		Description: `Manage websites: associate domain names with CIDs so your IPFS/IPNS content is served over your custom domains. Covers create/list/get/update/delete/validate, SSL certificate status, domain binding (websites domains), and enabling IPNS addressing (enable-ipns).

For raw DNS zone and record CRUD (A/AAAA/CNAME/TXT/MX/NS, _dnslink, apex vs subdomain), use the 'dns' command tree instead; websites only shows the DNS records your domain needs. Content addressing itself lives under 'ipns'.`,
		Commands: cmds,
	}
}

// getNameservers fetches the nameservers from the website hosting config.
func getNameservers(ctx context.Context, websitesService WebsitesService) []string {
	cfg, err := websitesService.GetConfig(ctx)
	if err != nil || cfg == nil || cfg.Nameservers == nil {
		return nil
	}
	return *cfg.Nameservers
}

// showDNSHostingInstructions displays NS delegation instructions when DNS hosting is enabled.
func showDNSHostingInstructions(output Output, website *ipfs.WebsiteItem, nameservers []string) {
	output.PrintListGroup(ListGroup{
		Title: "DNS hosting is enabled: Pinner manages your DNS records. Update your domain's nameservers at your registrar:",
	})

	if len(nameservers) > 0 {
		rows := make([][]string, len(nameservers))
		for i, ns := range nameservers {
			rows[i] = []string{website.Domain, "NS", ns}
		}
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, rows)
	} else {
		output.PrintListGroup(ListGroup{
			Title:  "Use: pinner websites config",
			Footer: "To find the required nameservers.",
			PadTop: 1,
		})
	}
}

// showSelfManagedDNSInstructions displays required DNS records for self-managed DNS.
func showSelfManagedDNSInstructions(output Output, website *ipfs.WebsiteItem) {
	output.Printfln("Required DNS records:")

	validationHost := website.Domain
	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		validationHost = *website.ValidationRecordHost
	}

	records := [][]string{
		{validationHost, "TXT", validationRecordValue(website)},
		{"_dnslink." + website.Domain, "TXT", "dnslink=/" + website.TargetType + "/" + website.TargetHash},
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, []string{website.Domain, "CNAME", *website.GatewayDomain})
	}

	output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, records)
}
