package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	coreadmin "go.lumeweb.com/pinner-cli/internal/core/admin"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// catalog_admin_wiring.go adapts the admin domain operations in
// internal/catalogops to the urfave CLI: it compiles catalog operations into
// commands under the "admin" parent, renders each handler's result through the
// Output formatter, and maps positionals and the destructive --force gate onto
// operation inputs. IO and CLI concerns live here, not in catalogops.
//
// Admin sections are added one at a time. This file currently mounts the
// platform-domains section (admin_platform_domains_*) under `admin
// platform-domains`; later sections (websites, quota, billing) add their own
// parent commands alongside it.

// catalogAdminDeps builds the catalogops.AdminDeps from the live CLI wiring.
// Services resolve lazily per invocation via the core factories; config is read
// at request time.
func catalogAdminDeps() catalogops.AdminDeps {
	return catalogops.AdminDeps{
		CfgMgr: func() config.Manager {
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return nil
			}
			return cfgMgr
		},
		PlatformDomainAdminService: func(cfgMgr config.Manager) (coreadmin.PlatformDomainAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultPlatformDomainAdminServiceFactory(cfgMgr), nil
		},
		WebsiteAdminService: func(cfgMgr config.Manager) (coreadmin.WebsiteAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultWebsiteAdminServiceFactory(cfgMgr), nil
		},
		QuotaAdminService: func(cfgMgr config.Manager) (coreadmin.QuotaAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultQuotaAdminServiceFactory(cfgMgr), nil
		},
		BillingAdminService: func(cfgMgr config.Manager) (coreadmin.BillingAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultBillingAdminServiceFactory(cfgMgr), nil
		},
	}
}

// adminCatalogDepsVar is an indirection so the wiring and the renderer can both
// reach the canonical operation list without rebuilding it repeatedly.
var adminCatalogDepsVar = catalogops.AdminDeps(catalogAdminDeps())

// newAdminPlatformDomainsCatalogCommand compiles the admin platform-domains
// catalog operations and returns the `platform-domains` command to mount under
// the `admin` parent: list, register, update, delete, bind.
func newAdminPlatformDomainsCatalogCommand() *cli.Command {
	return newAdminSectionCommand("admin_platform_domains_", CmdPlatformDomains, "Manage platform (free-subdomain) root domains")
}

// newAdminWebsitesCatalogCommand compiles the admin websites catalog operations
// into a `websites` command for the `admin` parent: block, unblock.
func newAdminWebsitesCatalogCommand() *cli.Command {
	return newAdminSectionCommand("admin_websites_", CmdWebsites, "Manage IPFS websites (admin)")
}

// adminSectionGroup maps an admin sub-op prefix (after the section prefix) to
// its CLI subgroup command name. Group and leaf segments can both span multiple
// underscore tokens (user_configs_list, plans_set_default), so the group is
// matched by prefix rather than by splitting on the first underscore.
type adminSectionGroup struct {
	prefix string
	name   string
}

// newAdminGroupedSection compiles the admin catalog operations whose names start
// with sectionPrefix into a single parent command under `admin`. Ops matching a
// group prefix mount under that subgroup; the rest mount directly on the parent.
// Each leaf is stripped of the section prefix and hyphenated.
func newAdminGroupedSection(parentName, usage, sectionPrefix string, groups []adminSectionGroup) *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.AdminOperations(adminCatalogDepsVar) {
		if strings.HasPrefix(op.Name(), sectionPrefix) {
			_ = cat.Add(op)
		}
	}
	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile admin section %q: %v", parentName, err))
	}

	parent := &cli.Command{
		Name:     parentName,
		Category: "Admin",
		Usage:    usage,
		Commands: []*cli.Command{},
	}
	subgroups := map[string]*cli.Command{}
	for _, c := range compiled {
		remainder := strings.TrimPrefix(c.Name, sectionPrefix)
		group, leaf := splitAdminGroup(remainder, groups)
		mounted := mountAdminSectionCommand(c, sectionPrefix)
		mounted.Name = leaf
		if group == "" {
			parent.Commands = append(parent.Commands, mounted)
			continue
		}
		sub, ok := subgroups[group]
		if !ok {
			sub = &cli.Command{Name: group, Category: "Admin", Usage: "Manage " + group + " (admin)", Commands: []*cli.Command{}}
			subgroups[group] = sub
			parent.Commands = append(parent.Commands, sub)
		}
		sub.Commands = append(sub.Commands, mounted)
	}
	return parent
}

