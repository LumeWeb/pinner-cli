package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
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
  pinner websites create --domain example.com --target-hash QmHash
  pinner websites get 1
  pinner websites update 1 --domain new-example.com --target-hash QmNewHash
  pinner websites delete 1
  pinner websites validate 1`,
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
  pinner websites create --domain example.com --target-hash QmHash
  pinner websites create --domain example.com --target-hash QmHash --target-type ipfs
  pinner websites create --domain example.com --target-hash QmHash --dns-hosting
  pinner websites create --domain example.com --target-hash QmHash --json`,
		Flags: []cli.Flag{
			DomainFlag(),
			TargetHashFlag(),
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
		Usage: "Get a website by ID",
		Description: `Get details of a specific website by its ID.

Examples:
  pinner websites get 1
  pinner websites get 1 --json`,
		ArgsUsage: "<id>",
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
		Description: `Update an existing website with new domain, target CID, or target type.

At least one of the optional fields must be provided to update the website.

Examples:
  pinner websites update 1 --domain new-example.com
  pinner websites update 1 --target-hash QmNewHash
  pinner websites update 1 --domain new-example.com --target-hash QmNewHash --target-type ipfs
  pinner websites update 1 --dns-hosting
  pinner websites update 1 --no-dns-hosting
  pinner websites update 1 --json`,
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			DomainFlag(),
			TargetHashFlag(),
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
	List(ctx context.Context) ([]ipfsclient.WebsiteItem, error)
	Create(ctx context.Context, domain, targetHash, targetType string) (*ipfsclient.WebsiteItem, error)
	Get(ctx context.Context, id string) (*ipfsclient.WebsiteItem, error)
	Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfsclient.WebsiteItem, error)
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) (*ipfsclient.WebsiteValidateResponse, error)
	GetSSLStatus(ctx context.Context, domain string) (*ipfsclient.WebsiteResponse, error)
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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
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
		output.Printf("No websites found")
		return nil
	}

	if output.IsJSON() {
		result := map[string]any{
			"count":    len(websites),
			"websites": websites,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d website(s)", len(websites))

	headers := []string{"ID", "NAME", "CID", "STATUS", "CREATED"}
	rows := make([][]string, len(websites))
	for i, website := range websites {
		rows[i] = []string{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			website.Status,
			website.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID is required")
	}

	id := args.First()

	domain := cmd.String(FlagDomain)
	targetHash := cmd.String(FlagTargetHash)
	targetType := cmd.String(FlagTargetType)
	dnsHosting := cmd.Bool(FlagDNSHosting)
	noDNSHosting := cmd.Bool(FlagNoDNSHosting)

	if domain == "" && targetHash == "" && targetType == "" && !dnsHosting && !noDNSHosting {
		return fmt.Errorf("at least one field must be provided for update (domain, target-hash, target-type, or dns-hosting flags)")
	}

	updatedWebsite, err := websitesService.Update(ctx, id, domain, targetHash, targetType)
	if err != nil {
		return err
	}

	if dnsHosting {
		if err := setupDNSHosting(ctx, cfgMgr, output, updatedWebsite.Domain, updatedWebsite.TargetHash); err != nil {
			output.Printf("Warning: Failed to setup DNS hosting: %v", err)
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	output.Printf("Website updated successfully")

	headers := []string{"ID", "NAME", "CID", "STATUS", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", updatedWebsite.Id),
			updatedWebsite.Domain,
			updatedWebsite.TargetHash,
			updatedWebsite.Status,
			updatedWebsite.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID is required")
	}

	id := args.First()

	website, err := websitesService.Get(ctx, id)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printf("Website Details")

	headers := []string{"ID", "NAME", "CID", "STATUS", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			website.Status,
			website.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	domain := cmd.String(FlagDomain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	targetHash := cmd.String(FlagTargetHash)
	if targetHash == "" {
		return fmt.Errorf("target hash is required")
	}

	targetType := cmd.String(FlagTargetType)
	if targetType == "" {
		targetType = "ipfs"
	}

	createdWebsite, err := websitesService.Create(ctx, domain, targetHash, targetType)
	if err != nil {
		return err
	}

	dnsHosting := cmd.Bool(FlagDNSHosting)

	if dnsHosting {
		if err := setupDNSHosting(ctx, cfgMgr, output, domain, targetHash); err != nil {
			output.Printf("Warning: Failed to setup DNS hosting: %v", err)
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(createdWebsite)
	}

	output.Printf("Website created successfully")

	headers := []string{"ID", "NAME", "CID", "STATUS", "CREATED"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", createdWebsite.Id),
			createdWebsite.Domain,
			createdWebsite.TargetHash,
			createdWebsite.Status,
			createdWebsite.Created.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	if dnsHosting {
		output.Printf("\nDNS hosting enabled for this domain")
		output.Printf("Next steps:")
		output.Printf("  1. Validate nameserver delegation: pinner dns zones validate %s", domain)
		output.Printf("  2. Validate website DNS records: pinner websites validate %d", createdWebsite.Id)
	}

	return nil
}

// setupDNSHosting creates a DNS zone and auto-created records for a website
func setupDNSHosting(ctx context.Context, cfgMgr config.Manager, output Output, domain, targetHash string) error {
	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	output.Printf("Setting up DNS hosting for %s...", domain)

	zone, err := dnsService.CreateZone(ctx, domain, nil)
	if err != nil {
		return fmt.Errorf("failed to create DNS zone: %w", err)
	}

	output.Printf("  ✓ Created DNS zone (ID: %d, Status: %s)", zone.Id, zone.Status)



	validationToken := generateValidationToken()
	ttl := 3600

	records := []ipfsclient.RecordRequest{
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
			output.Printf("  ✗ Failed to create record %s %s: %v", record.Name, record.Type, err)
			continue
		}
		output.Printf("  ✓ Created DNS record: %s %s", created.Name, created.Type)
	}

	return nil
}

// generateValidationToken generates a random validation token
func generateValidationToken() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

func newWebsitesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a website",
		Description: `Delete a website by its ID. This operation is irreversible.

Examples:
  pinner websites delete 1
  pinner websites delete 1 --json`,
		ArgsUsage: "<id>",
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
		Description: `Validate a website by its ID to check if the domain is properly configured.

Examples:
  pinner websites validate 1
  pinner websites validate 1 --json`,
		ArgsUsage: "<id>",
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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID is required")
	}

	id := args.First()

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

	output.Printf("Website %s deleted successfully", id)

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
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID is required")
	}

	id := args.First()

	validationResult, err := websitesService.Validate(ctx, id)
	if err != nil {
		return err
	}

	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	records, err := dnsService.ListRecords(ctx, validationResult.Domain)
	if err == nil {
		output.Printf("\nDNS Records Check:")

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

		for _, rr := range requiredRecords {
			icon := "✗"
			if rr.present {
				icon = "✓"
			}
			output.Printf("  %s %s %s", icon, rr.name, rr.rtype)
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(validationResult)
	}

	output.Printf("Website Validation Result")

	statusIcon := "⏳"
	if validationResult.Valid {
		statusIcon = "✅"
	}

	headers := []string{"DOMAIN", "ID", "VALID", "MESSAGE"}
	rows := [][]string{
		{
			validationResult.Domain,
			fmt.Sprintf("%d", validationResult.Id),
			fmt.Sprintf("%s %t", statusIcon, validationResult.Valid),
			validationResult.Message,
		},
	}
	output.PrintTable(headers, rows)

	return nil
}
