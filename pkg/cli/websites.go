package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// stripValidationPrefix strips the "key=" prefix from a validation token value.
// The API returns the full DNS TXT record value (e.g. "lumeweb-verify=abc123"),
// but for English display we only want the token portion.
func stripValidationPrefix(token string) string {
	if idx := strings.Index(token, "="); idx >= 0 {
		return token[idx+1:]
	}
	return token
}

func newWebsitesCommand() *cli.Command {
	// The websites parent is catalog-driven: the core website CRUD + status
	// subcommands (list, create, get, update, enable-ipns, delete, validate,
	// ssl status, config) are compiled from the canonical operation catalog
	// (internal/catalogops) — see catalog_websites_wiring.go. The commands
	// that are fundamentally interactive/IO or domain-delegation-focused are
	// NOT representable as pure data-returning handlers and remain hand-written:
	//   - websites wizard        — interactive stepwise creation session.
	//   - websites domains ...   — domain binding + DANE delegation tree
	//                              (list/add/rm/verify/dns-requirements/dane
	//                              republish/update + wizard), driven by the
	//                              core websites domain-binding service but
	//                              with wizard/IO coupling that is out of scope
	//                              for this catalog migration pass.
	cmds := newWebsitesCatalogCommands()
	cmds = append(cmds,
		newWebsitesWizardCommand(),
		newWebsitesDomainsCommand(),
	)

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

func newWebsitesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all websites",
		Description: `List all websites for the authenticated user. Returns each website's ID, domain, target CID, resolved CID, status, DNS-hosting flag and gateway. Use this to obtain the numeric website ID accepted interchangeably with a domain by websites get/update/validate/delete and websites domains list/add.

Examples:
  pinner websites list
  pinner websites list --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesList(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new website",
		Description: `Create a website that serves an IPFS CID under a custom domain. Takes the <domain> positional and --cid (required), plus optional --target-type (ipfs|ipns) and --dns-hosting. Returns the created website object including its numeric ID, the validation TXT token, and the DNS/CNAME records you must publish to make it live.

This registers the site itself. To point a website at an IPNS key instead of a fixed CID, use 'websites enable-ipns'; to add an extra domain binding to an existing site use 'websites domains add'. To manage raw DNS records for a zone use 'dns records create' rather than this command.

Examples:
  pinner websites create example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner websites create example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --target-type ipfs
  pinner websites create example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --dns-hosting
  pinner websites create example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			RequiredCIDFlag(),
			TargetTypeFlag(),
			DNSHostingFlag(),
			NoDNSHostingFlag(),
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesCreate(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get website details",
		Description: `Get full details of one website, selected by domain name or numeric ID (either works). Returns ID, domain, CID, resolved CID, target type, status, DNS-hosting flag, validation token, gateway and associated IPNS key / DNS zone IDs, plus the required DNS records.

This reports configuration state. For whether DNS is correctly configured use 'websites validate'; for TLS certificate state use 'websites ssl status'. It does NOT create or modify anything.

Examples:
  pinner websites get example.com
  pinner websites get example.com --json`,
		ArgsUsage: "<domain>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesGet(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a website",
		Description: `Update an existing website: change its CID, target type (ipfs|ipns), rename its domain (--rename-to), or toggle DNS hosting. Selects the site by the <domain> positional, then applies whichever optional flags are set (at least one is required). Returns the updated website object.

Passing --target-type ipns converts the site to IPNS addressing (auto-creates an IPNS key). For that conversion alone, prefer the dedicated single-purpose 'websites enable-ipns'. This does NOT touch DNS zone records; use 'dns records update' for those.

At least one of the optional fields must be provided to update the website.

When --target-type is set to "ipns" without --cid, the website will be converted
from IPFS to IPNS targeting (an IPNS key is auto-created and the current CID is
published to it).

When --target-type is "ipns" and --cid is a regular IPFS CID (not a peer ID),
an IPNS key is auto-created and that CID is published to it.

The positional <domain> selects the website to update. Use --rename-to to give
it a new domain.

Examples:
  pinner websites update example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --target-type ipfs
  pinner websites update example.com --target-type ipns
  pinner websites update example.com --rename-to newdomain.com
  pinner websites update example.com --dns-hosting
  pinner websites update example.com --no-dns-hosting
  pinner websites update example.com --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			RenameDomainFlag(),
			CIDFlag(),
			TargetTypeFlag(),
			DNSHostingFlag(),
			NoDNSHostingFlag(),
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesUpdate(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}


func resolveRequiredArg(ctx context.Context, websitesService WebsitesService, cmd websitesCommandGetter) (string, error) {
	args := cmd.Args()
	if args.Len() == 0 {
		return "", fmt.Errorf("website ID or domain is required")
	}

	return resolveWebsiteID(ctx, websitesService, args.First())
}

func printWebsiteUpdateResult(output Output, website *ipfs.WebsiteItem, message string) {
	output.Printf("%s\n", message)

	fields := []Field{
		{"ID", fmt.Sprintf("%d", website.Id)},
		{"Domain", website.Domain},
		{"CID", website.TargetHash},
		{"Target Type", website.TargetType},
		{"Status", website.Status},
		{"DNS Hosting", fmt.Sprintf("%t", website.DnsHostingEnabled)},
		{"Subdomain", fmt.Sprintf("%t", website.IsSubdomain)},
		{"Created", website.Created.Format("2006-01-02 15:04:05")},
	}

	if website.Status != "active" {
		fields = append(fields, Field{"Token Expired", fmt.Sprintf("%t", website.Expired)})
	}

	if website.IpnsKeyId != nil {
		fields = append(fields, Field{"IPNS Key ID", fmt.Sprintf("%d", *website.IpnsKeyId)})
	}

	output.PrintFields(FieldGroup{Fields: fields})

	if website.GatewayDomain != nil {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway", *website.GatewayDomain},
			},
		})
	}
}

func websitesList(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	websites, err := websitesService.List(ctx)
	if err != nil {
		return err
	}

	if len(websites) == 0 {
		output.Printfln("No websites found")
		return nil
	}

	if output.IsJSON() {
		result := map[string]any{
			"count":    len(websites),
			"websites": websites,
		}
		return output.PrintJSON(result)
	}

	output.Printfln("Found %d website(s)", len(websites))

	headers := []string{"ID", "NAME", "CID", "RESOLVED CID", "STATUS", "DNS", "SUBDOMAIN", "GATEWAY", "VALIDATION", "CREATED"}
	rows := make([][]string, len(websites))
	for i, website := range websites {
		validation := ""
		if website.Status == "active" {
			validation = "validated"
		} else if website.Expired {
			validation = "expired"
		} else if website.ValidationToken != "" {
			validation = stripValidationPrefix(website.ValidationToken)
		}
		gateway := ""
		if website.GatewayDomain != nil {
			gateway = *website.GatewayDomain
		}
		resolvedCID := "-"
		if website.ActiveCid != nil {
			resolvedCID = *website.ActiveCid
		}
		rows[i] = []string{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			resolvedCID,
			website.Status,
			fmt.Sprintf("%t", website.DnsHostingEnabled),
			fmt.Sprintf("%t", website.IsSubdomain),
			gateway,
			validation,
			website.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

// resolveWebsiteID resolves an ID or domain argument to a numeric website ID string.
// If arg is numeric, it's returned as-is. Otherwise, it searches by domain via List.
func resolveWebsiteID(ctx context.Context, websitesService WebsitesService, arg string) (string, error) {
	if _, err := strconv.Atoi(arg); err == nil {
		return arg, nil
	}

	websites, err := websitesService.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up website by domain: %w", err)
	}

	for _, w := range websites {
		if dnsname.Equal(w.Domain, arg) {
			return fmt.Sprintf("%d", w.Id), nil
		}
	}

	return "", fmt.Errorf("website not found for domain %q", arg)
}

// resolveAndGetWebsite resolves a website by ID or domain name.
// If arg is a numeric ID, it fetches directly. Otherwise, it searches by domain.
func resolveAndGetWebsite(ctx context.Context, websitesService WebsitesService, arg string) (*ipfs.WebsiteItem, error) {
	id, err := resolveWebsiteID(ctx, websitesService, arg)
	if err != nil {
		return nil, err
	}
	return websitesService.Get(ctx, id)
}

func websitesUpdate(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	domain := cmd.String(FlagRenameTo)
	cid := cmd.String(FlagCID)
	targetType := cmd.String(FlagTargetType)
	if err := requireUpdateFields(cmd, FlagRenameTo, FlagCID, FlagTargetType, FlagDNSHosting, FlagNoDNSHosting); err != nil {
		return err
	}

	req := ipfs.WebsiteUpdateRequest{}

	if cmd.IsSet(FlagRenameTo) {
		req.Domain = &domain
	}
	if cmd.IsSet(FlagCID) {
		req.TargetHash = &cid
	}
	if cmd.IsSet(FlagTargetType) {
		req.TargetType = &targetType
	}

	if req.TargetHash != nil && req.TargetType == nil {
		return fmt.Errorf("--target-type is required when --cid is provided")
	}

	if cmd.IsSet(FlagDNSHosting) {
		v := cmd.Bool(FlagDNSHosting)
		req.DnsHostingEnabled = &v
	} else if cmd.IsSet(FlagNoDNSHosting) {
		v := false
		req.DnsHostingEnabled = &v
	}

	updatedWebsite, err := websitesService.UpdateWithOptions(ctx, id, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	printWebsiteUpdateResult(output, updatedWebsite, "Website updated successfully")

	nameservers := getNameservers(ctx, websitesService)
	output.Printfln("")
	showDNSRecordInstructions(output, updatedWebsite, nameservers)

	if updatedWebsite.Expired {
		output.Printfln("")
		output.Printfln("⚠ This website's validation has expired.")
		output.Printfln("  Re-validate: pinner websites validate %d", updatedWebsite.Id)
	}

	return nil
}

func newWebsitesEnableIPNSCommand() *cli.Command {
	return &cli.Command{
		Name:    "enable-ipns",
		Aliases: []string{"ipns"},
		Usage:   "Enable IPNS targeting for a website",
		Description: `Convert a website from IPFS to IPNS targeting (alias 'ipns'). Auto-creates an IPNS key for the site and publishes the current CID to it, or, with --cid, publishes that CID instead. Returns the updated website including its new IPNS key ID. After this, the domain resolves via the mutable IPNS name.

Equivalent to 'websites update <domain> --target-type ipns'. To publish a new CID to an existing IPNS key (not tied to a website) or refresh a record, use 'ipns publish' / 'ipns republish' under the 'ipns' tree.

Examples:
  pinner websites enable-ipns example.com
  pinner websites enable-ipns example.com --cid bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner websites enable-ipns example.com --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			CIDFlag(),
		},
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesEnableIPNS(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func websitesEnableIPNS(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	ipnsType := "ipns"
	req := ipfs.WebsiteUpdateRequest{
		TargetType: &ipnsType,
	}

	if cmd.IsSet(FlagCID) {
		cid := cmd.String(FlagCID)
		req.TargetHash = &cid
	}

	updatedWebsite, err := websitesService.UpdateWithOptions(ctx, id, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	printWebsiteUpdateResult(output, updatedWebsite, "IPNS enabled for website")

	return nil
}

func websitesGet(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	website, err := websitesService.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ipfs.ErrGone) || website == nil {
			return err
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printfln("Website Details")

	fields := []Field{
		{"ID", fmt.Sprintf("%d", website.Id)},
		{"Domain", website.Domain},
		{"CID", website.TargetHash},
		{"Target Type", website.TargetType},
		{"Status", website.Status},
		{"DNS Hosting", fmt.Sprintf("%t", website.DnsHostingEnabled)},
		{"Subdomain", fmt.Sprintf("%t", website.IsSubdomain)},
	}

	if website.ActiveCid != nil {
		fields = append(fields, Field{"Resolved CID", *website.ActiveCid})
	}

	if website.Status != "active" {
		fields = append(fields,
			Field{"Token Expired", fmt.Sprintf("%t", website.Expired)},
			Field{"Validation Token", stripValidationPrefix(website.ValidationToken)},
		)
		if website.ValidationExpiresAt != nil {
			fields = append(fields, Field{"Token Expires", website.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
		}
	}

	if website.GatewayDomain != nil {
		fields = append(fields, Field{"Gateway", *website.GatewayDomain})
	}

	if website.IpnsKeyId != nil {
		fields = append(fields, Field{"IPNS Key ID", fmt.Sprintf("%d", *website.IpnsKeyId)})
	}

	if website.DnsZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *website.DnsZoneId)})
	}

	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		fields = append(fields, Field{"Validation Host", *website.ValidationRecordHost})
	}

	fields = append(fields, Field{"Created", website.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

	nameservers := getNameservers(ctx, websitesService)
	output.Printfln("")
	showDNSRecordInstructions(output, website, nameservers)

	if website.Expired && website.Status != "active" {
		output.Printfln("")
		output.Printfln("⚠ Validation token has expired. Re-validate to generate a new token:")
		output.Printfln("  pinner websites validate %d", website.Id)
	}

	return nil
}

func websitesCreate(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain is required")
	}

	domain := args.First()
	cid := cmd.String(FlagCID)

	targetType := cmd.String(FlagTargetType)
	if targetType == "" {
		targetType = "ipfs"
	}

	req := ipfs.WebsiteRequest{
		Domain:     domain,
		TargetHash: cid,
		TargetType: targetType,
	}

	if cmd.IsSet(FlagDNSHosting) {
		dnsHosting := cmd.Bool(FlagDNSHosting)
		req.DnsHostingEnabled = &dnsHosting
	} else if cmd.IsSet(FlagNoDNSHosting) {
		dnsHosting := false
		req.DnsHostingEnabled = &dnsHosting
	}

	createdWebsite, err := websitesService.CreateWithOptions(ctx, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(createdWebsite)
	}

	output.Printfln("Website created successfully")

	fields := []Field{
		{"ID", fmt.Sprintf("%d", createdWebsite.Id)},
		{"Domain", createdWebsite.Domain},
		{"CID", createdWebsite.TargetHash},
		{"Target Type", createdWebsite.TargetType},
		{"Status", createdWebsite.Status},
		{"DNS Hosting", fmt.Sprintf("%t", createdWebsite.DnsHostingEnabled)},
		{"Subdomain", fmt.Sprintf("%t", createdWebsite.IsSubdomain)},
		{"Expired", fmt.Sprintf("%t", createdWebsite.Expired)},
	}

	if createdWebsite.IpnsKeyId != nil {
		fields = append(fields, Field{"IPNS Key ID", fmt.Sprintf("%d", *createdWebsite.IpnsKeyId)})
	}

	output.PrintFields(FieldGroup{Fields: fields})

	if createdWebsite.GatewayDomain != nil {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway", *createdWebsite.GatewayDomain},
			},
		})
	}

	output.Printfln("")
	output.Printfln("Validation token: %s", stripValidationPrefix(createdWebsite.ValidationToken))
	output.Printfln("")

	nameservers := getNameservers(ctx, websitesService)
	showDNSRecordInstructions(output, createdWebsite, nameservers)

	output.Printfln("")
	output.Printfln("Validate: pinner websites validate %s", createdWebsite.Domain)

	if !createdWebsite.DnsHostingEnabled {
		output.Printfln("")
		output.Printfln("  Tip: Use --dns-hosting to have Pinner manage DNS for you")
	}

	return nil
}