// splitAdminGroup splits an admin op remainder (after the section prefix) into
// (group, leaf) using the group prefix list. An op matching a group prefix
// lands in that group; otherwise it is a single leaf.
func splitAdminGroup(remainder string, groups []adminSectionGroup) (group, leaf string) {
	for _, g := range groups {
		if strings.HasPrefix(remainder, g.prefix) {
			return g.name, hyphenate(strings.TrimPrefix(remainder, g.prefix))
		}
	}
	return "", hyphenate(remainder)
}

// quotaGroups maps the admin quota subgroup prefixes to their CLI names.
var quotaGroups = []adminSectionGroup{
	{"plans_", CmdPlans},
	{"allowances_", CmdAllowances},
	{"user_configs_", CmdUserConfigs},
}

// newAdminQuotaCatalogCommand compiles the admin quota operations into a
// `quota` command.
func newAdminQuotaCatalogCommand() *cli.Command {
	return newAdminGroupedSection(CmdQuota, "Quota management operations", "admin_quota_", quotaGroups)
}

// billingGroups maps the admin billing subgroup prefixes to their CLI names.
var billingGroups = []adminSectionGroup{
	{"credits_", CmdCredits},
	{"price_lines_", CmdPriceLines},
	{"pricing_plan_periods_", CmdPricingPlanPeriods},
	{"pricing_plans_", CmdPricingPlans},
	{"subscribers_", CmdSubscribers},
}

// newAdminBillingCatalogCommand compiles the admin billing operations into a
// `billing` command. The overview op mounts as a single leaf on billing.
func newAdminBillingCatalogCommand() *cli.Command {
	return newAdminGroupedSection(CmdBilling, "Billing management operations", "admin_billing_", billingGroups)
}

// hyphenate replaces underscores with hyphens for a CLI command name.
func hyphenate(s string) string {
	return strings.ReplaceAll(s, "_", "-")
}

// newAdminSectionCommand compiles the admin catalog operations whose names start
// with prefix into a single parent command under `admin`, stripping prefix from
// each leaf name. Leaves have their flag-required markers relaxed so positionals
// can supply required args, and their Action wrapped with the CLI adapter.
func newAdminSectionCommand(prefix, parentName, usage string) *cli.Command {
	cat := catalog.NewCatalog()
	for _, op := range catalogops.AdminOperations(adminCatalogDepsVar) {
		if !strings.HasPrefix(op.Name(), prefix) {
			continue
		}
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		panic(fmt.Sprintf("catalog compile admin section %q: %v", parentName, err))
	}

	parent := &cli.Command{
		Name:     parentName,
		Category: "Admin",
		Usage:    usage,
		Commands: []*cli.Command{},
	}
	for _, c := range compiled {
		parent.Commands = append(parent.Commands, mountAdminSectionCommand(c, prefix))
	}
	return parent
}

// mountAdminSectionCommand adapts a single catalog-compiled command into a live
// admin subcommand: strips the section prefix, relaxes flag-required markers so
// positionals supply required args, and wraps the Action with the CLI adapter.
func mountAdminSectionCommand(cmd *cli.Command, prefix string) *cli.Command {
	canonical := cmd.Name
	display := strings.TrimPrefix(canonical, prefix)
	cmd.Name = display
	cmd.Category = "Admin"

	relaxFlagRequired(cmd)

	var op catalog.Operation
	for _, cand := range catalogops.AdminOperations(adminCatalogDepsVar) {
		if cand.Name() == canonical {
			op = cand
			break
		}
	}
	if op != nil {
		cmd.Action = adminActionAdapter(op)
	}
	return cmd
}

// adminActionAdapter returns the per-invocation ActionFunc for an admin catalog
// operation. It builds the input map from flags plus resolved positionals,
// threads the --auth-token override, applies the destructive --force gate, and
// renders the handler result.
func adminActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		input := catalog.FlagsToInput(c, op)
		// Note: no --auth-token override is threaded here. Admin services read
		// auth from the live config manager's token, so a per-invocation flag
		// override is not currently supported for the admin domain.
		if err := applyPositionalArgs(op, input, c.Args()); err != nil {
			return err
		}

		// Destructive gate: the delete op requires confirm=true. Map --force (or
		// --confirm) onto the op's confirm input before execution.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			input["confirm"] = confirm
			if !confirm {
				return fmt.Errorf("admin platform-domains delete: pass --force to confirm this destructive operation")
			}
		}

		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
		if err != nil {
			return err
		}
		return renderAdminResult(ctx, c, op, result)
	}
}

