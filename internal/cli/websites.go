package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// stripValidationPrefix strips the "key=" prefix from a validation token value.
// Some server builds return the full DNS TXT record value (e.g.
// "pinner-verify=abc123"); for English display of the token alone we only want
// the portion after the "=".
func stripValidationPrefix(token string) string {
	if idx := strings.Index(token, "="); idx >= 0 {
		return token[idx+1:]
	}
	return token
}

// validationRecordValue returns the full DNS TXT record value the server
// validates a website against: "<key>=<token>". The verification key is
// server-provided — the server embeds it as the first DNS label of the
// validation record host (e.g. host "pinner-verify.example.com" carries the
// key "pinner-verify") and validates the TXT record value as
// "<key>=<token>". It is never hardcoded here.
//
// Some server builds return ValidationToken already carrying the "key="
// prefix (see stripValidationPrefix); strip any such prefix before
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

func resolveRequiredArg(ctx context.Context, websitesService WebsitesService, cmd websitesCommandGetter) (string, error) {
	args := cmd.Args()
	if args.Len() == 0 {
		return "", fmt.Errorf("website ID or domain is required")
	}

	return websites.ResolveWebsiteID(ctx, websitesService, args.First())
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

// resolveAndGetWebsite resolves a website by ID or domain name.
// If arg is a numeric ID, it fetches directly. Otherwise, it searches by domain.
func resolveAndGetWebsite(ctx context.Context, websitesService WebsitesService, arg string) (*ipfs.WebsiteItem, error) {
	id, err := websites.ResolveWebsiteID(ctx, websitesService, arg)
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
		{validationHost, "TXT", validationRecordValue(website)},
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

	id, err := websites.ResolveWebsiteID(ctx, websitesService, arg)
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
		{"name": validationHost, "type": "TXT", "value": validationRecordValue(website)},
		{"name": "_dnslink." + website.Domain, "type": "TXT", "value": "dnslink=/" + website.TargetType + "/" + website.TargetHash},
	}

	if website.GatewayDomain != nil && *website.GatewayDomain != "" {
		records = append(records, map[string]string{
			"name": website.Domain, "type": "CNAME", "value": *website.GatewayDomain,
		})
	}

	return records
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
