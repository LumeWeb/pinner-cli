package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func newWebsitesCommand() *cli.Command {
	return &cli.Command{
		Name:    "websites",
		Aliases: []string{"website"},
		Usage:   "Manage websites",
		Description: `Manage websites for your IPFS content. Websites allow you to associate
domain names with IPFS hashes, making your content accessible through custom domains.

Website operations include:
  - List all websites
  - Create a new website
  - Get website details
  - Update website configuration
  - Delete a website
  - Validate website configuration

Examples:
  pinner websites list
  pinner websites create example.com --cid QmHash
  pinner websites get example.com
  pinner websites update example.com --cid QmNewHash
  pinner websites delete example.com
  pinner websites validate example.com`,
		Commands: []*cli.Command{
			newWebsitesListCommand(),
			newWebsitesCreateCommand(),
			newWebsitesGetCommand(),
			newWebsitesUpdateCommand(),
			newWebsitesEnableIPNSCommand(),
			newWebsitesDeleteCommand(),
			newWebsitesValidateCommand(),
			newWebsitesSSLCommand(),
			newWebsitesConfigCommand(),
			newWebsitesWizardCommand(),
		},
	}
}

func newWebsitesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all websites",
		Description: `List all websites for the authenticated user.

Examples:
  pinner websites list
  pinner websites list --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesList(ctx, cmd, output)
		},
	}
}

func newWebsitesCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new website",
		Description: `Create a new website with the specified domain and target CID.

Examples:
  pinner websites create example.com --cid QmHash
  pinner websites create example.com --cid QmHash --target-type ipfs
  pinner websites create example.com --cid QmHash --dns-hosting
  pinner websites create example.com --cid QmHash --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			RequiredCIDFlag(),
			TargetTypeFlag(),
			DNSHostingFlag(),
			NoDNSHostingFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesCreate(ctx, cmd, output)
		},
	}
}

func newWebsitesGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Get website details",
		Description: `Get details of a specific website by domain.

Examples:
  pinner websites get example.com
  pinner websites get example.com --json`,
		ArgsUsage: "<domain>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesGet(ctx, cmd, output)
		},
	}
}

func newWebsitesUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a website",
		Description: `Update an existing website with new CID, target type, or domain rename.

At least one of the optional fields must be provided to update the website.

When --target-type is set to "ipns" without --cid, the website will be converted
from IPFS to IPNS targeting (an IPNS key is auto-created and the current CID is
published to it).

When --target-type is "ipns" and --cid is a regular IPFS CID (not a peer ID),
an IPNS key is auto-created and that CID is published to it.

Examples:
  pinner websites update example.com --cid QmNewHash --target-type ipfs
  pinner websites update example.com --target-type ipns
  pinner websites update example.com --cid QmNewHash --target-type ipns
  pinner websites update example.com --dns-hosting
  pinner websites update example.com --no-dns-hosting
  pinner websites update example.com --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			DomainFlag(),
			CIDFlag(),
			TargetTypeFlag(),
			DNSHostingFlag(),
			NoDNSHostingFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesUpdate(ctx, cmd, output)
		},
	}
}

// WebsitesService defines the interface for website operations.
type WebsitesService interface {
	RequireAuthenticated() error
	List(ctx context.Context) ([]ipfs.WebsiteItem, error)
	Create(ctx context.Context, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error)
	CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error)
	UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error)
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
	GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
}

func initWebsitesService(ctx context.Context, cmd *cli.Command, output Output) (context.Context, context.CancelFunc, WebsitesService, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		cancel()
		return ctx, func() {}, nil, err
	}

	var websitesService WebsitesService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		cancel()
		return ctx, func() {}, nil, err
	}

	return ctx, cancel, websitesService, nil
}

func resolveRequiredArg(ctx context.Context, websitesService WebsitesService, cmd *cli.Command) (string, error) {
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
		{"Expired", fmt.Sprintf("%t", website.Expired)},
		{"Created", website.Created.Format("2006-01-02 15:04:05")},
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

func websitesList(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

	headers := []string{"ID", "NAME", "CID", "STATUS", "DNS", "GATEWAY", "VALIDATION", "CREATED"}
	rows := make([][]string, len(websites))
	for i, website := range websites {
		validation := "valid"
		if website.Expired {
			validation = "expired"
		} else if website.ValidationToken != "" {
			validation = website.ValidationToken
		}
		gateway := ""
		if website.GatewayDomain != nil {
			gateway = *website.GatewayDomain
		}
		rows[i] = []string{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			website.Status,
			fmt.Sprintf("%t", website.DnsHostingEnabled),
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
		if w.Domain == arg {
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

func websitesUpdate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	domain := cmd.String(FlagDomain)
	cid := cmd.String(FlagCID)
	targetType := cmd.String(FlagTargetType)
	if err := requireUpdateFields(cmd, FlagDomain, FlagCID, FlagTargetType, FlagDNSHosting, FlagNoDNSHosting); err != nil {
		return err
	}

	req := ipfs.WebsiteUpdateRequest{}

	if cmd.IsSet(FlagDomain) {
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
		Description: `Convert a website from IPFS to IPNS targeting.

