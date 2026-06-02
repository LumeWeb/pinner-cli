package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestNewAdminCommand(t *testing.T) {
	t.Run("creates admin command with correct configuration", func(t *testing.T) {
		cmd := newAdminCommand()

		assert.Equal(t, "admin", cmd.Name)
		assert.Equal(t, "Administrative operations", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.Contains(t, cmd.Description, "Administrative operations for quota management, billing, and profiling")
	})

	t.Run("has quota and billing subcommands", func(t *testing.T) {
		cmd := newAdminCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 4)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "quota")
		assert.Contains(t, subcommandNames, "billing")
		assert.Contains(t, subcommandNames, "websites")
		assert.Contains(t, subcommandNames, "pprof")
	})
}

func TestNewQuotaCommand(t *testing.T) {
	t.Run("creates quota command with correct configuration", func(t *testing.T) {
		cmd := newQuotaCommand()

		assert.Equal(t, "quota", cmd.Name)
		assert.Equal(t, "Quota management operations", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.Contains(t, cmd.Description, "Manage quota plans")
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newQuotaCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "plans")
		assert.Contains(t, subcommandNames, "allowances")
		assert.Contains(t, subcommandNames, "user-configs")
		assert.Contains(t, subcommandNames, "stats")
		assert.Contains(t, subcommandNames, "reconcile")
		assert.Contains(t, subcommandNames, "cleanup")
	})
}

func TestNewBillingCommand(t *testing.T) {
	t.Run("creates billing command with correct configuration", func(t *testing.T) {
		cmd := newBillingCommand()

		assert.Equal(t, "billing", cmd.Name)
		assert.Equal(t, "Billing management operations", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.Contains(t, cmd.Description, "Manage billing credits")
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newBillingCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "overview")
		assert.Contains(t, subcommandNames, "credits")
		assert.Contains(t, subcommandNames, "price-lines")
		assert.Contains(t, subcommandNames, "pricing-plans")
		assert.Contains(t, subcommandNames, "pricing-plan-periods")
		assert.Contains(t, subcommandNames, "subscribers")
	})
}

func TestNewQuotaPlansCommand(t *testing.T) {
	t.Run("creates quota plans command with correct configuration", func(t *testing.T) {
		cmd := newQuotaPlansCommand()

		assert.Equal(t, "plans", cmd.Name)
		assert.Equal(t, "Manage quota plans", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newQuotaPlansCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "get")
		assert.Contains(t, subcommandNames, "create")
		assert.Contains(t, subcommandNames, "update")
		assert.Contains(t, subcommandNames, "delete")
		assert.Contains(t, subcommandNames, "set-default")
	})

	t.Run("create command has correct flags", func(t *testing.T) {
		cmd := newQuotaPlansCommand()

		createCmd := findSubcommand(cmd.Commands, "create")
		require.NotNil(t, createCmd)
		require.NotNil(t, createCmd.Flags)
		assert.Len(t, createCmd.Flags, 8)

		flagNames := getFlagNames(createCmd.Flags)
		assert.Contains(t, flagNames, "name")
		assert.Contains(t, flagNames, "description")
		assert.Contains(t, flagNames, "upload-limit")
		assert.Contains(t, flagNames, "download-limit")
		assert.Contains(t, flagNames, "storage-limit")
		assert.Contains(t, flagNames, "window-type")
		assert.Contains(t, flagNames, "is-active")
		assert.Contains(t, flagNames, "is-default")
	})
}

func TestNewQuotaAllowancesCommand(t *testing.T) {
	t.Run("creates quota allowances command with correct configuration", func(t *testing.T) {
		cmd := newQuotaAllowancesCommand()

		assert.Equal(t, "allowances", cmd.Name)
		assert.Equal(t, "Manage quota allowances", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newQuotaAllowancesCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 4)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "create")
		assert.Contains(t, subcommandNames, "update")
		assert.Contains(t, subcommandNames, "delete")
	})

	t.Run("create command has correct flags", func(t *testing.T) {
		cmd := newQuotaAllowancesCommand()

		createCmd := findSubcommand(cmd.Commands, "create")
		require.NotNil(t, createCmd)
		require.NotNil(t, createCmd.Flags)
		assert.Len(t, createCmd.Flags, 7)

		flagNames := getFlagNames(createCmd.Flags)
		assert.Contains(t, flagNames, "user-id")
		assert.Contains(t, flagNames, "source")
		assert.Contains(t, flagNames, "quota-type")
		assert.Contains(t, flagNames, "upload-limit")
		assert.Contains(t, flagNames, "download-limit")
		assert.Contains(t, flagNames, "storage-limit")
		assert.Contains(t, flagNames, "expiry")
	})
}

