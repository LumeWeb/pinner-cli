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

Commands that act on an existing binding (remove, verify, dns-requirements)
take only the domain, since a domain belongs to exactly one website. 'add'
takes a domain and optionally the website to bind it to.

Examples:
  pinner websites domains list example.com
  pinner websites domains add staging.example.com
  pinner websites domains rm staging.example.com
  pinner websites domains verify staging.example.com
  pinner websites domains dns-requirements mydomain`,

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

The website is selected by the --website flag, the first positional
argument, or automatically when there is exactly one website.

Examples:
  pinner websites domains add staging.example.com
  pinner websites domains add example.com staging.example.com
  pinner websites domains add mydomain --namespace hns
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
		Usage:     "Remove a domain binding from its website",
		ArgsUsage: "<domain>",
		Description: `Removes a domain binding. The domain belongs to exactly one website,
which is resolved automatically.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric binding ID.

Examples:
  pinner websites domains rm staging.example.com
  pinner websites domains rm 42`,

		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsRm(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:      "verify",
		Usage:     "Trigger domain verification",
		ArgsUsage: "<domain>",
		Description: `Triggers verification of a domain binding. The domain belongs to
exactly one website, which is resolved automatically.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric binding ID.

Examples:
  pinner websites domains verify staging.example.com
  pinner websites domains verify 42 --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsVerify(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesDomainsDNSRequirementsCommand() *cli.Command {
	return &cli.Command{
		Name:      "dns-requirements",
		Usage:     "Show the DNS records needed to complete domain delegation",
		ArgsUsage: "<domain>",
		Description: `Shows the DNS records a user must publish to complete delegation for a domain.
The domain belongs to exactly one website, which is resolved automatically.

For HNS namespaces this is the delegation bundle the backend generates: parent
records (NS/GLUE/DS) to publish in the HNS wallet and authoritative records
(NS/TLSA) to configure on the nameserver.

The domain argument can be either the domain name (e.g. staging.example.com)
or its numeric binding ID.

Examples:
  pinner websites domains dns-requirements mydomain
  pinner websites domains dns-requirements 42 --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDomainsDNSRequirements(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

// resolveAddTarget resolves the website and domain arguments for the
// `websites domains add` command.
//
// The website may be supplied via the --website flag or the first positional
// argument. When neither is given and the user supplies only a single <domain>,
// the website is auto-selected — but only when there is exactly one website;
// with multiple websites an error asks the caller to name the target.
func resolveAddTarget(ctx context.Context, websitesService WebsitesService, cmd websitesCommandGetter) (websiteID string, domain string, err error) {
	args := cmd.Args()
	flagWebsite := cmd.String(FlagWebsite)

	usage := "usage: pinner websites domains add [<website-id-or-domain>] <domain>"

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
		return websiteID, args.First(), nil
	case args.Len() == 0:
		return "", "", fmt.Errorf("domain argument is required (%s)", usage)
	case args.Len() == 1:
		// Single <domain> and no website: auto-select when there's exactly one.
		websites, err := websitesService.List(ctx)
		if err != nil {
			return "", "", fmt.Errorf("failed to list websites: %w", err)
		}
		switch len(websites) {
		case 0:
			return "", "", fmt.Errorf("no websites found; create a website first")
		case 1:
			return fmt.Sprintf("%d", websites[0].Id), args.First(), nil
		default:
			return "", "", fmt.Errorf("multiple websites found (%d); specify which website to add the domain to (%s)", len(websites), usage)
		}
	default:
		websiteID, err = resolveWebsiteID(ctx, websitesService, args.First())
		if err != nil {
			return "", "", err
		}
		return websiteID, args.Get(1), nil
	}
}

// resolveDomainArg returns the single <domain> argument for the
// existing-binding commands (rm/verify/dns-requirements), erroring when it
// is missing.
func resolveDomainArg(cmd websitesCommandGetter, commandName string) (string, error) {
	args := cmd.Args()
	if args.Len() == 0 {
		return "", fmt.Errorf("domain argument is required (usage: pinner websites domains %s <domain>)", commandName)
	}
	if args.Len() > 1 {
		return "", fmt.Errorf("unexpected extra argument %q (usage: pinner websites domains %s <domain>)", args.Get(1), commandName)
	}
	return args.First(), nil
}

// resolveDomainBinding resolves a single <domain> argument to its owning
// website and domain ID for the rm/verify/dns-requirements commands.
//
// A domain binding is unique across websites, so naming the website is
// redundant. This scans the user's website domain bindings to find the
// owning website, matching by domain name first and numeric domain ID second,
// and returns that website's ID together with the binding's domain ID.
func resolveDomainBinding(ctx context.Context, websitesService WebsitesService, domainArg string) (websiteID string, domainID string, err error) {
	websites, err := websitesService.List(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to list websites: %w", err)
	}

	// Match by domain name first across all websites, then fall back to a
	// numeric ID match. Two passes (rather than a single OR) keep name
	// matching priority: it protects namespaces like HNS where a domain name
	// can itself be numeric (e.g. "123"), so an unrelated binding with ID
	// "123" can't shadow a real domain named "123" depending on iteration
	// order.
	var idMatchWebsite, idMatchDomain string
	var deferredErr error

	for _, w := range websites {
		wID := fmt.Sprintf("%d", w.Id)
		domains, lerr := websitesService.ListDomains(ctx, wID)
		if lerr != nil {
			// Defer the listing error rather than aborting the scan: a
			// transient failure on an unrelated website must not make an
			// unambiguously name-matched domain unresolvable. It only blocks
			// the numeric-ID fallback, which is otherwise ambiguous.
			if deferredErr == nil {
				deferredErr = fmt.Errorf("failed to look up domain on website %s: %w", wID, lerr)
			}
			continue
		}
		for _, d := range domains {
			if dnsname.Equal(d.Domain, domainArg) {
				return wID, fmt.Sprintf("%d", d.Id), nil
			}
			if idMatchDomain == "" && fmt.Sprintf("%d", d.Id) == domainArg {
				idMatchWebsite, idMatchDomain = wID, fmt.Sprintf("%d", d.Id)
			}
		}
	}

	// Only fall back to a numeric ID match on a clean scan; a deferred
	// listing error could have hidden a conflicting binding.
	if idMatchDomain != "" && deferredErr == nil {
		return idMatchWebsite, idMatchDomain, nil
	}
	if deferredErr != nil {
		return "", "", deferredErr
	}

	return "", "", fmt.Errorf("domain %q not found bound to any website", domainArg)
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

	websiteID, domain, err := resolveAddTarget(ctx, websitesService, cmd)
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

	domainArg, err := resolveDomainArg(cmd, "rm")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
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

	domainArg, err := resolveDomainArg(cmd, "verify")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
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

	domainArg, err := resolveDomainArg(cmd, "dns-requirements")
	if err != nil {
		return err
	}

	websiteID, domainID, err := resolveDomainBinding(ctx, websitesService, domainArg)
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

	// DNS hosting enabled on the website means the platform is authoritative
	// for the zone (managed) and serves the authoritative records itself, so
	// the user only publishes the parent records. Read it from the website
	// API. If the fetch fails, fall back to unmanaged (current behavior)
	// rather than erroring the command.
	managed := false
	if website, gErr := websitesService.Get(ctx, websiteID); gErr == nil && website != nil {
		managed = website.DnsHostingEnabled
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	renderDomainDelegation(output, result, managed)
	return nil
}

// renderDomainDelegation prints the DNS delegation bundle the server computes
// for a domain. Rendering is driver-based: the namespace selects a
// context-specific driver (HNS, ICANN, ...) with a neutral generic fallback,
// mirroring the server's per-namespace DomainProvider design. managed (DNS
// hosting enabled on the website) flags whether the platform serves the
// authoritative records, so drivers don't tell managed users to configure
// their own authoritative DNS server.
func renderDomainDelegation(output Output, result *ipfs.DomainResponse, managed bool) {
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

	defaultDelegationDriver.Render(output, result, managed)
}