An IPNS key will be auto-created and the current CID will be published to it.
This enables content-addressed updates without changing the domain's DNS records.

If --cid is provided, the IPNS key will publish that CID instead of the current one.

Examples:
  pinner websites enable-ipns example.com
  pinner websites enable-ipns example.com --cid QmNewHash
  pinner websites enable-ipns example.com --json`,
		ArgsUsage: "<domain>",
		Flags: []cli.Flag{
			CIDFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesEnableIPNS(ctx, cmd, output)
		},
	}
}

func websitesEnableIPNS(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

func websitesGet(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

	id, err := resolveRequiredArg(ctx, websitesService, cmd)
	if err != nil {
		return err
	}

	website, err := websitesService.Get(ctx, id)
	if err != nil {
		return err
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
		{"Expired", fmt.Sprintf("%t", website.Expired)},
		{"Validation Token", website.ValidationToken},
	}

	if website.GatewayDomain != nil {
		fields = append(fields, Field{"Gateway", *website.GatewayDomain})
	}

	if website.ValidationExpiresAt != nil {
		fields = append(fields, Field{"Token Expires", website.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
	}

	if website.DnsZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *website.DnsZoneId)})
	}

	fields = append(fields, Field{"Created", website.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

	nameservers := getNameservers(ctx, websitesService)
	output.Printfln("")
	showDNSRecordInstructions(output, website, nameservers)

	if website.Expired {
		output.Printfln("")
		output.Printfln("⚠ This website's validation has expired.")
		output.Printfln("  Re-validate: pinner websites validate %d", website.Id)
	}

	return nil
}

func websitesCreate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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
		Domain:       domain,
		TargetHash:  cid,
		TargetType:  targetType,
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

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", fmt.Sprintf("%d", createdWebsite.Id)},
			{"Domain", createdWebsite.Domain},
			{"CID", createdWebsite.TargetHash},
			{"Target Type", createdWebsite.TargetType},
			{"Status", createdWebsite.Status},
			{"DNS Hosting", fmt.Sprintf("%t", createdWebsite.DnsHostingEnabled)},
			{"Expired", fmt.Sprintf("%t", createdWebsite.Expired)},
		},
	})

	if createdWebsite.GatewayDomain != nil {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway", *createdWebsite.GatewayDomain},
			},
		})
	}

	output.Printfln("")
	output.Printfln("Validation token: %s", createdWebsite.ValidationToken)
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
	output.Printfln("DNS hosting is enabled — Pinner manages your DNS records.")
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

	records := [][]string{
		{website.Domain, "TXT", "lumeweb-verify=" + website.ValidationToken},
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
		Description: `Delete a website by domain. This operation is irreversible.

Examples:
  pinner websites delete example.com
  pinner websites delete example.com --json`,
		ArgsUsage: "<domain>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesDelete(ctx, cmd, output)
		},
	}
}

func newWebsitesValidateCommand() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "Validate a website",
		Description: `Validate a website by domain to check if DNS is properly configured.

Examples:
  pinner websites validate example.com
  pinner websites validate example.com --json`,
		ArgsUsage: "<domain>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesValidate(ctx, cmd, output)
		},
	}
}

func websitesDelete(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

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

func websitesValidate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

	return doWebsitesValidate(ctx, cmd, output, websitesService)
}

func doWebsitesValidate(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

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
		}

		if website != nil {
			nameservers := getNameservers(ctx, websitesService)
			result["required_records"] = buildRequiredRecords(website, nameservers)
		}

		return output.PrintJSON(result)
	}

	if validateErr != nil {
		output.Printfln("Validation failed: %s", validateErr)
		output.Printfln("")
		if website != nil {
			nameservers := getNameservers(ctx, websitesService)
			showDNSRecordInstructions(output, website, nameservers)
		}
		output.Printfln("")
		output.Printfln("Re-validate after adding the records: pinner websites validate %s", arg)
		return nil
	}

	output.Printfln("Website Validation Result")

	statusIcon := "⏳"
	if validationResult.Valid {
		statusIcon = "✅"
	}

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Domain", validationResult.Domain},
			{"ID", fmt.Sprintf("%d", validationResult.Id)},
			{"Valid", fmt.Sprintf("%s %t", statusIcon, validationResult.Valid)},
			{"Message", validationResult.Message},
		},
	})

	if !validationResult.Valid {
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

	return nil
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

	records := []map[string]string{
		{"name": website.Domain, "type": "TXT", "value": "lumeweb-verify=" + website.ValidationToken},
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
		Description: `Show the website hosting configuration including the gateway domain.

Use this to find the gateway domain for setting up CNAME records with your DNS provider.

Examples:
  pinner websites config
  pinner websites config --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return websitesConfig(ctx, cmd, output)
		},
	}
}

func websitesConfig(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel, websitesService, err := initWebsitesService(ctx, cmd, output)
	if err != nil {
		return err
	}
	defer cancel()

	if err := websitesService.RequireAuthenticated(); err != nil {
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
