package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"go.lumeweb.com/portal-sdk/admin"
)

var quotaAdminServiceFactory QuotaAdminServiceFactory = defaultQuotaAdminServiceFactory

// quotaPlansListCmdGetter interface for quota plans list command
type quotaPlansListCmdGetter interface {
}

// quotaPlansListAction lists all quota plans
func quotaPlansListAction(ctx context.Context, cmd quotaPlansListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	plans, total, err := quotaService.ListPlans(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"count": total,
			"plans": plans,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d quota plan(s)", total)

	if len(plans) == 0 {
		return nil
	}

	headers := []string{"ID", "NAME", "UPLOAD", "DOWNLOAD", "STORAGE", "DEFAULT"}
	rows := make([][]string, len(plans))
	for i, plan := range plans {
		isDefault := ""
		if plan.IsDefault {
			isDefault = "*"
		}
		rows[i] = []string{
			fmt.Sprintf("%d", plan.Id),
			plan.Name,
			formatBytes(plan.UploadLimitBytes),
			formatBytes(plan.DownloadLimitBytes),
			formatBytes(plan.StorageLimitBytes),
			isDefault,
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaPlansGetCmdGetter interface for quota plans get command
type quotaPlansGetCmdGetter interface {
	Args() cli.Args
}

// quotaPlansGetAction gets a quota plan by ID
func quotaPlansGetAction(ctx context.Context, cmd quotaPlansGetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("plan ID is required")
	}
	planID := args.First()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	plan, err := quotaService.GetPlan(ctx, planID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(plan)
	}

	output.Printf("Quota Plan Details")

	headers := []string{"ID", "NAME", "DESCRIPTION", "UPLOAD", "DOWNLOAD", "STORAGE", "DEFAULT", "CREATED", "UPDATED"}
	isDefault := "No"
	if plan.IsDefault {
		isDefault = "Yes"
	}
	rows := [][]string{
		{
			fmt.Sprintf("%d", plan.Id),
			plan.Name,
			plan.Description,
			formatBytes(plan.UploadLimitBytes),
			formatBytes(plan.DownloadLimitBytes),
			formatBytes(plan.StorageLimitBytes),
			isDefault,
			plan.CreatedAt.Format("2006-01-02 15:04:05"),
			plan.UpdatedAt.Format("2006-01-02 15:04:05"),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaPlansCreateCmdGetter interface for quota plans create command
type quotaPlansCreateCmdGetter interface {
	String(string) string
	Int(string) int
	Bool(string) bool
	IsSet(string) bool
}

// quotaPlansCreateAction creates a new quota plan
func quotaPlansCreateAction(ctx context.Context, cmd quotaPlansCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	if err := requireUpdateFields(cmd, FlagName); err != nil {
		return err
	}

	limits := admin.QuotaLimits{
		UploadLimitBytes:   cmd.Int(FlagUploadLimit),
		DownloadLimitBytes: cmd.Int(FlagDownloadLimit),
		StorageLimitBytes:  cmd.Int(FlagStorageLimit),
		WindowType:         cmd.String(FlagWindowType),
	}

	plan := admin.NewQuotaPlan(
		cmd.String(FlagName),
		cmd.String(FlagDescription),
		limits,
	)
	plan.IsActive = cmd.Bool(FlagIsActive)

	created, err := quotaService.CreatePlan(ctx, plan)
	if err != nil {
		return err
	}

	if cmd.Bool(FlagIsDefault) {
		if err := quotaService.SetDefaultPlan(ctx, fmt.Sprintf("%d", created.Id)); err != nil {
			return fmt.Errorf("plan created but failed to set as default: %w", err)
		}
		created.IsDefault = true
	}

	if output.IsJSON() {
		return output.PrintJSON(created)
	}

	output.Printf("Quota plan created successfully")

	headers := []string{"ID", "NAME", "DESCRIPTION", "UPLOAD", "DOWNLOAD", "STORAGE", "ACTIVE", "DEFAULT"}
	isDefault := "No"
	if created.IsDefault {
		isDefault = "Yes"
	}
	rows := [][]string{
		{
			fmt.Sprintf("%d", created.Id),
			created.Name,
			created.Description,
			formatBytes(created.UploadLimitBytes),
			formatBytes(created.DownloadLimitBytes),
			formatBytes(created.StorageLimitBytes),
			fmt.Sprintf("%t", created.IsActive),
			isDefault,
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaPlansUpdateCmdGetter interface for quota plans update command
type quotaPlansUpdateCmdGetter interface {
	Args() cli.Args
	String(string) string
	Int(string) int
	Bool(string) bool
	IsSet(string) bool
}

// quotaPlansUpdateAction updates a quota plan
func quotaPlansUpdateAction(ctx context.Context, cmd quotaPlansUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("plan ID is required")
	}

	planID := args.First()

	if err := requireUpdateFields(cmd, FlagName, FlagDescription, FlagUploadLimit, FlagDownloadLimit, FlagStorageLimit, FlagWindowType, FlagIsActive, FlagIsDefault); err != nil {
		return err
	}

	// Get existing plan first
	existing, err := quotaService.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get existing plan: %w", err)
	}

	// Start with existing values
	limits := admin.QuotaLimits{
		UploadLimitBytes:   existing.UploadLimitBytes,
		DownloadLimitBytes: existing.DownloadLimitBytes,
		StorageLimitBytes:  existing.StorageLimitBytes,
		WindowType:         existing.WindowType,
		WindowDuration:     existing.WindowDuration,
		WindowStartHour:    existing.WindowStartHour,
		WindowTimezone:     existing.WindowTimezone,
	}

	// Override with provided values
	if cmd.IsSet(FlagUploadLimit) {
		limits.UploadLimitBytes = cmd.Int(FlagUploadLimit)
	}
	if cmd.IsSet(FlagDownloadLimit) {
		limits.DownloadLimitBytes = cmd.Int(FlagDownloadLimit)
	}
	if cmd.IsSet(FlagStorageLimit) {
		limits.StorageLimitBytes = cmd.Int(FlagStorageLimit)
	}
	if cmd.IsSet(FlagWindowType) {
		limits.WindowType = cmd.String(FlagWindowType)
	}

	name := existing.Name
	if cmd.IsSet(FlagName) {
		name = cmd.String(FlagName)
	}
	description := existing.Description
	if cmd.IsSet(FlagDescription) {
		description = cmd.String(FlagDescription)
	}

	plan := admin.NewQuotaPlan(name, description, limits)
	plan.IsActive = existing.IsActive

	if cmd.IsSet(FlagIsActive) {
		plan.IsActive = cmd.Bool(FlagIsActive)
	}

	updated, err := quotaService.UpdatePlan(ctx, planID, plan)
	if err != nil {
		return err
	}

	if cmd.IsSet(FlagIsDefault) && cmd.Bool(FlagIsDefault) {
		if err := quotaService.SetDefaultPlan(ctx, planID); err != nil {
			return fmt.Errorf("plan updated but failed to set as default: %w", err)
		}
		updated.IsDefault = true
	}

	if output.IsJSON() {
		return output.PrintJSON(updated)
	}

	output.Printf("Quota plan updated successfully")

	headers := []string{"ID", "NAME", "DESCRIPTION", "UPLOAD", "DOWNLOAD", "STORAGE", "ACTIVE", "DEFAULT"}
	isDefault := "No"
	if updated.IsDefault {
		isDefault = "Yes"
	}
	rows := [][]string{
		{
			fmt.Sprintf("%d", updated.Id),
			updated.Name,
			updated.Description,
			formatBytes(updated.UploadLimitBytes),
			formatBytes(updated.DownloadLimitBytes),
			formatBytes(updated.StorageLimitBytes),
			fmt.Sprintf("%t", updated.IsActive),
			isDefault,
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaPlansDeleteCmdGetter interface for quota plans delete command
type quotaPlansDeleteCmdGetter interface {
	Args() cli.Args
}

// quotaPlansDeleteAction deletes a quota plan
func quotaPlansDeleteAction(ctx context.Context, cmd quotaPlansDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("plan ID is required")
	}
	planID := args.First()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	if err := quotaService.DeletePlan(ctx, planID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"message": fmt.Sprintf("Plan %s deleted successfully", planID),
		})
	}

	output.Printf("Plan %s deleted successfully", planID)

	return nil
}

// quotaPlansSetDefaultCmdGetter interface for quota plans set-default command
type quotaPlansSetDefaultCmdGetter interface {
	Args() cli.Args
}

// quotaPlansSetDefaultAction sets a quota plan as default
func quotaPlansSetDefaultAction(ctx context.Context, cmd quotaPlansSetDefaultCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("plan ID is required")
	}
	planID := args.First()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	if err := quotaService.SetDefaultPlan(ctx, planID); err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			return fmt.Errorf("plan %s not found (ensure the plan is active with: pinner admin quota plans update %s --is-active)", planID, planID)
		}
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"message": fmt.Sprintf("Plan %s set as default", planID),
		})
	}

	output.Printf("Plan %s set as default", planID)

	return nil
}

// quotaAllowancesListCmdGetter interface for quota allowances list command
type quotaAllowancesListCmdGetter interface {
}

// quotaAllowancesListAction lists all quota allowances
func quotaAllowancesListAction(ctx context.Context, cmd quotaAllowancesListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	allowances, total, err := quotaService.ListAllowances(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"count":      total,
			"allowances": allowances,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d quota allowance(s)", total)

	if len(allowances) == 0 {
		return nil
	}

	headers := []string{"ID", "USER ID", "SOURCE", "TYPE", "BYTES", "REMAINING", "USED", "EXPIRY"}
	rows := make([][]string, len(allowances))
	for i, a := range allowances {
		expiry := "Never"
		if !a.ExpiryDate.IsZero() {
			expiry = a.ExpiryDate.Format("2006-01-02")
		}
		rows[i] = []string{
			fmt.Sprintf("%d", a.Id),
			fmt.Sprintf("%d", a.UserId),
			a.Source,
			a.Type,
			formatBytes(a.Bytes),
			formatBytes(a.BytesRemaining),
			formatBytes(a.BytesUsed),
			expiry,
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaAllowancesCreateCmdGetter interface for quota allowances create command
type quotaAllowancesCreateCmdGetter interface {
	Int(string) int
	String(string) string
	IsSet(string) bool
}

// quotaAllowancesCreateAction creates a quota allowance
func quotaAllowancesCreateAction(ctx context.Context, cmd quotaAllowancesCreateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	userID, err := requireSetInt(cmd, FlagUserID)
	if err != nil {
		return err
	}

	var expiryDate time.Time
	if cmd.IsSet(FlagExpiry) {
		days := cmd.Int(FlagExpiry)
		expiryDate = time.Now().AddDate(0, 0, days)
	}

	bytes := cmd.Int(FlagUploadLimit)
	bytesRemaining := cmd.Int(FlagUploadLimit)

	created, err := quotaService.CreateAllowance(
		ctx,
		userID,
		cmd.String(FlagSource),
		cmd.String(FlagQuotaType),
		bytes,
		bytesRemaining,
		0,
		expiryDate,
	)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(created)
	}

	output.Printf("Quota allowance created successfully")

	headers := []string{"ID", "USER ID", "SOURCE", "TYPE", "BYTES", "REMAINING"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", created.Id),
			fmt.Sprintf("%d", created.UserId),
			created.Source,
			created.Type,
			formatBytes(created.Bytes),
			formatBytes(created.BytesRemaining),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaAllowancesUpdateCmdGetter interface for quota allowances update command
type quotaAllowancesUpdateCmdGetter interface {
	Args() cli.Args
	Int(string) int
	String(string) string
	IsSet(string) bool
}

// quotaAllowancesUpdateAction updates a quota allowance
func quotaAllowancesUpdateAction(ctx context.Context, cmd quotaAllowancesUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("grant ID is required")
	}

	grantID := args.First()

	if err := requireUpdateFields(cmd, FlagUserID, FlagSource, FlagQuotaType, FlagUploadLimit, FlagDownloadLimit, FlagExpiry); err != nil {
		return err
	}

	userID := 0
	if cmd.IsSet(FlagUserID) {
		userID = cmd.Int(FlagUserID)
	}

	var expiryDate time.Time
	if cmd.IsSet(FlagExpiry) {
		days := cmd.Int(FlagExpiry)
		expiryDate = time.Now().AddDate(0, 0, days)
	}

	source := ""
	if cmd.IsSet(FlagSource) {
		source = cmd.String(FlagSource)
	}

	allowanceType := ""
	if cmd.IsSet(FlagQuotaType) {
		allowanceType = cmd.String(FlagQuotaType)
	}

	bytes := 0
	if cmd.IsSet(FlagUploadLimit) {
		bytes = cmd.Int(FlagUploadLimit)
	}

	bytesRemaining := bytes
	if cmd.IsSet(FlagDownloadLimit) {
		bytesRemaining = cmd.Int(FlagDownloadLimit)
	}

	updated, err := quotaService.UpdateAllowance(
		ctx,
		grantID,
		userID,
		source,
		allowanceType,
		bytes,
		bytesRemaining,
		0,
		expiryDate,
	)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(updated)
	}

	output.Printf("Quota allowance updated successfully")

	headers := []string{"ID", "USER ID", "SOURCE", "TYPE", "BYTES", "REMAINING"}
	rows := [][]string{
		{
			fmt.Sprintf("%d", updated.Id),
			fmt.Sprintf("%d", updated.UserId),
			updated.Source,
			updated.Type,
			formatBytes(updated.Bytes),
			formatBytes(updated.BytesRemaining),
		},
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaAllowancesDeleteCmdGetter interface for quota allowances delete command
type quotaAllowancesDeleteCmdGetter interface {
	Args() cli.Args
}

// quotaAllowancesDeleteAction deletes a quota allowance
func quotaAllowancesDeleteAction(ctx context.Context, cmd quotaAllowancesDeleteCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("grant ID is required")
	}
	grantID := args.First()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	if err := quotaService.DeleteAllowance(ctx, grantID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"message": fmt.Sprintf("Allowance %s deleted successfully", grantID),
		})
	}

	output.Printf("Allowance %s deleted successfully", grantID)

	return nil
}

// quotaStatsCmdGetter interface for quota stats command
type quotaStatsCmdGetter interface {
}

// quotaStatsAction gets quota system statistics
func quotaStatsAction(ctx context.Context, cmd quotaStatsCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	stats, err := quotaService.GetStats(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(stats)
	}

	output.Printf("Quota System Statistics")
	output.Printf("Total Users: %d", stats.TotalUsers)
	output.Printf("Active Users: %d", stats.ActiveUsers)
	output.Printf("Total Plans: %d", stats.TotalPlans)
	output.Printf("Active Plans: %d", stats.TotalActivePlans)
	output.Printf("Total Grants: %d", stats.TotalGrants)
	output.Printf("Active Grants: %d", stats.TotalActiveGrants)
	output.Printf("Total Usage Bytes: %d", stats.TotalUsageBytes)

	return nil
}

// quotaReconcileCmdGetter interface for quota reconcile command
type quotaReconcileCmdGetter interface {
	Int(string) int
	IsSet(string) bool
}

// quotaReconcileAction reconciles quota data
func quotaReconcileAction(ctx context.Context, cmd quotaReconcileCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	var userID *int
	if cmd.IsSet(FlagUserID) {
		id := cmd.Int(FlagUserID)
		userID = &id
	}

	message, affected, err := quotaService.Reconcile(ctx, userID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"message":  message,
			"affected": affected,
		})
	}

	output.Printf("Reconciliation complete: %s", message)
	output.Printf("Affected records: %d", affected)

	return nil
}

// quotaCleanupCmdGetter interface for quota cleanup command
type quotaCleanupCmdGetter interface {
	Int(string) int
}

// quotaCleanupAction cleans up expired quota data
func quotaCleanupAction(ctx context.Context, cmd quotaCleanupCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	retentionDays := cmd.Int(FlagRetentionDays)

	deleted, err := quotaService.Cleanup(ctx, retentionDays)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"deleted":        deleted,
			"retention_days": retentionDays,
		})
	}

	output.Printf("Cleanup complete: %d records deleted (retention: %d days)", deleted, retentionDays)

	return nil
}

