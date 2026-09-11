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
	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/clicatalog"
	"go.lumeweb.com/pinner/catalogops"
	coreadmin "go.lumeweb.com/pinner/core/admin"
	"go.lumeweb.com/pinner/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// catalog_admin_wiring.go adapts the admin domain operations in
// internal/catalogops to the urfave CLI: it compiles catalog operations into
// commands under the "admin" parent, renders each handler's result through the
// Output formatter, and maps positionals and the destructive --force gate onto
// operation inputs. IO and CLI concerns live here, not in catalogops.
//
// The admin command tree (sections: quota, billing, websites, platform-domains,
// social-providers; plus the quota/billing group parents: plans, allowances,
// user-configs / credits, price-lines, pricing-plans, pricing-plan-periods,
// subscribers) is declared in internal/clicatalog/shapes_admin.go (AdminShapes
// + AdminDomainRoot) and materialized by CompileCommandTree through the shared
// buildCLIParent parent builder and mount-owned buildAdminLeaf. No
// string-prefix section/group inference remains here. The only hand-written
// admin command is `pprof`
// (admin_pprof.go), merged in by newAdminCommand because it is not
// catalog-backed.

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

// newAdminCatalogSections compiles ALL admin catalog operations into the admin
// section parent commands under the `admin` root using the shape model in
// internal/clicatalog/shapes_admin.go. The sections (quota, billing, websites,
// platform-domains, social-providers) and the quota/billing group parents
// (plans, allowances, user-configs / credits, price-lines, pricing-plans,
// pricing-plan-periods, subscribers) are declared in the shape registry +
// AdminDomainRoot; no string-prefix grouping remains here. CompileCommandTree
// returns the root's ordered section children; the mount merges in the
// hand-written `pprof` section afterwards.
func newAdminCatalogSections() []*cli.Command {
	root := clicatalog.AdminDomainRoot
	ops := catalogops.AdminOperations(adminCatalogDepsVar)

	cmds, err := clicatalog.CompileCommandTree(
		ops,
		clicatalog.AdminShapes,
		root,
		adminCatalogConfig(),
		buildAdminLeaf,
		buildCLIParent,
	)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip an admin section.
		panic(fmt.Sprintf("catalog compile admin: %v", err))
	}
	return cmds
}

// adminSectionByName returns the compiled admin section parent with the given
// name (the same names the DomainRoot.Sections use). It is the SINGLE
// extraction seam for a named admin section — the per-section builder entry
// points (newAdminQuotaCommand, newAdminBillingCommand, etc.) and tests alike
// delegate to it, so there is exactly one deterministic compile and no
// per-section wrapper duplication.
func adminSectionByName(name string) *cli.Command {
	for _, c := range newAdminCatalogSections() {
		if c.Name == name {
			return c
		}
	}
	// Unreachable for a valid AdminDomainRoot; fail loudly in tests and during
	// command construction rather than mounting a nil parent.
	panic(fmt.Sprintf("admin catalog compile did not emit required section %q", name))
}

// buildAdminLeaf is the mount-owned leaf builder materializing one admin leaf
// into an urfave *cli.Command. Shape (name/category/aliases/flags/usage) comes
// from the clicatalog model via NewCLILeaf; behavior (the catalog action
// adapter + relaxFlagRequired) stays mount-owned here. The admin section/group
// parents (Category Admin) are materialized by the shared buildCLIParent.
func buildAdminLeaf(loc clicatalog.LeafLocator, cfg any) (*cli.Command, error) {
	base, err := clicatalog.NewCLILeaf(loc.Op, loc.Name, loc.Category, loc.Aliases)
	if err != nil {
		return nil, err
	}
	relaxFlagRequired(base)
	base.Action = catalogActionAdapter(loc.Op, cfg.(CatalogAdapterConfig))
	return base, nil
}

