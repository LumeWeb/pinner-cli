package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/pterm/pterm"
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
		SocialProviderAdminService: func(cfgMgr config.Manager) (coreadmin.SocialProviderAdminService, error) {
			if cfgMgr == nil {
				return nil, fmt.Errorf("no config manager available")
			}
			return coreadmin.DefaultSocialProviderAdminServiceFactory(cfgMgr), nil
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

// newAdminSocialProvidersCatalogCommand compiles the admin social-providers
// catalog operations and returns the `social-providers` command to mount under
// the `admin` parent: list, get, create, update, delete, enable, disable.
func newAdminSocialProvidersCatalogCommand() *cli.Command {
	return newAdminSectionCommand("admin_social_providers_", CmdSocialProviders, "Manage social login providers")
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

		// The platform-domain ops key records by a numeric ID, but an operator may
		// supply the registered domain name (e.g. pinned.site) instead. Resolve a
		// non-numeric id to the numeric ID the API expects before execution.
		var deleteID string
		switch op.Name() {
		case catalogops.OpAdminPlatformDomainsDelete,
			catalogops.OpAdminPlatformDomainsUpdate,
			catalogops.OpAdminPlatformDomainsBind:
			if id := catalog.StrArg(input, "id", ""); id != "" {
				if op.Name() == catalogops.OpAdminPlatformDomainsDelete {
					deleteID = id
				}
				resolved, err := resolvePlatformDomainID(ctx, adminCatalogDepsVar, id)
				if err != nil {
					return err
				}
				input["id"] = resolved
			}
		}

		// Destructive gate: destructive admin ops require confirm=true. Other
		// destructive admin ops keep the --force gate. Admin platform-domains
		// delete is an explicit CLI action, so a human at a terminal confirms
		// interactively instead of passing --force; non-interactive contexts
		// (scripts, --json/agent) still require --force so nothing is ever deleted
		// without an explicit override.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			if !confirm {
				switch op.Name() {
				case catalogops.OpAdminPlatformDomainsDelete:
					interactive := !setupOutput(c).IsJSON() && isatty.IsTerminal(os.Stdin.Fd())
					ok, err := confirmPlatformDomainDelete(deleteID, interactive)
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("deletion aborted")
					}
					confirm = true
				default:
					return fmt.Errorf("%s: pass --force to confirm this destructive operation", op.Name())
				}
			}
			input["confirm"] = confirm
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

// confirmPlatformDomainDelete confirms an irreversible platform-domain deletion
// with a human operator. As a package-level var it can be swapped in tests to
// drive the interactive path deterministically.
var confirmPlatformDomainDelete = promptPlatformDomainDelete

// promptPlatformDomainDelete prompts a human operator to confirm an irreversible
// platform-domain deletion. When no interactive terminal is available (scripts,
// --json/agent runs) it returns an error directing the caller to --force, so
// nothing is deleted without an explicit override; otherwise it returns whether
// the operator accepted the prompt.
func promptPlatformDomainDelete(deleteID string, interactive bool) (bool, error) {
	if !interactive {
		return false, fmt.Errorf("%s: pass --force to confirm this destructive operation", catalogops.OpAdminPlatformDomainsDelete)
	}
	ok, err := pterm.DefaultInteractiveConfirm.
		WithDefaultValue(false).
		Show(fmt.Sprintf("Permanently delete platform domain %q?", deleteID))
	if err != nil {
		return false, err
	}
	return ok, nil
}

// resolvePlatformDomainID resolves a platform-domain identifier an operator may
// supply either as the numeric ID or as the registered domain name (e.g.
// pinned.site). Numeric identifiers pass through unchanged; a domain name is
// resolved by listing the registered platform domains and matching on Domain,
// so callers need not look up the numeric ID first. Mirrors resolveZoneID.
func resolvePlatformDomainID(ctx context.Context, deps catalogops.AdminDeps, idOrDomain string) (string, error) {
	if _, err := strconv.Atoi(idOrDomain); err == nil {
		return idOrDomain, nil
	}
	cfgMgr := deps.CfgMgr()
	if cfgMgr == nil {
		return "", fmt.Errorf("no config manager available")
	}
	svc, err := deps.PlatformDomainAdminService(cfgMgr)
	if err != nil {
		return "", fmt.Errorf("failed to resolve platform domain service: %w", err)
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return "", err
	}
	domains, _, err := svc.ListPlatformDomains(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up platform domain by name: %w", err)
	}
	for _, d := range domains {
		if d.Domain == idOrDomain {
			return fmt.Sprintf("%d", d.Id), nil
		}
	}
	return "", fmt.Errorf("platform domain not found for %q", idOrDomain)
}