// quotaUserConfigsListCmdGetter interface for quota user configs list command
type quotaUserConfigsListCmdGetter interface {
}

// quotaUserConfigsListAction lists all user quota configs
func quotaUserConfigsListAction(ctx context.Context, cmd quotaUserConfigsListCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	configs, total, err := quotaService.ListUserConfigs(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"count":   total,
			"configs": configs,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d user config(s)", total)

	if len(configs) == 0 {
		return nil
	}

	headers := []string{"ID", "USER ID", "PLAN ID", "UPLOAD", "DOWNLOAD", "STORAGE"}
	rows := make([][]string, len(configs))
	for i, c := range configs {
		planID := "-"
		if c.QuotaPlanId != nil {
			planID = fmt.Sprintf("%d", *c.QuotaPlanId)
		}
		upload := "-"
		if c.UploadLimitBytes != nil {
			upload = formatBytes(*c.UploadLimitBytes)
		}
		download := "-"
		if c.DownloadLimitBytes != nil {
			download = formatBytes(*c.DownloadLimitBytes)
		}
		storage := "-"
		if c.StorageLimitBytes != nil {
			storage = formatBytes(*c.StorageLimitBytes)
		}
		rows[i] = []string{
			fmt.Sprintf("%d", c.Id),
			fmt.Sprintf("%d", c.UserId),
			planID,
			upload,
			download,
			storage,
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

// quotaUserConfigsResetCmdGetter interface for quota user configs reset command
type quotaUserConfigsResetCmdGetter interface {
	Args() cli.Args
}

// quotaUserConfigsResetAction resets a user's quota config to default
func quotaUserConfigsResetAction(ctx context.Context, cmd quotaUserConfigsResetCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("user ID is required")
	}

	userIDStr := args.First()
	var userID int
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		return fmt.Errorf("invalid user ID: %s", userIDStr)
	}

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	if err := quotaService.ResetUserPlan(ctx, userID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"message": fmt.Sprintf("User %d config reset to default", userID),
		})
	}

	output.Printf("User %d config reset to default", userID)

	return nil
}

