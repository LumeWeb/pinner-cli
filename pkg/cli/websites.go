package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
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

Examples:
  pinner websites update example.com --cid QmNewHash
  pinner websites update example.com --cid QmNewHash --target-type ipfs
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

func websitesList(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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

	if req.TargetType != nil || req.TargetHash != nil {
		if req.TargetType == nil || req.TargetHash == nil {
			return fmt.Errorf("--target-type and --cid must both be provided")
		}
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

	if updatedWebsite.DnsHostingEnabled {
		if err := setupDNSHosting(ctx, cfgMgr, output, updatedWebsite); err != nil {
			output.Printfln("Warning: Failed to setup DNS hosting: %v", err)
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	output.Printfln("Website updated successfully")

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", fmt.Sprintf("%d", updatedWebsite.Id)},
			{"Domain", updatedWebsite.Domain},
			{"CID", updatedWebsite.TargetHash},
			{"Target Type", updatedWebsite.TargetType},
			{"Status", updatedWebsite.Status},
			{"DNS Hosting", fmt.Sprintf("%t", updatedWebsite.DnsHostingEnabled)},
			{"Expired", fmt.Sprintf("%t", updatedWebsite.Expired)},
			{"Created", updatedWebsite.Created.Format("2006-01-02 15:04:05")},
		},
	})

	if updatedWebsite.GatewayDomain != nil {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway", *updatedWebsite.GatewayDomain},
			},
		})
	}

	if updatedWebsite.GatewayDomain != nil && *updatedWebsite.GatewayDomain != "" {
		output.Printfln("")
		output.Printfln("CNAME record to point your domain to the gateway:")
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
			{updatedWebsite.Domain, "CNAME", *updatedWebsite.GatewayDomain},
		})
	}

	if updatedWebsite.Expired {
		output.Printfln("")
		output.Printfln("⚠ This website's validation has expired.")
		output.Printfln("  Re-validate: pinner websites validate %d", updatedWebsite.Id)
	}

	return nil
}

func websitesGet(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	arg := args.First()

	website, err := resolveAndGetWebsite(ctx, websitesService, arg)
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

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		output.Printfln("")
		output.Printfln("CNAME record to point your domain to the gateway:")
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
			{website.Domain, "CNAME", *website.GatewayDomain},
		})
	}

	if website.Expired {
		output.Printfln("")
		output.Printfln("⚠ This website's validation has expired.")
		output.Printfln("  Re-validate: pinner websites validate %d", website.Id)
	}

	return nil
}

func websitesCreate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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

	if createdWebsite.DnsHostingEnabled {
		if err := setupDNSHosting(ctx, cfgMgr, output, createdWebsite); err != nil {
			output.Printfln("Warning: Failed to setup DNS hosting: %v", err)
		}
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

	showDNSRecordInstructions(output, createdWebsite)

	output.Printfln("")
	output.Printfln("Validate: pinner websites validate %s", createdWebsite.Domain)

	if !createdWebsite.DnsHostingEnabled {
		output.Printfln("")
		output.Printfln("  Tip: Use --dns-hosting to have Pinner manage DNS for you")
	}

	return nil
}

// showDNSRecordInstructions displays the DNS records a user needs to add for their website.
func showDNSRecordInstructions(output Output, website *ipfs.WebsiteItem) {
	if website == nil {
		return
	}

	output.Printfln("Required DNS records:")

	records := [][]string{
		{website.Domain, "TXT", "lumeweb-verify=" + website.ValidationToken},
	}

	if !website.DnsHostingEnabled {
		records = append(records, []string{"_dnslink." + website.Domain, "TXT", "dnslink=/" + website.TargetType + "/" + website.TargetHash})
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, []string{website.Domain, "CNAME", *website.GatewayDomain})
	}

	output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, records)
}

// setupDNSHosting creates a DNS zone and auto-created records for a website
func setupDNSHosting(ctx context.Context, cfgMgr config.Manager, output Output, website *ipfs.WebsiteItem) error {
	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	domain := website.Domain
	targetHash := website.TargetHash

	output.Printfln("Setting up DNS hosting for %s...", domain)

	zone, err := dnsService.CreateZone(ctx, domain, nil)
	if err != nil {
		return fmt.Errorf("failed to create DNS zone: %w", err)
	}

	output.Printfln("  ✓ Created DNS zone (ID: %d, Status: %s)", zone.Id, zone.Status)

	ttl := 3600

	records := []ipfs.RecordRequest{
		{
			Name:    "_dnslink." + domain,
			Type:    "TXT",
			Content: "/ipfs/" + targetHash,
			Ttl:     &ttl,
		},
		{
			Name:    domain,
			Type:    "TXT",
			Content: "lumeweb-verify=" + website.ValidationToken,
			Ttl:     &ttl,
		},
		{
			Name:    "www." + domain,
			Type:    "CNAME",
			Content: domain,
			Ttl:     &ttl,
		},
	}

	for _, record := range records {
		created, err := dnsService.CreateRecord(ctx, domain, record)
		if err != nil {
			output.Printfln("  ✗ Failed to create record %s %s: %v", record.Name, record.Type, err)
			continue
		}
		output.Printfln("  ✓ Created DNS record: %s %s", created.Name, created.Type)
	}

	return nil
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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

	if err := websitesService.Delete(ctx, id); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("Website %s deleted successfully", arg),
		}
		return output.PrintJSON(result)
	}

	output.Printfln("Website %s deleted successfully", arg)

	return nil
}

func websitesValidate(ctx context.Context, cmd *cli.Command, output Output) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var websitesService WebsitesService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

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
			result["required_records"] = buildRequiredRecords(website)
		}

		return output.PrintJSON(result)
	}

	if validateErr != nil {
		output.Printfln("Validation failed: %s", validateErr)
		output.Printfln("")
		if website != nil {
			showDNSRecordInstructions(output, website)
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
			showDNSRecordInstructions(output, website)
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
func buildRequiredRecords(website *ipfs.WebsiteItem) []map[string]string {
	if website == nil {
		return nil
	}

	records := []map[string]string{
		{"name": website.Domain, "type": "TXT", "value": "lumeweb-verify=" + website.ValidationToken},
	}

	if !website.DnsHostingEnabled {
		records = append(records, map[string]string{
			"name": "_dnslink." + website.Domain, "type": "TXT",
			"value": "dnslink=/" + website.TargetType + "/" + website.TargetHash,
		})
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
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

	if config.GatewayDomain != nil && *config.GatewayDomain != "" {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway Domain", *config.GatewayDomain},
			},
		})
		output.Printfln("")
		output.Printfln("CNAME record to point your domain to the gateway:")
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
			{"<your-domain>", "CNAME", *config.GatewayDomain},
		})
	} else {
		output.Printfln("  No gateway domain configured")
	}

	return nil
}