// adminCatalogConfig returns the CatalogAdapterConfig that expresses the admin
// domain's exact per-invocation behavior on top of the shared
// catalogActionAdapter pipeline. It mirrors the former per-domain admin
// adapter faithfully: the result renderer, the canonical positional
// mapping (MapPositionalArgs, the shared default), the platform-domain /
// social-provider symbolic-id resolution, the per-op destructive gate
// (interactive pterm prompt for platform-domains delete, --force error for
// every other destructive admin op), and normalize/timeout/execute/render.
// Admin does NOT honor the global --auth-token override (services read the
// live config manager's token), so HonorAuthTokenOverride stays false (the
// pipeline default). All remaining fields stay nil so the shared pipeline's
// safe defaults apply.
func adminCatalogConfig() CatalogAdapterConfig {
	return CatalogAdapterConfig{
		Renderer: renderAdminResult,

		// Symbolic-id resolution. The platform-domain ops key records by a
		// numeric ID, but an operator may supply the registered domain name
		// (e.g. pinned.site) instead; the social-provider ops are keyed by
		// numeric ID but the provider key (e.g. google) is the natural handle.
		// Numeric values pass through unchanged; a non-numeric value is
		// resolved to the numeric ID the API expects. The raw (unresolved) id
		// for platform-domains delete is stashed in Attribs so the
		// DestructiveGate prompt can address the operator by what they typed.
		ResolveIDs: func(ic *CatalogInvokeContext) error {
			switch ic.Op.Name() {
			case catalogops.OpAdminPlatformDomainsDelete,
				catalogops.OpAdminPlatformDomainsUpdate,
				catalogops.OpAdminPlatformDomainsBind:
				if id := opmesh.StrArg(ic.Input, "id", ""); id != "" {
					if ic.Op.Name() == catalogops.OpAdminPlatformDomainsDelete {
						ic.Attribs["deleteID"] = id
					}
					resolved, err := resolvePlatformDomainID(ic.Ctx, adminCatalogDepsVar, id)
					if err != nil {
						return err
					}
					ic.Input["id"] = resolved
				}
			case catalogops.OpAdminSocialProvidersGet,
				catalogops.OpAdminSocialProvidersUpdate,
				catalogops.OpAdminSocialProvidersDelete,
				catalogops.OpAdminSocialProvidersEnable,
				catalogops.OpAdminSocialProvidersDisable:
				if id := opmesh.StrArg(ic.Input, "id", ""); id != "" {
					resolved, err := resolveSocialProviderID(ic.Ctx, adminCatalogDepsVar, id)
					if err != nil {
						return err
					}
					ic.Input["id"] = resolved
				}
			}
			return nil
		},

		// Destructive gate: destructive admin ops require confirm=true. Most
		// keep the --force gate; admin platform-domains delete is an explicit
		// CLI action, so a human at a terminal confirms interactively instead of
		// passing --force, while non-interactive contexts (scripts, --json/agent)
		// still require --force so nothing is ever deleted without an explicit
		// override.
		DestructiveGate: func(ic *CatalogInvokeContext) (bool, error) {
			if ic.Op.Name() == catalogops.OpAdminPlatformDomainsDelete {
				return GateInteractivePrompt(
					func(ic *CatalogInvokeContext) (bool, error) {
						interactive := !ic.Output.IsJSON() && isatty.IsTerminal(os.Stdin.Fd())
						rawID, _ := ic.Attribs["deleteID"].(string)
						return confirmPlatformDomainDelete(rawID, interactive)
					},
					func(*CatalogInvokeContext) string {
						return "deletion aborted"
					},
				)(ic)
			}
			return GateForceReject(
				func(*CatalogInvokeContext) bool { return true },
				func(ic *CatalogInvokeContext) string {
					return ic.Op.Name() + ": pass --force to confirm this destructive operation"
				},
			)(ic)
		},
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

// resolveSocialProviderID resolves a social-provider identifier an operator may
// supply either as the numeric record ID or as the provider key (e.g. google).
// Numeric identifiers pass through unchanged; a key is resolved by listing the
// configured providers and matching on ProviderId. Mirrors resolvePlatformDomainID.
func resolveSocialProviderID(ctx context.Context, deps catalogops.AdminDeps, idOrKey string) (string, error) {
	if _, err := strconv.Atoi(idOrKey); err == nil {
		return idOrKey, nil
	}
	if deps.CfgMgr == nil || deps.SocialProviderAdminService == nil {
		return "", fmt.Errorf("social provider service unavailable (not wired)")
	}
	cfgMgr := deps.CfgMgr()
	svc, err := deps.SocialProviderAdminService(cfgMgr)
	if err != nil {
		return "", fmt.Errorf("failed to resolve social provider service: %w", err)
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return "", err
	}
	providers, _, err := svc.ListSocialProviders(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to look up social provider by key: %w", err)
	}
	for _, p := range providers {
		if p.ProviderId == idOrKey {
			return fmt.Sprintf("%d", p.Id), nil
		}
	}
	return "", fmt.Errorf("social provider %q not found; run 'pinner admin social-providers list' to see configured providers", idOrKey)
}

// renderAdminResult renders an admin handler's typed result through the CLI
// Output formatter.
func renderAdminResult(_ context.Context, c *cli.Command, op opmesh.Operation, result any) error {
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

	case *catalogops.SocialProvidersDeleteResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted, "id": r.ID})
		}
		output.Printfln("Social provider %s deleted", r.ID)
		return nil

	case *admin.SocialProvider:
		// create/get/update/enable/disable all return the provider object;
		// client secrets are never present on this response.
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{Title: "Social provider", Fields: []Field{
			{"ID", fmt.Sprintf("%d", r.Id)},
			{"Provider", r.ProviderId},
			{"Display name", r.DisplayName},
			{"Enabled", yesNo(r.Enabled)},
			{"Order", fmt.Sprintf("%d", r.OrderIndex)},
			{"Client ID", r.ClientId},
			{"Auth URL", r.AuthUrl},
			{"Token URL", r.TokenUrl},
			{"User URL", r.UserUrl},
			{"Scopes", strings.Join(r.Scopes, ", ")},
			{"User ID key", r.UserIdKey},
			{"User email key", r.UserEmailKey},
			{"User name key", r.UserNameKey},
		}})
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