// renderAdminResult renders an admin handler's typed result through the CLI
// Output formatter.
func renderAdminResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)
	if result != nil && isNilPointerResult(result) {
		return fmt.Errorf("%s returned no result", op.Name())
	}

	switch r := result.(type) {
	case catalogops.ListResult:
		return renderListResult(output, r)

	case *admin.PlatformDomain:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if op.Name() == catalogops.OpAdminPlatformDomainsUpdate {
			output.Printfln("Platform domain %s updated: enabled=%t", r.Domain, r.Enabled)
		} else {
			output.Printfln("Platform domain %s (ID %d)", r.Domain, r.Id)
		}
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

	case *catalogops.BillingUserDeletedCredits:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"user_id": r.UserID, "count": r.Count, "credits": r.Credits})
		}
		return renderCreditsTable(output, r.Credits, r.Count)

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
		return renderBillingFields(c, output, r)
	case *admin.PriceLine:
		return renderBillingFields(c, output, r)
	case *admin.PriceLineDetailResponse:
		return renderBillingFields(c, output, r)
	case *admin.PricingPlan:
		return renderBillingFields(c, output, r)
	case *admin.PricingPlanPeriod:
		return renderBillingFields(c, output, r)
	case *admin.Subscriber:
		return renderBillingFields(c, output, r)
	case *admin.ManagementResult:
		return renderBillingFields(c, output, r)
	case *admin.PlanChangeResult:
		return renderBillingFields(c, output, r)

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

// renderCountTable prints a labeled count line plus a typed table in human mode.
func renderCountTable(output Output, noun string, count int, headers []string, rows [][]string) error {
	if count == 0 {
		output.Printfln("No %s found", noun)
		return nil
	}
	output.Printfln("Found %d %s", count, noun)
	output.PrintTable(headers, rows)
	return nil
}

// renderCreditsTable renders a list of billing credits.
func renderCreditsTable(output Output, credits []*admin.CreditItem, count int) error {
	headers := []string{"ID", "USER", "AMOUNT", "TYPE", "DIRECTION"}
	rows := make([][]string, 0, len(credits))
	for _, c := range credits {
		rows = append(rows, []string{
			fmt.Sprintf("%s", c.Id), fmt.Sprintf("%d", c.UserId),
			fmt.Sprintf("%v", c.Amount), c.Type, c.Direction,
		})
	}
	return renderCountTable(output, "credit(s)", count, headers, rows)
}

// renderPriceLinesTable renders a list of price lines.
func renderPriceLinesTable(output Output, lines []*admin.PriceLine, count int) error {
	headers := []string{"ID", "NAME", "ACTIVE", "DEFAULT"}
	rows := make([][]string, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, []string{
			fmt.Sprintf("%d", l.Id), l.Name,
			yesNo(l.IsActive), yesNo(l.IsDefault),
		})
	}
	return renderCountTable(output, "price line(s)", count, headers, rows)
}

// renderPricingPlansTable renders a list of pricing plans.
func renderPricingPlansTable(output Output, plans []*admin.PricingPlanItem, count int) error {
	headers := []string{"ID", "NAME", "CURRENCY", "ACTIVE", "POSITION"}
	rows := make([][]string, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), p.Name, p.Currency,
			yesNo(p.IsActive), fmt.Sprintf("%d", p.Position),
		})
	}
	return renderCountTable(output, "pricing plan(s)", count, headers, rows)
}

// renderPricingPlanPeriodsTable renders a list of pricing plan periods.
func renderPricingPlanPeriodsTable(output Output, periods []*admin.PricingPlanPeriod, count int) error {
	headers := []string{"ID", "PLAN", "CADENCE", "PRICE USD", "ROLLING DAYS", "ACTIVE"}
	rows := make([][]string, 0, len(periods))
	for _, p := range periods {
		rolling := "-"
		if p.RollingDays != nil {
			rolling = fmt.Sprintf("%d", *p.RollingDays)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), fmt.Sprintf("%d", p.PricingPlanId),
			p.Cadence, fmt.Sprintf("%.2f", p.PriceUsd), rolling, yesNo(p.IsActive),
		})
	}
	return renderCountTable(output, "pricing plan period(s)", count, headers, rows)
}

