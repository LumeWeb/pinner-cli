package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newWebsitesDomainsCommand() *cli.Command {
	return &cli.Command{
		Name:    "domains",
		Aliases: []string{"domain"},
		Usage:   "Manage domain bindings for a website",
		Description: `Manage domain bindings for a website. A website can have multiple domains
bound to it across ICANN and HNS namespaces.

ARGUMENT ORDER: <website> is the FIRST argument (a website's numeric ID or its
primary domain name), and the domain being acted on is the SECOND argument.

Examples:
  pinner websites domains list example.com
  pinner websites domains add example.com staging.example.com
  pinner websites domains add example.com mydomain --namespace hns
  pinner websites domains rm example.com staging.example.com
  pinner websites domains verify example.com staging.example.com`,

		// Note: the first argument selects the WEBSITE, the second is the DOMAIN.
		// Domain names accept a properly-terminated FQDN (e.g. "mydomain.") and
		// match the stored bare name case-insensitively.
		Commands: []*cli.Command{
			newWebsitesDomainsListCommand(),
			newWebsitesDomainsAddCommand(),
			newWebsitesDomainsRmCommand(),
			newWebsitesDomainsVerifyCommand(),
			newWebsitesDomainsDNSRequirementsCommand(),
			newWebsitesDomainsWizardCommand(),
		},
	}
}

func newWebsitesDomainsListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Aliases:   []string{"ls"},
		Usage:     "List all domains bound to a website",
		ArgsUsage: "<website-id-or-domain>",
		Description: `List all domains bound to a website.

Examples:
  pinner websites domains list example.com
  pinner websites domains list 123
  pinner websites domains list example.com --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsList(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "Bind a domain to a website",
		ArgsUsage: "[<website-id-or-domain>] <domain>",
		Flags: []cli.Flag{
			WebsiteFlag(),
			&cli.StringFlag{
				Name:    "namespace",
				Usage:   "Domain namespace: icann or hns",
				Value:   "icann",
				Sources: cli.EnvVars("PINNER_DOMAIN_NAMESPACE"),
			},
		},
		Description: `Binds a domain to a website under a specific namespace.

The namespace determines which DNS system manages the domain:
  icann - Traditional ICANN-managed domains (e.g. example.com)
  hns   - Handshake naming system domains

The domain should be the bare name without a TLD suffix (e.g. 'mydomain'
not 'mydomain.hns'). The namespace flag determines how it's registered.

The website is selected by the --website flag, or the first positional
argument if --website is omitted.

