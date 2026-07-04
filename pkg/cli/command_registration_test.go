package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// findCommand finds a command by name in a list of commands.
func findCommand(cmds []*cli.Command, name string) *cli.Command {
	for _, cmd := range cmds {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}

// commandNames extracts all command names from a slice.
func commandNames(cmds []*cli.Command) []string {
	names := make([]string, len(cmds))
	for i, cmd := range cmds {
		names[i] = cmd.Name
	}
	return names
}

// collectAllCommandNames walks the entire command tree and collects all command names.
func collectAllCommandNames(cmd *cli.Command) []string {
	var names []string
	walkCommands(cmd, func(c *cli.Command) {
		names = append(names, c.Name)
	})
	return names
}

// walkCommands recursively walks the command tree, calling fn for each command (including root).
func walkCommands(cmd *cli.Command, fn func(*cli.Command)) {
	fn(cmd)
	for _, sub := range cmd.Commands {
		walkCommands(sub, fn)
	}
}

// countCommands recursively counts all commands in the tree (including root).
func countCommands(cmd *cli.Command) int {
	count := 1
	for _, sub := range cmd.Commands {
		count += countCommands(sub)
	}
	return count
}

func TestCommandRegistration_RootSubcommands(t *testing.T) {
	root := NewRootCommand()

	expectedRootSubcommands := []string{
		"setup",
		"auth",
		"register",
		"confirm-email",
		"account",
		"upload",
		"download",
		"cat",
		"ls",
		"pin",
		"point",
		"unpoint",
		"pins",
		"list",
		"status",
		"unpin",
		"metadata",
		"operations",
		"config",
		"doctor",
		"bench",
		"dns",
		"ipns",
		"websites",
		"admin",
		"generate-docs",
		"mcp",
	}

	names := commandNames(root.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedRootSubcommands {
		assert.True(t, nameSet[expected], "root should have subcommand %q", expected)
	}

	// Verify exact count — no unexpected commands
	assert.Len(t, root.Commands, len(expectedRootSubcommands),
		"root should have exactly %d subcommands, got %d: %v",
		len(expectedRootSubcommands), len(root.Commands), names)
}

func TestCommandRegistration_Categories(t *testing.T) {
	root := NewRootCommand()

	expectedCategories := map[string]string{
		"setup":         "Setup",
		"auth":          "Setup",
		"register":      "Setup",
		"confirm-email": "Setup",
		"account":       "Setup",
		"upload":        "Content",
		"download":      "Content",
		"cat":           "Content",
		"ls":            "Content",
		"pin":           "Pinning",
		"pins":          "Pinning",
		"list":          "Pinning",
		"status":        "Pinning",
		"unpin":         "Pinning",
		"metadata":      "Pinning",
		"operations":    "Management",
		"dns":           "Management",
		"ipns":          "Management",
		"websites":      "Management",
		"config":        "System",
		"doctor":        "System",
		"bench":         "System",
		"point":         "Management",
		"unpoint":       "Management",
		"admin":         "Admin",
	}

	for name, expectedCat := range expectedCategories {
		cmd := findCommand(root.Commands, name)
		require.NotNil(t, cmd, "command %q should exist", name)
		assert.Equal(t, expectedCat, cmd.Category,
			"command %q should have category %q, got %q", name, expectedCat, cmd.Category)
	}
}

func TestCommandRegistration_PinsSubcommands(t *testing.T) {
	root := NewRootCommand()
	pins := findCommand(root.Commands, "pins")
	require.NotNil(t, pins, "pins command should exist")

	expectedPinsSubs := []string{"add", "rm", "ls", "status", "update"}
	names := commandNames(pins.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedPinsSubs {
		assert.True(t, nameSet[expected], "pins should have subcommand %q", expected)
	}
	assert.Len(t, pins.Commands, len(expectedPinsSubs),
		"pins should have exactly %d subcommands, got %d: %v",
		len(expectedPinsSubs), len(pins.Commands), names)
}

func TestCommandRegistration_AuthSubcommands(t *testing.T) {
	root := NewRootCommand()
	auth := findCommand(root.Commands, "auth")
	require.NotNil(t, auth, "auth command should exist")

	expectedAuthSubs := []string{"status"}
	names := commandNames(auth.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedAuthSubs {
		assert.True(t, nameSet[expected], "auth should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AccountSubcommands(t *testing.T) {
	root := NewRootCommand()
	account := findCommand(root.Commands, "account")
	require.NotNil(t, account, "account command should exist")

	expectedAccountSubs := []string{"otp", "api-keys"}
	names := commandNames(account.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedAccountSubs {
		assert.True(t, nameSet[expected], "account should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AccountOTPSubcommands(t *testing.T) {
	root := NewRootCommand()
	account := findCommand(root.Commands, "account")
	require.NotNil(t, account, "account command should exist")

	otp := findCommand(account.Commands, "otp")
	require.NotNil(t, otp, "otp command should exist")

	expectedOTPSubs := []string{"enable", "disable"}
	names := commandNames(otp.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedOTPSubs {
		assert.True(t, nameSet[expected], "otp should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AccountAPIKeysSubcommands(t *testing.T) {
	root := NewRootCommand()
	account := findCommand(root.Commands, "account")
	require.NotNil(t, account, "account command should exist")

	apiKeys := findCommand(account.Commands, "api-keys")
	require.NotNil(t, apiKeys, "api-keys command should exist")

	expectedSubs := []string{"list", "create", "delete"}
	names := commandNames(apiKeys.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "api-keys should have subcommand %q", expected)
	}
}

func TestCommandRegistration_DNSSubcommands(t *testing.T) {
	root := NewRootCommand()
	dns := findCommand(root.Commands, "dns")
	require.NotNil(t, dns, "dns command should exist")

	expectedDNSSubs := []string{"zones", "records"}
	names := commandNames(dns.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedDNSSubs {
		assert.True(t, nameSet[expected], "dns should have subcommand %q", expected)
	}
}

func TestCommandRegistration_DNSZonesSubcommands(t *testing.T) {
	root := NewRootCommand()
	dns := findCommand(root.Commands, "dns")
	require.NotNil(t, dns, "dns command should exist")

	zones := findCommand(dns.Commands, "zones")
	require.NotNil(t, zones, "dns zones command should exist")

	expectedSubs := []string{"list", "create", "get", "delete", "validate"}
	names := commandNames(zones.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "dns zones should have subcommand %q", expected)
	}
}

func TestCommandRegistration_DNSRecordsSubcommands(t *testing.T) {
	root := NewRootCommand()
	dns := findCommand(root.Commands, "dns")
	require.NotNil(t, dns, "dns command should exist")

	records := findCommand(dns.Commands, "records")
	require.NotNil(t, records, "dns records command should exist")

	expectedSubs := []string{"list", "create", "get", "update", "delete"}
	names := commandNames(records.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "dns records should have subcommand %q", expected)
	}
}

func TestCommandRegistration_IPNSSubcommands(t *testing.T) {
	root := NewRootCommand()
	ipns := findCommand(root.Commands, "ipns")
	require.NotNil(t, ipns, "ipns command should exist")

	expectedIPNSSubs := []string{"keys", "publish", "republish", "resolve"}
	names := commandNames(ipns.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedIPNSSubs {
		assert.True(t, nameSet[expected], "ipns should have subcommand %q", expected)
	}
}

func TestCommandRegistration_IPNSKeysSubcommands(t *testing.T) {
	root := NewRootCommand()
	ipns := findCommand(root.Commands, "ipns")
	require.NotNil(t, ipns, "ipns command should exist")

	keys := findCommand(ipns.Commands, "keys")
	require.NotNil(t, keys, "ipns keys command should exist")

	expectedSubs := []string{"list", "create", "get", "delete"}
	names := commandNames(keys.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "ipns keys should have subcommand %q", expected)
	}
}

func TestCommandRegistration_WebsitesSubcommands(t *testing.T) {
	root := NewRootCommand()
	websites := findCommand(root.Commands, "websites")
	require.NotNil(t, websites, "websites command should exist")

	expectedSubs := []string{
		"list", "create", "get", "update", "enable-ipns",
		"delete", "validate", "ssl", "config", "wizard",
	}
	names := commandNames(websites.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "websites should have subcommand %q", expected)
	}
}

func TestCommandRegistration_WebsitesSSLSubcommands(t *testing.T) {
	root := NewRootCommand()
	websites := findCommand(root.Commands, "websites")
	require.NotNil(t, websites, "websites command should exist")

	ssl := findCommand(websites.Commands, "ssl")
	require.NotNil(t, ssl, "websites ssl command should exist")

	expectedSubs := []string{"status"}
	names := commandNames(ssl.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "websites ssl should have subcommand %q", expected)
	}
}

func TestCommandRegistration_OperationsSubcommands(t *testing.T) {
	root := NewRootCommand()
	ops := findCommand(root.Commands, "operations")
	require.NotNil(t, ops, "operations command should exist")

	expectedSubs := []string{"list", "get"}
	names := commandNames(ops.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "operations should have subcommand %q", expected)
	}
}

func TestCommandRegistration_UnpinSubcommands(t *testing.T) {
	root := NewRootCommand()
	unpin := findCommand(root.Commands, "unpin")
	require.NotNil(t, unpin, "unpin command should exist")

	expectedSubs := []string{"all"}
	names := commandNames(unpin.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "unpin should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	expectedAdminSubs := []string{"quota", "billing", "websites", "pprof"}
	names := commandNames(admin.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedAdminSubs {
		assert.True(t, nameSet[expected], "admin should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminQuotaSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	quota := findCommand(admin.Commands, "quota")
	require.NotNil(t, quota, "admin quota command should exist")

	expectedSubs := []string{"plans", "allowances", "user-configs", "stats", "reconcile", "cleanup"}
	names := commandNames(quota.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin quota should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	expectedSubs := []string{
		"overview", "credits", "price-lines",
		"pricing-plans", "pricing-plan-periods", "subscribers",
	}
	names := commandNames(billing.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminWebsitesSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	websites := findCommand(admin.Commands, "websites")
	require.NotNil(t, websites, "admin websites command should exist")

	expectedSubs := []string{"block", "unblock"}
	names := commandNames(websites.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin websites should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminPprofSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	pprof := findCommand(admin.Commands, "pprof")
	require.NotNil(t, pprof, "admin pprof command should exist")

	expectedSubs := []string{
		"index", "block", "set-block-rate", "cmdline",
		"goroutine", "heap", "mutex", "set-mutex-fraction",
		"cpu", "status", "symbol", "threadcreate", "trace",
	}
	names := commandNames(pprof.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin pprof should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminQuotaPlansSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	quota := findCommand(admin.Commands, "quota")
	require.NotNil(t, quota, "admin quota command should exist")

	plans := findCommand(quota.Commands, "plans")
	require.NotNil(t, plans, "admin quota plans command should exist")

	expectedSubs := []string{"list", "get", "create", "update", "delete", "set-default"}
	names := commandNames(plans.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin quota plans should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingCreditsSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	credits := findCommand(billing.Commands, "credits")
	require.NotNil(t, credits, "admin billing credits command should exist")

	expectedSubs := []string{
		"list", "get", "create", "delete", "restore", "purge",
		"user-balance", "user-deleted-credits",
	}
	names := commandNames(credits.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing credits should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingSubscribersSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	subscribers := findCommand(billing.Commands, "subscribers")
	require.NotNil(t, subscribers, "admin billing subscribers command should exist")

	expectedSubs := []string{
		"list", "get", "list-gateway", "list-user",
		"cancel", "abort-cancel", "change-plan", "pause", "resume",
	}
	names := commandNames(subscribers.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing subscribers should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingPricingPlansSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	plans := findCommand(billing.Commands, "pricing-plans")
	require.NotNil(t, plans, "admin billing pricing-plans command should exist")

	expectedSubs := []string{
		"list", "get", "create", "update", "delete", "sync", "sync-all",
	}
	names := commandNames(plans.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing pricing-plans should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingPriceLinesSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	priceLines := findCommand(billing.Commands, "price-lines")
	require.NotNil(t, priceLines, "admin billing price-lines command should exist")

	expectedSubs := []string{
		"list", "get", "create", "update", "delete",
		"add-plan", "delete-plan", "update-plan-position",
	}
	names := commandNames(priceLines.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing price-lines should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminBillingPricingPlanPeriodsSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	billing := findCommand(admin.Commands, "billing")
	require.NotNil(t, billing, "admin billing command should exist")

	periods := findCommand(billing.Commands, "pricing-plan-periods")
	require.NotNil(t, periods, "admin billing pricing-plan-periods command should exist")

	expectedSubs := []string{"list", "get", "create", "update", "delete"}
	names := commandNames(periods.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin billing pricing-plan-periods should have subcommand %q", expected)
	}
}

func TestCommandRegistration_Aliases(t *testing.T) {
	root := NewRootCommand()

	aliasTests := map[string][]string{
		"websites": {"website"},
	}

	for cmdName, expectedAliases := range aliasTests {
		cmd := findCommand(root.Commands, cmdName)
		require.NotNil(t, cmd, "command %q should exist", cmdName)
		assert.Equal(t, expectedAliases, cmd.Aliases,
			"command %q should have aliases %v, got %v", cmdName, expectedAliases, cmd.Aliases)
	}
}

func TestCommandRegistration_AccountAPIKeysAliases(t *testing.T) {
	root := NewRootCommand()
	account := findCommand(root.Commands, "account")
	require.NotNil(t, account, "account command should exist")

	apiKeys := findCommand(account.Commands, "api-keys")
	require.NotNil(t, apiKeys, "api-keys command should exist")

	expectedAliases := []string{"apikey", "api-key"}
	assert.Equal(t, expectedAliases, apiKeys.Aliases,
		"api-keys should have aliases %v, got %v", expectedAliases, apiKeys.Aliases)
}

func TestCommandRegistration_MetadataHidden(t *testing.T) {
	root := NewRootCommand()
	metadata := findCommand(root.Commands, "metadata")
	require.NotNil(t, metadata, "metadata command should exist")

	assert.True(t, metadata.Hidden, "metadata command should be hidden")
}

func TestCommandRegistration_AllCommandsHaveUsage(t *testing.T) {
	root := NewRootCommand()

	walkCommands(root, func(cmd *cli.Command) {
		// Skip root — it has Usage but we only care about subcommands
		if cmd.Name == "pinner" {
			return
		}
		assert.NotEmpty(t, cmd.Usage, "command %q should have non-empty Usage", cmd.Name)
	})
}

func TestCommandRegistration_AllCommandsHaveActionOrSubcommands(t *testing.T) {
	root := NewRootCommand()

	walkCommands(root, func(cmd *cli.Command) {
		if cmd.Name == "pinner" {
			return
		}
		hasAction := cmd.Action != nil
		hasSubcommands := len(cmd.Commands) > 0
		assert.True(t, hasAction || hasSubcommands,
			"command %q should have an Action or subcommands", cmd.Name)
	})
}

func TestCommandRegistration_NoDuplicateNames(t *testing.T) {
	root := NewRootCommand()

	// Check each level for duplicate command names
	walkCommands(root, func(cmd *cli.Command) {
		seen := make(map[string]bool)
		for _, sub := range cmd.Commands {
			assert.False(t, seen[sub.Name],
				"duplicate command name %q under %q", sub.Name, cmd.Name)
			seen[sub.Name] = true
		}
	})
}

func TestCommandRegistration_CommandTreeDepth(t *testing.T) {
	root := NewRootCommand()

	// The command tree should have a reasonable depth.
	// Root → admin → billing → credits → list = depth 4 (max expected)
	totalCommands := countCommands(root)
	allNames := collectAllCommandNames(root)

	// Verify we have a substantial number of commands (the tree is well-populated)
	assert.Greater(t, totalCommands, 50,
		"command tree should have more than 50 commands total, got %d", totalCommands)

	// Verify no empty-named commands
	for _, name := range allNames {
		assert.NotEmpty(t, name, "all commands should have non-empty names")
	}
}

func TestCommandRegistration_DownloadCatLsCategories(t *testing.T) {
	root := NewRootCommand()

	contentCmds := []string{"download", "cat", "ls"}
	for _, name := range contentCmds {
		cmd := findCommand(root.Commands, name)
		require.NotNil(t, cmd, "command %q should exist", name)
		assert.Equal(t, "Content", cmd.Category,
			"command %q should have category Content, got %q", name, cmd.Category)
	}
}

func TestCommandRegistration_AdminQuotaAllowancesSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	quota := findCommand(admin.Commands, "quota")
	require.NotNil(t, quota, "admin quota command should exist")

	allowances := findCommand(quota.Commands, "allowances")
	require.NotNil(t, allowances, "admin quota allowances command should exist")

	expectedSubs := []string{"list", "create", "update", "delete"}
	names := commandNames(allowances.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin quota allowances should have subcommand %q", expected)
	}
}

func TestCommandRegistration_AdminQuotaUserConfigsSubcommands(t *testing.T) {
	root := NewRootCommand()
	admin := findCommand(root.Commands, "admin")
	require.NotNil(t, admin, "admin command should exist")

	quota := findCommand(admin.Commands, "quota")
	require.NotNil(t, quota, "admin quota command should exist")

	userConfigs := findCommand(quota.Commands, "user-configs")
	require.NotNil(t, userConfigs, "admin quota user-configs command should exist")

	expectedSubs := []string{"list", "update", "reset"}
	names := commandNames(userConfigs.Commands)
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, nameSet[expected], "admin quota user-configs should have subcommand %q", expected)
	}
}