// getNameservers fetches the nameservers from the website hosting config.
func getNameservers(ctx context.Context, websitesService WebsitesService) []string {
	cfg, err := websitesService.GetConfig(ctx)
	if err != nil || cfg == nil || cfg.Nameservers == nil {
		return nil
	}
	return *cfg.Nameservers
}

// showDNSRecordInstructions displays the DNS records a user needs to add for their website.
func showDNSRecordInstructions(output Output, website *ipfs.WebsiteItem, nameservers []string) {
	if website == nil {
		return
	}

	if website.DnsHostingEnabled {
		showDNSHostingInstructions(output, website, nameservers)
		output.Printfln("Then validate: pinner dns zones validate %s", website.Domain)
		return
	}

	showSelfManagedDNSInstructions(output, website)
}

// showDNSHostingInstructions displays NS delegation instructions when DNS hosting is enabled.
func showDNSHostingInstructions(output Output, website *ipfs.WebsiteItem, nameservers []string) {
	output.Printfln("DNS hosting is enabled: Pinner manages your DNS records.")
	output.Printfln("Update your domain's nameservers at your registrar:")

	if len(nameservers) > 0 {
		rows := make([][]string, len(nameservers))
		for i, ns := range nameservers {
			rows[i] = []string{website.Domain, "NS", ns}
		}
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, rows)
	} else {
		output.Printfln("  Use: pinner websites config")
		output.Printfln("  To find the required nameservers.")
	}

	output.Printfln("")
}

