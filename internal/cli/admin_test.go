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
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd)
		assert.Contains(t, subcommandNames, "quota")
		assert.Contains(t, subcommandNames, "billing")
		assert.Contains(t, subcommandNames, "websites")
		assert.Contains(t, subcommandNames, "pprof")
		assert.Contains(t, subcommandNames, "platform-domains")
		assert.Contains(t, subcommandNames, "social-providers")
	})
}

func TestNewQuotaCommand(t *testing.T) {
	t.Run("is catalog-compiled with correct configuration", func(t *testing.T) {
		cmd := newQuotaCommand()

		assert.Equal(t, "quota", cmd.Name)
		assert.Equal(t, "Quota management operations", cmd.Usage)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newQuotaCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd)
		assert.Contains(t, subcommandNames, "plans")
		assert.Contains(t, subcommandNames, "allowances")
		assert.Contains(t, subcommandNames, "user-configs")
		assert.Contains(t, subcommandNames, "stats")
		assert.Contains(t, subcommandNames, "reconcile")
		assert.Contains(t, subcommandNames, "cleanup")
	})

	t.Run("plans group exposes its operations", func(t *testing.T) {
		quota := newQuotaCommand()
		var plans *cli.Command
		for _, c := range quota.Commands {
			if c.Name == CmdPlans {
				plans = c
				break
			}
		}
		require.NotNil(t, plans)
		for _, want := range []string{CmdList, CmdGet, CmdCreate, CmdUpdate, CmdDelete, CmdSetDefault} {
			assert.Contains(t, getSubcommandNames(plans), want)
		}
	})
}

func TestNewBillingCommand(t *testing.T) {
	t.Run("is catalog-compiled with correct configuration", func(t *testing.T) {
		cmd := newBillingCommand()

		assert.Equal(t, "billing", cmd.Name)
		assert.Equal(t, "Billing management operations", cmd.Usage)
	})

	t.Run("has correct subcommands", func(t *testing.T) {
		cmd := newBillingCommand()

		require.NotNil(t, cmd.Commands)
		assert.Len(t, cmd.Commands, 6)

		subcommandNames := getSubcommandNames(cmd)
		assert.Contains(t, subcommandNames, "overview")
		assert.Contains(t, subcommandNames, "credits")
		assert.Contains(t, subcommandNames, "price-lines")
		assert.Contains(t, subcommandNames, "pricing-plans")
		assert.Contains(t, subcommandNames, "pricing-plan-periods")
		assert.Contains(t, subcommandNames, "subscribers")
	})
}

func findSubcommand(commands []*cli.Command, name string) *cli.Command {
	for _, cmd := range commands {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}