// renderAdminResult renders an admin handler's typed result through the CLI
// Output formatter.
func renderAdminResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)
	if result != nil && isNilPointerResult(result) {
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case *catalogops.AdminPlatformDomainsListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "platform_domains": r.PlatformDomains})
		}
		output.Printfln("Found %d platform domain(s)", r.Count)
		if len(r.PlatformDomains) == 0 {
			return nil
		}
		headers := []string{"ID", "DOMAIN", "NAMESPACE", "ZONE", "ENABLED"}
		rows := make([][]string, len(r.PlatformDomains))
		for i, d := range r.PlatformDomains {
			rows[i] = []string{
				fmt.Sprintf("%d", d.Id), d.Domain, d.Namespace,
				fmt.Sprintf("%d", d.ZoneId), yesNo(d.Enabled),
			}
		}
		output.PrintTable(headers, rows)
		return nil

	case *admin.PlatformDomain:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Platform domain %s (ID %d)", r.Domain, r.Id)
		return nil

	case *admin.Website:
		// admin websites block/unblock return a Website (embedded response).
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Website %s (ID %d): %s", r.Domain, r.Id, r.Status)
		return nil

	case *admin.RootDomain:
		// admin platform-domains bind returns a RootDomain (the bound apex).
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Bound website to platform domain %s (domain ID %d)", r.Domain, r.Id)
		return nil

	case *catalogops.AdminPlatformDomainsDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "id": r.ID})
		}
		output.Printfln("Platform domain %s deleted", r.ID)
		return nil

	case *catalogops.QuotaPlansListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "plans": r.Plans})
		}
		output.Printfln("Found %d quota plan(s)", r.Count)
		if len(r.Plans) == 0 {
			return nil
		}
		headers := []string{"ID", "NAME", "UPLOAD", "DOWNLOAD", "STORAGE", "ACTIVE", "DEFAULT"}
		rows := make([][]string, len(r.Plans))
		for i, p := range r.Plans {
			rows[i] = []string{
				fmt.Sprintf("%d", p.Id), p.Name,
				formatQuotaBytes(p.UploadLimitBytes), formatQuotaBytes(p.DownloadLimitBytes),
				formatQuotaBytes(p.StorageLimitBytes),
				fmt.Sprintf("%t", p.IsActive), yesNo(p.IsDefault),
			}
		}
		output.PrintTable(headers, rows)
		return nil

	case *admin.QuotaPlan:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Quota plan %s (ID %d)", r.Name, r.Id)
		return nil

	case *catalogops.QuotaPlansDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "id": r.ID})
		}
		output.Printfln("Quota plan %s deleted", r.ID)
		return nil

	case *catalogops.QuotaPlansSetDefaultResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"id": r.ID, "is_default": r.IsDefault})
		}
		output.Printfln("Quota plan %s is now the default", r.ID)
		return nil

	case *catalogops.QuotaAllowancesListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "allowances": r.Allowances})
		}
		output.Printfln("Found %d quota allowance(s)", r.Count)
		if len(r.Allowances) == 0 {
			return nil
		}
		headers := []string{"ID", "USER", "SOURCE", "TYPE", "BYTES", "ACTIVE"}
		rows := make([][]string, len(r.Allowances))
		for i, a := range r.Allowances {
			rows[i] = []string{
				fmt.Sprintf("%d", a.Id), fmt.Sprintf("%d", a.UserId), string(a.Source),
				string(a.Type), formatQuotaBytes(a.Bytes), yesNo(a.IsActive),
			}
		}
		output.PrintTable(headers, rows)
		return nil

	case *admin.QuotaAllowance:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Quota allowance ID %d for user %d", r.Id, r.UserId)
		return nil

	case *catalogops.QuotaAllowancesDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "grant_id": r.GrantID})
		}
		output.Printfln("Quota allowance %s deleted", r.GrantID)
		return nil

	case *catalogops.QuotaUserConfigsListResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "configs": r.Configs})
		}
		output.Printfln("Found %d user quota config(s)", r.Count)
		if len(r.Configs) == 0 {
			return nil
		}
		headers := []string{"USER", "PLAN"}
		rows := make([][]string, len(r.Configs))
		for i, c := range r.Configs {
			plan := "-"
			if c.QuotaPlanId != nil {
				plan = fmt.Sprintf("%d", *c.QuotaPlanId)
			}
			rows[i] = []string{fmt.Sprintf("%d", c.UserId), plan}
		}
		output.PrintTable(headers, rows)
		return nil

	case *admin.UserQuotaConfig:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("User %d quota config", r.UserId)
		return nil

	case *catalogops.QuotaUserConfigsResetResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"user_id": r.UserID, "reset": r.Reset})
		}
		output.Printfln("User %d quota plan reset", r.UserID)
		return nil

	case *admin.SystemStats:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Fields: []Field{
			{"Total Users", fmt.Sprintf("%d", r.TotalUsers)},
			{"Active Users", fmt.Sprintf("%d", r.ActiveUsers)},
			{"Total Plans", fmt.Sprintf("%d", r.TotalPlans)},
			{"Active Plans", fmt.Sprintf("%d", r.TotalActivePlans)},
			{"Total Grants", fmt.Sprintf("%d", r.TotalGrants)},
			{"Active Grants", fmt.Sprintf("%d", r.TotalActiveGrants)},
		}})
		return nil

	case *catalogops.QuotaReconcileResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"message": r.Message, "users_processed": r.UsersProcessed})
		}
		output.Printfln("Reconcile complete: %s (%d users processed)", r.Message, r.UsersProcessed)
		return nil

	case *catalogops.QuotaCleanupResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted})
		}
		output.Printfln("Cleaned up %d expired record(s)", r.Deleted)
		return nil

	case *catalogops.BillingCreditsListResult:
		return renderJSONOrCount(c, output, map[string]any{"count": r.Count, "credits": r.Credits}, "billing credit(s)", r.Count)
	case *catalogops.BillingUserDeletedCredits:
		return renderJSONOrCount(c, output, map[string]any{"user_id": r.UserID, "count": r.Count, "credits": r.Credits}, "deleted credit(s)", r.Count)
	case *catalogops.BillingPriceLinesListResult:
		return renderJSONOrCount(c, output, map[string]any{"count": r.Count, "price_lines": r.PriceLines}, "price line(s)", r.Count)
	case *catalogops.BillingPricingPlansListResult:
		return renderJSONOrCount(c, output, map[string]any{"count": r.Count, "plans": r.Plans}, "pricing plan(s)", r.Count)
	case *catalogops.BillingPricingPlanPeriodsListResult:
		return renderJSONOrCount(c, output, map[string]any{"count": r.Count, "periods": r.Periods}, "pricing plan period(s)", r.Count)
	case *catalogops.BillingSubscribersListResult:
		return renderJSONOrCount(c, output, map[string]any{"count": r.Count, "subscribers": r.Subscribers}, "subscriber(s)", r.Count)

	case *catalogops.BillingPurgeResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"purged": r.Purged})
		}
		output.Printfln("Purged %d credit(s)", r.Purged)
		return nil
	case *catalogops.BillingSyncResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"synced": r.Synced, "plan_id": r.PlanID})
		}
		output.Printfln("Pricing plan synced")
		return nil
	case *catalogops.BillingGenericActionResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"success": r.Success, "message": r.Message})
		}
		output.Printfln("%s", r.Message)
		return nil
	case *catalogops.BillingUserBalanceResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"balance": r.Balance})
		}
		output.Printfln("User balance")
		return nil
	case *catalogops.BillingOverviewResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"quota_plans": r.QuotaPlans, "price_lines": r.PriceLines, "pricing_plans": r.PricingPlans, "periods": r.Periods})
		}
		output.Printfln("Quota plans: %d, price lines: %d, pricing plans: %d, periods: %d", r.QuotaPlans, r.PriceLines, r.PricingPlans, r.Periods)
		return nil

	case *admin.Credit:
		return renderBillingSingle(c, output, r)
	case *admin.PriceLine:
		return renderBillingSingle(c, output, r)
	case *admin.PriceLineDetailResponse:
		return renderBillingSingle(c, output, r)
	case *admin.PricingPlan:
		return renderBillingSingle(c, output, r)
	case *admin.PricingPlanPeriod:
		return renderBillingSingle(c, output, r)
	case *admin.Subscriber:
		return renderBillingSingle(c, output, r)
	case *admin.ManagementResult:
		return renderBillingSingle(c, output, r)
	case *admin.PlanChangeResult:
		return renderBillingSingle(c, output, r)

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// yesNo renders a bool as "yes"/"no" for tables.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// renderJSONOrCount renders a JSON payload in JSON mode, or a count line plus
// the fetched entities in human mode (the legacy billing CLI rendered the full
// entity fields, not just a count).
func renderJSONOrCount(c *cli.Command, output Output, payload map[string]any, noun string, count int) error {
	if output.IsJSON() {
		return output.PrintJSON(payload)
	}
	output.Printfln("Found %d %s", count, noun)
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	output.Printfln("%s", string(b))
	return nil
}

// renderBillingSingle renders a single billing object as JSON in JSON mode, or
// pretty-printed JSON in human mode so the requested fields are always shown.
func renderBillingSingle(c *cli.Command, output Output, r any) error {
	if output.IsJSON() {
		return output.PrintJSON(r)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	output.Printfln("%s", string(b))
	return nil
}

// formatQuotaBytes renders a byte count in human-readable form.
func formatQuotaBytes(b int) string {
	return humanReadableSize(int64(b))
}