// showSelfManagedDNSInstructions displays required DNS records for self-managed DNS.
func showSelfManagedDNSInstructions(output Output, website *ipfs.WebsiteItem) {
	output.Printfln("Required DNS records:")

	validationHost := website.Domain
	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		validationHost = *website.ValidationRecordHost
	}

	records := [][]string{
		{validationHost, "TXT", website.ValidationToken},
		{"_dnslink." + website.Domain, "TXT", "dnslink=/" + website.TargetType + "/" + website.TargetHash},
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, []string{website.Domain, "CNAME", *website.GatewayDomain})
	}

	output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, records)
}

// showConfigDNSRecords displays DNS record tables from the website hosting config.
func showConfigDNSRecords(output Output, config *ipfs.WebsiteConfigResponse) {
	if config.GatewayDomain != nil && *config.GatewayDomain != "" {
		output.Printfln("")
		output.Printfln("CNAME record to point your domain to the gateway:")
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
			{"<your-domain>", "CNAME", *config.GatewayDomain},
		})
	}

	if config.Nameservers != nil && len(*config.Nameservers) > 0 {
		output.Printfln("")
		output.Printfln("NS records for DNS hosting (delegate your domain's nameservers):")
		rows := make([][]string, len(*config.Nameservers))
		for i, ns := range *config.Nameservers {
			rows[i] = []string{"<your-domain>", "NS", ns}
		}
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, rows)
	}
}

func newWebsitesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a website",
		Description: `Delete a website, selected by domain name or numeric ID. DESTRUCTIVE and irreversible: there is no undo. Returns a success confirmation. Does NOT delete the website's DNS zone or its IPNS keys; use 'dns zones delete' and 'ipns keys delete' for those.

Examples:
  pinner websites delete example.com
  pinner websites delete example.com --json`,
		ArgsUsage: "<domain>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesDelete(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func newWebsitesValidateCommand() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "Validate a website",
		Description: `Validate that a website's DNS records are correctly configured (TXT validation token + _dnslink). Selects the site by domain name or numeric ID. Returns a valid/message/reason result and, when invalid, lists the required TXT/CNAME records.

This checks website-specific records. To validate that a DNS zone's nameservers are delegated to Pinner's nameservers, use 'dns zones validate' instead.

Examples:
  pinner websites validate example.com
  pinner websites validate example.com --json`,
		ArgsUsage: "<domain>",
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesValidate(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func websitesDelete(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	if err := websitesService.Delete(ctx, id); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("Website %s deleted successfully", id),
		}
		return output.PrintJSON(result)
	}

	output.Printfln("Website deleted successfully")

	return nil
}

func websitesValidate(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	return doWebsitesValidate(ctx, cmd, output, websitesService)
}

