package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newWebsitesCommand() *cli.Command {
	return &cli.Command{
		Name:  "websites",
		Usage: "Manage websites",
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
	UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
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

	headers := []string{"ID", "NAME", "CID", "STATUS", "DNS", "VALIDATION", "CREATED"}
	rows := make([][]string, len(websites))
	for i, website := range websites {
		validation := "valid"
		if website.Expired {
			validation = "expired"
		} else if website.ValidationToken != "" {
			validation = website.ValidationToken
		}
		rows[i] = []string{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			website.Status,
			fmt.Sprintf("%t", website.DnsHostingEnabled),
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

	req := ipfs.WebsiteRequest{
		Domain:      domain,
		TargetHash:  cid,
		TargetType:  targetType,
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
		if err := setupDNSHosting(ctx, cfgMgr, output, updatedWebsite.Domain, updatedWebsite.TargetHash); err != nil {
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

	if website.ValidationExpiresAt != nil {
		fields = append(fields, Field{"Token Expires", website.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
	}

	if website.DnsZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *website.DnsZoneId)})
	}

	fields = append(fields, Field{"Created", website.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

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
		if err := setupDNSHosting(ctx, cfgMgr, output, createdWebsite.Domain, createdWebsite.TargetHash); err != nil {
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
			{"Created", createdWebsite.Created.Format("2006-01-02 15:04:05")},
		},
	})

	output.Printfln("")
	output.Printfln("Validation token: %s", createdWebsite.ValidationToken)
	output.Printfln("")
	output.Printfln("Next steps:")
	if createdWebsite.DnsHostingEnabled {
		output.Printfln("  1. Point your domain's nameservers to Pinner (see: pinner dns zones validate %s)", createdWebsite.Domain)
		output.Printfln("  2. Add a TXT record at your registrar to verify ownership:")
		output.Printfln("     %s  TXT  lumeweb-verify=%s", createdWebsite.Domain, createdWebsite.ValidationToken)
		output.Printfln("  3. Validate: pinner websites validate %s", createdWebsite.Domain)
	} else {
		output.Printfln("  1. Add a TXT record at your registrar to verify ownership:")
		output.Printfln("     %s  TXT  lumeweb-verify=%s", createdWebsite.Domain, createdWebsite.ValidationToken)
		output.Printfln("  2. Add a DNSLink TXT record:")
		output.Printfln("     _dnslink.%s  TXT  dnslink=/%s/%s", createdWebsite.Domain, createdWebsite.TargetType, createdWebsite.TargetHash)
		output.Printfln("  3. Validate: pinner websites validate %s", createdWebsite.Domain)
		output.Printfln("")
		output.Printfln("  Tip: Use --dns-hosting to have Pinner manage DNS for you")
	}

	return nil
}

// setupDNSHosting creates a DNS zone and auto-created records for a website
func setupDNSHosting(ctx context.Context, cfgMgr config.Manager, output Output, domain, targetHash string) error {
	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	output.Printfln("Setting up DNS hosting for %s...", domain)

	zone, err := dnsService.CreateZone(ctx, domain, nil)
	if err != nil {
		return fmt.Errorf("failed to create DNS zone: %w", err)
	}

	output.Printfln("  ✓ Created DNS zone (ID: %d, Status: %s)", zone.Id, zone.Status)



	validationToken, err := generateValidationToken()
	if err != nil {
		return fmt.Errorf("failed to generate validation token: %w", err)
	}
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
			Content: "lumeweb-verify=" + validationToken,
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

// generateValidationToken generates a random validation token
func generateValidationToken() (string, error) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate validation token: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
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

	validationResult, err := websitesService.Validate(ctx, id)
	if err != nil {
		return err
	}

	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	records, err := dnsService.ListRecords(ctx, validationResult.Domain)
	if err == nil {
		requiredRecords := []struct {
			name    string
			rtype   string
			present bool
		}{
			{"_dnslink." + validationResult.Domain, "TXT", false},
			{validationResult.Domain, "TXT", false},
			{"www." + validationResult.Domain, "CNAME", false},
		}

		for i := range requiredRecords {
			for _, record := range records {
				if record.Name == requiredRecords[i].name && record.Type == requiredRecords[i].rtype {
					requiredRecords[i].present = true
					break
				}
			}
		}

		headers := []string{"RECORD", "TYPE", "STATUS"}
		rows := make([][]string, len(requiredRecords))
		for i, rr := range requiredRecords {
			status := "✗"
			if rr.present {
				status = "✓"
			}
			rows[i] = []string{rr.name, rr.rtype, status}
		}
		output.PrintTable(headers, rows)
	}

	if output.IsJSON() {
		return output.PrintJSON(validationResult)
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
		output.Printfln("Next steps:")
		output.Printfln("  1. Make sure you have added the required DNS records to your domain")
		output.Printfln("  2. Check DNS record status above — missing records are marked with ✗")
		output.Printfln("  3. Re-validate after making changes: pinner websites validate %d", validationResult.Id)
		output.Printfln("  4. View website details: pinner websites get %d", validationResult.Id)
	}

	return nil
}