// renderSubscribersTable renders a list of subscribers.
func renderSubscribersTable(output Output, subs []*admin.Subscriber, count int) error {
	headers := []string{"ID", "USER", "GATEWAY", "STATUS", "ACTIVE"}
	rows := make([][]string, 0, len(subs))
	for _, s := range subs {
		status := "active"
		if s.PausedAt != nil {
			status = "paused"
		}
		if s.CancelledAt != nil {
			status = "cancelled"
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", s.Id), fmt.Sprintf("%d", s.UserId),
			s.GatewayType, status, yesNo(s.IsActive),
		})
	}
	return renderCountTable(output, "subscriber(s)", count, headers, rows)
}

// renderBillingFields renders a single billing object as a labeled field group
// in human mode, or as JSON in JSON mode.
func renderBillingFields(c *cli.Command, output Output, r any) error {
	if output.IsJSON() {
		return output.PrintJSON(r)
	}
	switch v := r.(type) {
	case *admin.Credit:
		output.PrintFields(FieldGroup{Title: "Credit", Fields: []Field{
			{"ID", fmt.Sprintf("%s", v.Id)},
			{"User", fmt.Sprintf("%d", v.UserId)},
			{"Amount", fmt.Sprintf("%v", v.Amount)},
			{"Type", v.Type},
			{"Direction", v.Direction},
		}})
		return nil
	case *admin.PriceLine:
		output.PrintFields(FieldGroup{Title: "Price line", Fields: []Field{
			{"ID", fmt.Sprintf("%d", v.Id)},
			{"Name", v.Name},
			{"Description", v.Description},
			{"Active", yesNo(v.IsActive)},
			{"Default", yesNo(v.IsDefault)},
		}})
		return nil
	case *admin.PriceLineDetailResponse:
		fields := []Field{
			{"ID", fmt.Sprintf("%d", v.Id)},
			{"Name", v.Name},
			{"Description", v.Description},
			{"Active", yesNo(v.IsActive)},
			{"Default", yesNo(v.IsDefault)},
		}
		if len(v.Plans) > 0 {
			fields = append(fields, Field{"Plans", fmt.Sprintf("%d", len(v.Plans))})
		}
		output.PrintFields(FieldGroup{Title: "Price line", Fields: fields})
		return nil
	case *admin.PricingPlan:
		output.PrintFields(FieldGroup{Title: "Pricing plan", Fields: []Field{
			{"ID", fmt.Sprintf("%d", v.Id)},
			{"Name", v.Name},
			{"Currency", v.Currency},
			{"Active", yesNo(v.IsActive)},
			{"Public", yesNo(v.IsPublic)},
		}})
		return nil
	case *admin.PricingPlanPeriod:
		output.PrintFields(FieldGroup{Title: "Pricing plan period", Fields: []Field{
			{"ID", fmt.Sprintf("%d", v.Id)},
			{"Plan", fmt.Sprintf("%d", v.PricingPlanId)},
			{"Cadence", v.Cadence},
			{"Price USD", fmt.Sprintf("%.2f", v.PriceUsd)},
			{"Active", yesNo(v.IsActive)},
		}})
		return nil
	case *admin.Subscriber:
		output.PrintFields(FieldGroup{Title: "Subscriber", Fields: []Field{
			{"ID", fmt.Sprintf("%d", v.Id)},
			{"User", fmt.Sprintf("%d", v.UserId)},
			{"Gateway", v.GatewayType},
			{"Active", yesNo(v.IsActive)},
		}})
		return nil
	case *admin.ManagementResult:
		output.Printfln("%s", managementResultText(v))
		return nil
	case *admin.PlanChangeResult:
		output.Printfln("%s", planChangeResultText(v))
		return nil
	default:
		return fmt.Errorf("catalog command rendered an unhandled billing result type %T", r)
	}
}

// managementResultText renders a management action result as a human line.
func managementResultText(v *admin.ManagementResult) string {
	msg := v.Status
	if v.Action != "" {
		msg = v.Action + ": " + v.Status
	}
	if v.ErrorMessage != nil && *v.ErrorMessage != "" {
		msg += " (" + *v.ErrorMessage + ")"
	}
	return msg
}

// planChangeResultText renders a plan-change result as a human line.
func planChangeResultText(v *admin.PlanChangeResult) string {
	msg := v.Action
	if v.ChargeDue.IsZero() {
		return msg
	}
	return fmt.Sprintf("%s (charge due %v)", msg, v.ChargeDue)
}

// formatQuotaBytes renders a byte count in human-readable form.
func formatQuotaBytes(b int) string {
	return humanReadableSize(int64(b))
}