func doWebsitesValidate(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	arg := args.First()

	id, err := resolveWebsiteID(ctx, websitesService, arg)
	if err != nil {
		return err
	}

	validationResult, validateErr := websitesService.Validate(ctx, id)

	website, _ := resolveAndGetWebsite(ctx, websitesService, arg)

	if output.IsJSON() {
		result := map[string]any{
			"domain": arg,
			"id":     id,
		}

		if validateErr != nil {
			result["valid"] = false
			result["error"] = validateErr.Error()
		} else {
			result["valid"] = validationResult.Valid
			result["message"] = validationResult.Message
			result["reason"] = validationResult.Reason
		}

		if website != nil {
			nameservers := getNameservers(ctx, websitesService)
			result["required_records"] = buildRequiredRecords(website, nameservers)
		}

		return output.PrintJSON(result)
	}

	if validateErr != nil {
		output.Printfln("Validation failed: %s", validateErr.Error())
		return nil
	}

	switch ipfs.WebsiteValidationReasonOf(validationResult) {
	case ipfs.WebsiteValidationReasonTokenExpired:
		output.Printfln("Validation token has expired; a new token has been generated.")
		showValidationInstructions(ctx, output, website, websitesService, arg)
		return nil
	case ipfs.WebsiteValidationReasonDNSMissing, ipfs.WebsiteValidationReasonTokenMissing, ipfs.WebsiteValidationReasonDNSMismatch:
		printValidationResult(output, validationResult)
		showValidationInstructions(ctx, output, website, websitesService, arg)
		return nil
	}

	printValidationResult(output, validationResult)

	if !validationResult.Valid {
		showValidationInstructions(ctx, output, website, websitesService, arg)
	}

	return nil
}

