package catalogops

import (
	"fmt"

	admin "go.lumeweb.com/portal-sdk/admin"
)

// The admin list row builders centralize the human table view (headers + rows)
// for each admin *-list operation so the shared ListResult renderer can handle
// them all identically. JSON always carries {count, items}; these helpers only
// supply the CLI table.

func adminYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func adminBytes(b int) string {
	return humanBytes(int64(b))
}

func humanBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func quotaPlansListResult(plans []*admin.QuotaPlan) ListResult {
	headers := []string{"ID", "NAME", "UPLOAD", "DOWNLOAD", "STORAGE", "ACTIVE", "DEFAULT"}
	rows := make([][]string, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), p.Name,
			adminBytes(p.UploadLimitBytes), adminBytes(p.DownloadLimitBytes), adminBytes(p.StorageLimitBytes),
			fmt.Sprintf("%t", p.IsActive), adminYesNo(p.IsDefault),
		})
	}
	return NewListResult(plans, ListResultMeta{Noun: "quota plan(s)", Headers: headers, Rows: rows})
}

func quotaAllowancesListResult(allows []*admin.QuotaAllowance) ListResult {
	headers := []string{"ID", "USER", "SOURCE", "TYPE", "BYTES", "ACTIVE"}
	rows := make([][]string, 0, len(allows))
	for _, a := range allows {
		rows = append(rows, []string{
			fmt.Sprintf("%d", a.Id), fmt.Sprintf("%d", a.UserId), string(a.Source),
			string(a.Type), adminBytes(a.Bytes), adminYesNo(a.IsActive),
		})
	}
	return NewListResult(allows, ListResultMeta{Noun: "quota allowance(s)", Headers: headers, Rows: rows})
}

func quotaUserConfigsListResult(configs []*admin.UserQuotaConfig) ListResult {
	headers := []string{"USER", "PLAN"}
	rows := make([][]string, 0, len(configs))
	for _, c := range configs {
		plan := "-"
		if c.QuotaPlanId != nil {
			plan = fmt.Sprintf("%d", *c.QuotaPlanId)
		}
		rows = append(rows, []string{fmt.Sprintf("%d", c.UserId), plan})
	}
	return NewListResult(configs, ListResultMeta{Noun: "user quota config(s)", Headers: headers, Rows: rows})
}

func billingCreditsListResult(credits []*admin.CreditItem) ListResult {
	headers := []string{"ID", "USER", "AMOUNT", "TYPE", "DIRECTION"}
	rows := make([][]string, 0, len(credits))
	for _, c := range credits {
		rows = append(rows, []string{
			fmt.Sprintf("%s", c.Id), fmt.Sprintf("%d", c.UserId),
			fmt.Sprintf("%v", c.Amount), c.Type, c.Direction,
		})
	}
	return NewListResult(credits, ListResultMeta{Noun: "credit(s)", Headers: headers, Rows: rows})
}

func billingPriceLinesListResult(lines []*admin.PriceLine) ListResult {
	headers := []string{"ID", "NAME", "ACTIVE", "DEFAULT"}
	rows := make([][]string, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, []string{
			fmt.Sprintf("%d", l.Id), l.Name, adminYesNo(l.IsActive), adminYesNo(l.IsDefault),
		})
	}
	return NewListResult(lines, ListResultMeta{Noun: "price line(s)", Headers: headers, Rows: rows})
}

func billingPricingPlansListResult(plans []*admin.PricingPlanItem) ListResult {
	headers := []string{"ID", "NAME", "CURRENCY", "ACTIVE", "POSITION"}
	rows := make([][]string, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), p.Name, p.Currency,
			adminYesNo(p.IsActive), fmt.Sprintf("%d", p.Position),
		})
	}
	return NewListResult(plans, ListResultMeta{Noun: "pricing plan(s)", Headers: headers, Rows: rows})
}

func billingPricingPlanPeriodsListResult(periods []*admin.PricingPlanPeriod) ListResult {
	headers := []string{"ID", "PLAN", "CADENCE", "PRICE USD", "ROLLING DAYS", "ACTIVE"}
	rows := make([][]string, 0, len(periods))
	for _, p := range periods {
		rolling := "-"
		if p.RollingDays != nil {
			rolling = fmt.Sprintf("%d", *p.RollingDays)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", p.Id), fmt.Sprintf("%d", p.PricingPlanId),
			p.Cadence, fmt.Sprintf("%.2f", p.PriceUsd), rolling, adminYesNo(p.IsActive),
		})
	}
	return NewListResult(periods, ListResultMeta{Noun: "pricing plan period(s)", Headers: headers, Rows: rows})
}

func billingSubscribersListResult(subs []*admin.Subscriber) ListResult {
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
			s.GatewayType, status, adminYesNo(s.IsActive),
		})
	}
	return NewListResult(subs, ListResultMeta{Noun: "subscriber(s)", Headers: headers, Rows: rows})
}

func platformDomainsListResult(domains []*admin.PlatformDomain) ListResult {
	headers := []string{"ID", "DOMAIN", "NAMESPACE", "ZONE", "ENABLED"}
	rows := make([][]string, 0, len(domains))
	for _, d := range domains {
		rows = append(rows, []string{
			fmt.Sprintf("%d", d.Id), d.Domain, d.Namespace,
			fmt.Sprintf("%d", d.ZoneId), adminYesNo(d.Enabled),
		})
	}
	return NewListResult(domains, ListResultMeta{Noun: "platform domain(s)", Headers: headers, Rows: rows})
}