func TestNewQuotaUserConfigsCommand(t *testing.T) {
	t.Run("creates quota user-configs command with correct configuration", func(t *testing.T) {
		cmd := newQuotaUserConfigsCommand()

		assert.Equal(t, "user-configs", cmd.Name)
		assert.Equal(t, "Manage user quota configurations", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newQuotaUserConfigsCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 3)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "update")
		assert.Contains(t, subcommandNames, "reset")
	})
}

func TestNewQuotaStatsCommand(t *testing.T) {
	t.Run("creates quota stats command with correct configuration", func(t *testing.T) {
		cmd := newQuotaStatsCommand()

		assert.Equal(t, "stats", cmd.Name)
		assert.Equal(t, "Get quota system statistics", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.NotNil(t, cmd.Action)
	})
}

func TestNewQuotaReconcileCommand(t *testing.T) {
	t.Run("creates quota reconcile command with correct configuration", func(t *testing.T) {
		cmd := newQuotaReconcileCommand()

		assert.Equal(t, "reconcile", cmd.Name)
		assert.Equal(t, "Reconcile quota data", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.NotNil(t, cmd.Action)
	})

	t.Run("has user-id flag", func(t *testing.T) {
		cmd := newQuotaReconcileCommand()

		require.NotNil(t, cmd.Flags)
		assert.Len(t, cmd.Flags, 1)

		flag, ok := cmd.Flags[0].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "user-id", flag.Name)
	})
}

func TestNewQuotaCleanupCommand(t *testing.T) {
	t.Run("creates quota cleanup command with correct configuration", func(t *testing.T) {
		cmd := newQuotaCleanupCommand()

		assert.Equal(t, "cleanup", cmd.Name)
		assert.Equal(t, "Cleanup expired quota data", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
		assert.NotNil(t, cmd.Action)
	})

	t.Run("has retention-days flag with default value", func(t *testing.T) {
		cmd := newQuotaCleanupCommand()

		require.NotNil(t, cmd.Flags)
		assert.Len(t, cmd.Flags, 1)

		flag, ok := cmd.Flags[0].(*cli.IntFlag)
		require.True(t, ok)
		assert.Equal(t, "retention-days", flag.Name)
		assert.Equal(t, 90, flag.Value)
	})
}

func TestNewBillingPricingPlansCommand(t *testing.T) {
	t.Run("creates billing pricing-plans command with correct configuration", func(t *testing.T) {
		cmd := newBillingPricingPlansCommand()

		assert.Equal(t, "pricing-plans", cmd.Name)
		assert.Equal(t, "Manage billing pricing plans", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newBillingPricingPlansCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 7)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "get")
		assert.Contains(t, subcommandNames, "create")
		assert.Contains(t, subcommandNames, "update")
		assert.Contains(t, subcommandNames, "delete")
		assert.Contains(t, subcommandNames, "sync")
		assert.Contains(t, subcommandNames, "sync-all")
	})
}

func TestNewBillingPricingPlanPeriodsCommand(t *testing.T) {
	t.Run("creates billing pricing-plan-periods command with correct configuration", func(t *testing.T) {
		cmd := newBillingPricingPlanPeriodsCommand()

		assert.Equal(t, "pricing-plan-periods", cmd.Name)
		assert.Equal(t, "Manage billing pricing plan periods", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newBillingPricingPlanPeriodsCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 5)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "get")
		assert.Contains(t, subcommandNames, "create")
		assert.Contains(t, subcommandNames, "update")
		assert.Contains(t, subcommandNames, "delete")
	})
}

func TestNewBillingSubscribersCommand(t *testing.T) {
	t.Run("creates billing subscribers command with correct configuration", func(t *testing.T) {
		cmd := newBillingSubscribersCommand()

		assert.Equal(t, "subscribers", cmd.Name)
		assert.Equal(t, "Manage billing subscribers", cmd.Usage)
		assert.NotEmpty(t, cmd.Description)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newBillingSubscribersCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 9)

		subcommandNames := getSubcommandNames(cmd.Commands)
		assert.Contains(t, subcommandNames, "list")
		assert.Contains(t, subcommandNames, "get")
		assert.Contains(t, subcommandNames, "list-gateway")
		assert.Contains(t, subcommandNames, "list-user")
		assert.Contains(t, subcommandNames, "cancel")
		assert.Contains(t, subcommandNames, "abort-cancel")
		assert.Contains(t, subcommandNames, "change-plan")
		assert.Contains(t, subcommandNames, "pause")
		assert.Contains(t, subcommandNames, "resume")
	})
}

// Helper functions

func getSubcommandNames(commands []*cli.Command) []string {
	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.Name
	}
	return names
}

func getFlagNames(flags []cli.Flag) []string {
	names := make([]string, len(flags))
	for i, flag := range flags {
		names[i] = flag.Names()[0]
	}
	return names
}

func findSubcommand(commands []*cli.Command, name string) *cli.Command {
	for _, cmd := range commands {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}