// quotaUserConfigsUpdateCmdGetter interface for quota user configs update command
type quotaUserConfigsUpdateCmdGetter interface {
	Int(name string) int
	String(name string) string
	IsSet(name string) bool
}

// quotaUserConfigsUpdateAction updates a user's quota config
func quotaUserConfigsUpdateAction(ctx context.Context, cmd quotaUserConfigsUpdateCmdGetter, output Output, cfgMgr config.Manager, serviceFactory QuotaAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	userID, err := requireSetInt(cmd, FlagUserID)
	if err != nil {
		return err
	}

	if err := requireUpdateFields(cmd, FlagPlanID, FlagEnforcementPolicy, FlagUploadLimit, FlagDownloadLimit, FlagStorageLimit, FlagUploadThreshold, FlagDownloadThreshold, FlagStorageThreshold, FlagWindowDuration, FlagWindowStartHour, FlagWindowTimezone, FlagWindowType); err != nil {
		return err
	}

	quotaService := serviceFactory(cfgMgr, output)

	if err := quotaService.RequireAuthenticated(); err != nil {
		return err
	}

	config := &admin.UserQuotaConfigUpdate{}

	if cmd.IsSet(FlagPlanID) {
		planID := cmd.Int(FlagPlanID)
		config.QuotaPlanId = &planID
	}
	if cmd.IsSet(FlagEnforcementPolicy) {
		policy := cmd.String(FlagEnforcementPolicy)
		config.EnforcementPolicy = &policy
	}
	if cmd.IsSet(FlagUploadLimit) {
		v := cmd.Int(FlagUploadLimit)
		config.UploadLimitBytes = &v
	}
	if cmd.IsSet(FlagDownloadLimit) {
		v := cmd.Int(FlagDownloadLimit)
		config.DownloadLimitBytes = &v
	}
	if cmd.IsSet(FlagStorageLimit) {
		v := cmd.Int(FlagStorageLimit)
		config.StorageLimitBytes = &v
	}
	if cmd.IsSet(FlagUploadThreshold) {
		v := cmd.Int(FlagUploadThreshold)
		config.UploadThreshold = &v
	}
	if cmd.IsSet(FlagDownloadThreshold) {
		v := cmd.Int(FlagDownloadThreshold)
		config.DownloadThreshold = &v
	}
	if cmd.IsSet(FlagStorageThreshold) {
		v := cmd.Int(FlagStorageThreshold)
		config.StorageThreshold = &v
	}
	if cmd.IsSet(FlagWindowDuration) {
		v := cmd.Int(FlagWindowDuration)
		config.WindowDuration = &v
	}
	if cmd.IsSet(FlagWindowStartHour) {
		v := cmd.Int(FlagWindowStartHour)
		config.WindowStartHour = &v
	}
	if cmd.IsSet(FlagWindowTimezone) {
		v := cmd.String(FlagWindowTimezone)
		config.WindowTimezone = &v
	}
	if cmd.IsSet(FlagWindowType) {
		v := cmd.String(FlagWindowType)
		config.WindowType = &v
	}

	result, err := quotaService.UpdateUserConfig(ctx, userID, config)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printf("User %d quota config updated", userID)
	return nil
}

// formatBytes formats bytes to human-readable string
func formatBytes(bytes int) string {
	if bytes < 0 {
		return "unlimited"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := int64(bytes) / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