func printValidationResult(output Output, result *ipfs.WebsiteValidateResponse) {
	statusIcon := "⏳"
	if result.Valid {
		statusIcon = "✅"
	}

	output.Printfln("Website Validation Result")
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Domain", result.Domain},
			{"ID", fmt.Sprintf("%d", result.Id)},
			{"Valid", fmt.Sprintf("%s %t", statusIcon, result.Valid)},
			{"Message", result.Message},
		},
	})
}

func showValidationInstructions(ctx context.Context, output Output, website *ipfs.WebsiteItem, websitesService WebsitesService, arg string) {
	output.Printfln("")
	if website != nil {
		nameservers := getNameservers(ctx, websitesService)
		showDNSRecordInstructions(output, website, nameservers)
	} else {
		output.Printfln("Make sure you have added the required DNS records to your domain")
		output.Printfln("View website details: pinner websites get %s", arg)
	}
	output.Printfln("")
	output.Printfln("Re-validate: pinner websites validate %s", arg)
}

// buildRequiredRecords returns the DNS records a user needs to add for their website.
func buildRequiredRecords(website *ipfs.WebsiteItem, nameservers []string) []map[string]string {
	if website == nil {
		return nil
	}

	if website.DnsHostingEnabled {
		records := []map[string]string{}
		for _, ns := range nameservers {
			records = append(records, map[string]string{
				"name": website.Domain, "type": "NS", "value": ns,
			})
		}
		return records
	}

	validationHost := website.Domain
	if website.ValidationRecordHost != nil && *website.ValidationRecordHost != "" {
		validationHost = *website.ValidationRecordHost
	}

	records := []map[string]string{
		{"name": validationHost, "type": "TXT", "value": website.ValidationToken},
		{"name": "_dnslink." + website.Domain, "type": "TXT", "value": "dnslink=/" + website.TargetType + "/" + website.TargetHash},
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, map[string]string{
			"name": website.Domain, "type": "CNAME", "value": *website.GatewayDomain,
		})
	}

	return records
}

func newWebsitesConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Show website hosting configuration",
		Description: `Show the account-wide website hosting configuration: the Pinner gateway domain and the nameservers used for DNS hosting. Returns these values plus the suggested CNAME/NS records to configure.

This is account-level, not per-website. To see one site's records use 'websites get'; to list or edit actual DNS records use 'dns records'.

Examples:
  pinner websites config
  pinner websites config --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return websitesConfig(ctx, cc.Cmd, cc.Output, cc.CfgMgr, cc.AuthToken, cc.Secure)
		}),
	}
}

func websitesConfig(ctx context.Context, cmd websitesCommandGetter, output Output, cfgMgr config.Manager, authToken string, secure bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetDefaultTimeout())
	defer cancel()
	websitesService, err := newWebsitesAPI(cfgMgr, authToken, secure)
	if err != nil {
		return err
	}

	config, err := websitesService.GetConfig(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(config)
	}

	output.Printfln("Website Hosting Configuration")

	fields := []Field{}
	if config.GatewayDomain != nil && *config.GatewayDomain != "" {
		fields = append(fields, Field{"Gateway Domain", *config.GatewayDomain})
	}
	if config.Nameservers != nil && len(*config.Nameservers) > 0 {
		fields = append(fields, Field{"Nameservers", strings.Join(*config.Nameservers, ", ")})
	}
	if len(fields) > 0 {
		output.PrintFields(FieldGroup{Fields: fields})
	}

	showConfigDNSRecords(output, config)

	if len(fields) == 0 {
		output.Printfln("  No gateway domain or nameservers configured")
	}

	return nil
}