Examples:
  pinner websites domains add example.com staging.example.com
  pinner websites domains add --website example.com staging.example.com
  pinner websites domains add example.com mydomain --namespace hns
  pinner websites domains add 123 staging.example.com --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsAdd(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Aliases:   []string{"rm"},
		Usage:     "Remove a domain binding from a website",
		ArgsUsage: "[<website-id-or-domain>] <domain>",
		Flags: []cli.Flag{
			WebsiteFlag(),
		},
		Description: `Removes a domain binding from a website.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric ID. Domain names are resolved automatically.

The website is selected by the --website flag, or the first positional
argument if --website is omitted.

Examples:
  pinner websites domains rm example.com staging.example.com
  pinner websites domains rm --website example.com staging.example.com
  pinner websites domains rm 123 staging.example.com
  pinner websites domains rm example.com 42`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsRm(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:      "verify",
		Usage:     "Trigger domain verification",
		ArgsUsage: "[<website-id-or-domain>] <domain>",
		Flags: []cli.Flag{
			WebsiteFlag(),
		},
		Description: `Triggers verification of a domain binding.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric ID. Domain names are resolved automatically.

The website is selected by the --website flag, or the first positional
argument if --website is omitted.

Examples:
  pinner websites domains verify example.com staging.example.com
  pinner websites domains verify --website example.com staging.example.com
  pinner websites domains verify 123 staging.example.com --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsVerify(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsDNSRequirementsCommand() *cli.Command {
	return &cli.Command{
		Name:      "dns-requirements",
		Usage:     "Show the DNS records needed to complete domain delegation",
		ArgsUsage: "[<website-id-or-domain>] <domain>",
		Flags: []cli.Flag{
			WebsiteFlag(),
		},
		Description: `Shows the DNS records a user must publish to complete delegation for a domain.

For HNS namespaces this is the delegation bundle the backend generates: parent
records (NS/GLUE/DS) to publish in the HNS wallet and authoritative records
(NS/TLSA) to configure on the nameserver.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric ID. Domain names are resolved automatically.

The website is selected by the --website flag, or the first positional
argument if --website is omitted.

Examples:
  pinner websites domains dns-requirements example.com mydomain
  pinner websites domains dns-requirements --website example.com mydomain
  pinner websites domains dns-requirements 123 staging.example.com --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsDNSRequirements(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

// resolveDomainCommandTarget resolves the website and domain arguments for the
// `websites domains` add/remove/verify commands.
//
// The website can be supplied via the --website flag or the first positional
// argument. When --website is given, the only positional is the domain.
//
// commandName is the subcommand name (add/rm/verify) used in usage errors.
func resolveDomainCommandTarget(ctx context.Context, websitesService WebsitesService, cmd websitesCommandGetter, commandName string) (websiteID string, domainArg string, err error) {
	args := cmd.Args()
	flagWebsite := cmd.String(FlagWebsite)

	usage := fmt.Sprintf("usage: pinner websites domains %s [<website-id-or-domain>] <domain>", commandName)

	switch {
	case flagWebsite != "" && args.Len() > 1:
		return "", "", fmt.Errorf("website provided both as --website flag and positional argument; use one form")
	case flagWebsite != "":
		if args.Len() < 1 {
			return "", "", fmt.Errorf("domain argument is required (%s)", usage)
		}
		websiteID, err = resolveWebsiteID(ctx, websitesService, flagWebsite)
		if err != nil {
			return "", "", err
		}
		domainArg = args.First()
		return websiteID, domainArg, nil
	default:
		if args.Len() < 2 {
			return "", "", fmt.Errorf("domain argument is required (%s)", usage)
		}
		websiteID, err = resolveWebsiteID(ctx, websitesService, args.First())
		if err != nil {
			return "", "", err
		}
		domainArg = args.Get(1)
		return websiteID, domainArg, nil
	}
}

// resolveDomainID resolves a domain argument to its numeric ID.
// It lists the website's bound domains and matches by name first, then
// by numeric ID. This avoids ambiguity in namespaces like HNS where a
// domain name could legitimately be numeric (e.g. "123").
func resolveDomainID(ctx context.Context, websitesService WebsitesService, websiteID string, domainArg string) (string, error) {
	domains, err := websitesService.ListDomains(ctx, websiteID)
	if err != nil {
		return "", fmt.Errorf("failed to look up domain: %w", err)
	}

	// Match by name first (case-insensitive and tolerant of a trailing
	// dot — DNS names are case-insensitive and bare/FQDN forms are equal).
	for _, d := range domains {
		if dnsname.Equal(d.Domain, domainArg) {
			return strconv.Itoa(d.Id), nil
		}
	}

	// Then match by numeric ID
	for _, d := range domains {
		if strconv.Itoa(d.Id) == domainArg {
			return domainArg, nil
		}
	}

	return "", fmt.Errorf("domain %q not found for website %s", domainArg, websiteID)
}

func websitesDomainsList(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	websiteID, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	domains, err := websitesService.ListDomains(ctx, websiteID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		if domains == nil {
			domains = []ipfs.DomainResponse{}
		}
		return output.PrintJSON(map[string]any{
			"count":   len(domains),
			"domains": domains,
		})
	}

	if len(domains) == 0 {
		output.Printfln("No domains found for website %s", websiteID)
		return nil
	}

	output.Printfln("Found %d domain(s) for website %s", len(domains), websiteID)

	headers := []string{"ID", "DOMAIN", "NAMESPACE", "STATUS", "ZONE NAME"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		zoneName := ""
		if d.ZoneName != nil {
			zoneName = *d.ZoneName
		}
		status := ""
		if d.Status != nil {
			status = *d.Status
		}
		rows[i] = []string{
			strconv.Itoa(d.Id),
			d.Domain,
			d.Namespace,
			status,
			zoneName,
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

func websitesDomainsAdd(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	websiteID, domain, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "add")
	if err != nil {
		return err
	}

	namespace := cmd.String("namespace")
	if namespace != "icann" && namespace != "hns" {
		return fmt.Errorf("invalid namespace %q: must be 'icann' or 'hns'", namespace)
	}

	req := ipfs.DomainRequest{
		Domain:    domain,
		Namespace: namespace,
	}

	result, err := websitesService.BindDomain(ctx, websiteID, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain bound successfully")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

func websitesDomainsRm(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "rm")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	if err := websitesService.UnbindDomain(ctx, websiteID, domainID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"deleted":   true,
			"domain_id": domainID,
		})
	}

	output.Printfln("Domain %s removed from website %s", domainArg, websiteID)
	return nil
}

func websitesDomainsVerify(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "verify")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.VerifyDomain(ctx, websiteID, domainID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain verification triggered")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

func websitesDomainsDNSRequirements(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()

	websitesService, err := newAuthenticatedWebsitesService(cfgMgr, output, authToken, secure)
	if err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "dns-requirements")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.GetDomainDNSRequirements(ctx, websiteID, domainID)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("no DNS requirements returned for domain %s", domainID)
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	renderDomainDelegation(output, result)
	return nil
}

// renderDomainDelegation prints the DNS delegation bundle the server computes
// for a domain. Rendering is driver-based: the namespace selects a
// context-specific driver (HNS, ICANN, ...) with a neutral generic fallback,
// mirroring the server's per-namespace DomainProvider design.
func renderDomainDelegation(output Output, result *ipfs.DomainResponse) {
	output.Printfln("DNS requirements for %s", result.Domain)

	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
		},
	})

	if result.Delegation == nil {
		output.Printfln("No delegation records are available for %s.", result.Domain)
		return
	}

	defaultDelegationDriver.Render(output, result)
}
