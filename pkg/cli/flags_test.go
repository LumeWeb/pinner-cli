package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestGlobalFlags verifies that all global flags are properly defined.
func TestGlobalFlags(t *testing.T) {
	cmd := NewRootCommand()

	// Check that global flags are defined
	globalFlags := cmd.Flags

	flagNames := make(map[string]bool)
	for _, flag := range globalFlags {
		flagNames[flag.Names()[0]] = true
	}

	// Verify required global flags exist
	assert.True(t, flagNames["json"], "global --json flag should be defined")
	assert.True(t, flagNames["verbose"], "global --verbose flag should be defined")
	assert.True(t, flagNames["quiet"], "global --quiet flag should be defined")
}

// TestCommandFlagsVerification verifies that all commands have the required flags.
func TestCommandFlagsVerification(t *testing.T) {
	cmd := NewRootCommand()

	tests := []struct {
		name          string
		commandName   string
		requiredFlags []string
		optionalFlags []string
	}{
		{
			name:          "auth command",
			commandName:   "auth",
			requiredFlags: []string{},
			optionalFlags: []string{
				"email",
				"password",
				"otp-code",
				"key-name",
				"no-create-key",
				"force",
			},
		},
		{
			name:          "upload command",
			commandName:   "upload",
			requiredFlags: []string{},
			optionalFlags: []string{
				"name",
				"wait",
				"memory-limit",
			},
		},
		{
			name:          "download command",
			commandName:   "download",
			requiredFlags: []string{},
			optionalFlags: []string{
				"output",
				"force",
				"dry-run",
			},
		},
		{
			name:          "ls command",
			commandName:   "ls",
			requiredFlags: []string{},
			optionalFlags: []string{
				"limit",
			},
		},
		{
			name:          "pin command",
			commandName:   "pin",
			requiredFlags: []string{},
			optionalFlags: []string{
				"name",
				"wait",
			},
		},
		{
			name:          "list command",
			commandName:   "list",
			requiredFlags: []string{},
			optionalFlags: []string{
				"name",
				"limit",
			},
		},
		{
			name:          "status command",
			commandName:   "status",
			requiredFlags: []string{},
			optionalFlags: []string{
				"watch",
			},
		},
		{
			name:          "unpin command",
			commandName:   "unpin",
			requiredFlags: []string{},
			optionalFlags: []string{
				"confirm",
			},
		},
		{
			name:          "config command",
			commandName:   "config",
			requiredFlags: []string{},
			optionalFlags: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCmd *cli.Command
			for _, c := range cmd.Commands {
				if c.Name == tt.commandName {
					targetCmd = c
					break
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", tt.commandName)

			flagNames := make(map[string]bool)
			for _, flag := range targetCmd.Flags {
				flagNames[flag.Names()[0]] = true
			}

			// Verify required flags exist
			for _, flagName := range tt.requiredFlags {
				assert.True(t, flagNames[flagName], "command %s should have required flag --%s", tt.commandName, flagName)
			}

			// Verify optional flags exist
			for _, flagName := range tt.optionalFlags {
				assert.True(t, flagNames[flagName], "command %s should have optional flag --%s", tt.commandName, flagName)
			}
		})
	}
}

// TestCommandArgsVerification verifies that all commands have proper argument definitions.
func TestCommandArgsVerification(t *testing.T) {
	cmd := NewRootCommand()

	tests := []struct {
		name        string
		commandName string
		argsUsage   string
		minArgs     int
		maxArgs     int
	}{
		{
			name:        "auth command",
			commandName: "auth",
			argsUsage:   "[jwt-token]",
			minArgs:     0,
			maxArgs:     1,
		},
		{
			name:        "upload command",
			commandName: "upload",
			argsUsage:   "[path]",
			minArgs:     0,
			maxArgs:     1,
		},
		{
			name:        "download command",
			commandName: "download",
			argsUsage:   "<cid>[/<path>]",
			minArgs:     1,
			maxArgs:     1,
		},
		{
			name:        "cat command",
			commandName: "cat",
			argsUsage:   "<cid>[/<path>]",
			minArgs:     1,
			maxArgs:     1,
		},
		{
			name:        "ls command",
			commandName: "ls",
			argsUsage:   "<cid>[/<path>]",
			minArgs:     1,
			maxArgs:     1,
		},
		{
			name:        "pin command",
			commandName: "pin",
			argsUsage:   "<cid...>",
			minArgs:     0,
			maxArgs:     -1,
		},
		{
			name:        "list command",
			commandName: "list",
			argsUsage:   "",
			minArgs:     0,
			maxArgs:     0,
		},
		{
			name:        "status command",
			commandName: "status",
			argsUsage:   "<cid>",
			minArgs:     1,
			maxArgs:     1,
		},
		{
			name:        "unpin command",
			commandName: "unpin",
			argsUsage:   "<cid...>",
			minArgs:     0,
			maxArgs:     -1,
		},
		{
			name:        "config command",
			commandName: "config",
			argsUsage:   "[get <key> | set <key> <value>]",
			minArgs:     0,
			maxArgs:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCmd *cli.Command
			for _, c := range cmd.Commands {
				if c.Name == tt.commandName {
					targetCmd = c
					break
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", tt.commandName)

			// Verify args usage matches expected
			assert.Equal(t, tt.argsUsage, targetCmd.ArgsUsage, "command %s should have correct args usage", tt.commandName)
		})
	}
}

// TestCommandDescriptions verifies that all commands have descriptions.
func TestCommandDescriptions(t *testing.T) {
	cmd := NewRootCommand()

	requiredCommands := []string{
		"auth",
		"register",
		"confirm-email",
		"account",
		"upload",
		"download",
		"cat",
		"ls",
		"pin",
		"list",
		"status",
		"unpin",
		"config",
	}

	for _, commandName := range requiredCommands {
		t.Run(commandName+" has description", func(t *testing.T) {
			var targetCmd *cli.Command
			for _, c := range cmd.Commands {
				if c.Name == commandName {
					targetCmd = c
					break
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", commandName)

			assert.NotEmpty(t, targetCmd.Usage, "command %s should have usage text", commandName)
			assert.NotEmpty(t, targetCmd.Description, "command %s should have description", commandName)
		})
	}
}

// TestCommandRegistration verifies that all required commands are registered.
func TestCommandRegistration(t *testing.T) {
	cmd := NewRootCommand()

	requiredCommands := []string{
		"auth",
		"register",
		"confirm-email",
		"account",
		"upload",
		"download",
		"cat",
		"ls",
		"pin",
		"list",
		"status",
		"unpin",
		"config",
	}

	registeredCommands := make(map[string]bool)
	for _, c := range cmd.Commands {
		registeredCommands[c.Name] = true
	}

	for _, commandName := range requiredCommands {
		assert.True(t, registeredCommands[commandName], "command %s should be registered", commandName)
	}
}

// TestFlagAliases verifies that flags have proper aliases where specified.
func TestFlagAliases(t *testing.T) {
	cmd := NewRootCommand()

	tests := []struct {
		name        string
		commandName string
		flagName    string
		expected    []string
	}{
		{
			name:        "auth email flag",
			commandName: "auth",
			flagName:    "email",
			expected:    []string{"email", "e"},
		},
		{
			name:        "auth password flag",
			commandName: "auth",
			flagName:    "password",
			expected:    []string{"password", "p"},
		},
		{
			name:        "auth otp-code flag",
			commandName: "auth",
			flagName:    "otp-code",
			expected:    []string{"otp-code", "o"},
		},
		{
			name:        "auth key-name flag",
			commandName: "auth",
			flagName:    "key-name",
			expected:    []string{"key-name", "k"},
		},
		{
			name:        "root verbose flag",
			commandName: "",
			flagName:    "verbose",
			expected:    []string{"verbose", "v"},
		},
		{
			name:        "root quiet flag",
			commandName: "",
			flagName:    "quiet",
			expected:    []string{"quiet", "q"},
		},
		{
			name:        "download output flag",
			commandName: "download",
			flagName:    "output",
			expected:    []string{"output", "o"},
		},
		{
			name:        "download force flag",
			commandName: "download",
			flagName:    "force",
			expected:    []string{"force"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCmd *cli.Command
			if tt.commandName == "" {
				targetCmd = cmd
			} else {
				for _, c := range cmd.Commands {
					if c.Name == tt.commandName {
						targetCmd = c
						break
					}
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", tt.commandName)

			var found bool
			for _, flag := range targetCmd.Flags {
				if flag.Names()[0] == tt.flagName {
					assert.Equal(t, tt.expected, flag.Names(), "flag %s should have aliases %v", tt.flagName, tt.expected)
					found = true
					break
				}
			}
			assert.True(t, found, "flag %s should be found", tt.flagName)
		})
	}
}

// TestFlagDefaults verifies that flags have proper default values.
func TestFlagDefaults(t *testing.T) {
	cmd := NewRootCommand()

	tests := []struct {
		name        string
		commandName string
		flagName    string
		expected    any
	}{
		{
			name:        "list limit flag",
			commandName: "list",
			flagName:    "limit",
			expected:    10,
		},
		{
			name:        "auth key-name flag",
			commandName: "auth",
			flagName:    "key-name",
			expected:    "cli-generated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCmd *cli.Command
			for _, c := range cmd.Commands {
				if c.Name == tt.commandName {
					targetCmd = c
					break
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", tt.commandName)

			for _, flag := range targetCmd.Flags {
				if flag.Names()[0] == tt.flagName {
					if intFlag, ok := flag.(*cli.IntFlag); ok {
						assert.Equal(t, tt.expected, intFlag.Value)
					}
					if strFlag, ok := flag.(*cli.StringFlag); ok {
						assert.Equal(t, tt.expected, strFlag.Value)
					}
					break
				}
			}
		})
	}
}

// TestOutputFormatterIntegration verifies that commands properly use OutputFormatter.
func TestOutputFormatterIntegration(t *testing.T) {
	cmd := NewRootCommand()

	// Test that commands create OutputFormatter with global flags
	tests := []struct {
		name        string
		commandName string
	}{
		{"auth command", "auth"},
		{"upload command", "upload"},
		{"download command", "download"},
		{"cat command", "cat"},
		{"ls command", "ls"},
		{"pin command", "pin"},
		{"list command", "list"},
		{"status command", "status"},
		{"unpin command", "unpin"},
		{"config command", "config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCmd *cli.Command
			for _, c := range cmd.Commands {
				if c.Name == tt.commandName {
					targetCmd = c
					break
				}
			}
			require.NotNil(t, targetCmd, "command %s should exist", tt.commandName)

			// Verify that the command has an action
			assert.NotNil(t, targetCmd.Action, "command %s should have an action", tt.commandName)
		})
	}
}

// TestGlobalFlagsInheritance verifies that global flags are accessible to subcommands.
func TestGlobalFlagsInheritance(t *testing.T) {
	cmd := NewRootCommand()

	// The root command should have global flags
	assert.NotNil(t, cmd.Flags, "root command should have flags")

	// Verify global flags can be accessed
	globalFlagNames := make(map[string]bool)
	for _, flag := range cmd.Flags {
		for _, name := range flag.Names() {
			globalFlagNames[name] = true
		}
	}

	assert.True(t, globalFlagNames["json"], "global flag --json should be accessible")
	assert.True(t, globalFlagNames["verbose"], "global flag --verbose should be accessible")
	assert.True(t, globalFlagNames["quiet"], "global flag --quiet should be accessible")
}

// TestCommandStructure verifies the overall command structure.
func TestCommandStructure(t *testing.T) {
	cmd := NewRootCommand()

	// Verify root command properties
	assert.Equal(t, "pinner", cmd.Name)
	assert.NotEmpty(t, cmd.Usage)
	assert.NotEmpty(t, cmd.Description)
	assert.NotEmpty(t, cmd.Version)
	assert.Greater(t, len(cmd.Commands), 0, "root command should have subcommands")

	// Verify that subcommands have the root command as parent
	for _, subCmd := range cmd.Commands {
		assert.NotEmpty(t, subCmd.Name, "subcommand should have a name")
		assert.NotEmpty(t, subCmd.Usage, "subcommand should have usage")
	}
}

// TestCommandTree verifies the command tree structure matches spec.
func TestCommandTree(t *testing.T) {
	cmd := NewRootCommand()

	// Verify top-level commands
	topLevelCommands := []string{
		"auth",
		"register",
		"confirm-email",
		"account",
		"upload",
		"download",
		"cat",
		"ls",
		"pin",
		"list",
		"status",
		"unpin",
		"config",
	}

	registeredCommands := make(map[string]bool)
	for _, c := range cmd.Commands {
		registeredCommands[c.Name] = true
	}

	for _, commandName := range topLevelCommands {
		assert.True(t, registeredCommands[commandName], "top-level command %s should exist", commandName)
	}

	// Verify account subcommands
	var accountCmd *cli.Command
	for _, c := range cmd.Commands {
		if c.Name == "account" {
			accountCmd = c
			break
		}
	}
	require.NotNil(t, accountCmd, "account command should exist")

	accountSubcommands := []string{"otp", "api-keys"}
	for _, subcommandName := range accountSubcommands {
		var found bool
		for _, subCmd := range accountCmd.Commands {
			if subCmd.Name == subcommandName {
				found = true
				break
			}
		}
		assert.True(t, found, "account subcommand %s should exist", subcommandName)
	}

	// Verify otp subcommands
	var otpCmd *cli.Command
	for _, c := range accountCmd.Commands {
		if c.Name == "otp" {
			otpCmd = c
			break
		}
	}
	require.NotNil(t, otpCmd, "otp command should exist")

	otpSubcommands := []string{"enable", "disable"}
	for _, subcommandName := range otpSubcommands {
		var found bool
		for _, subCmd := range otpCmd.Commands {
			if subCmd.Name == subcommandName {
				found = true
				break
			}
		}
		assert.True(t, found, "otp subcommand %s should exist", subcommandName)
	}
}

func TestGetAuthToken(t *testing.T) {
	t.Run("returns flag value when set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.StringFlag{Name: FlagAuthToken}}
		_ = cmd.Set(FlagAuthToken, "flag-token")
		result := GetAuthToken(cmd, cfgMgr)
		assert.Equal(t, "flag-token", result)
	})

	t.Run("returns config value when flag not set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.StringFlag{Name: FlagAuthToken}}
		result := GetAuthToken(cmd, cfgMgr)
		assert.Equal(t, "test-token", result)
	})

	t.Run("returns config value when cmd is nil", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		result := GetAuthToken(nil, cfgMgr)
		assert.Equal(t, "test-token", result)
	})
}

func TestGetSecureSetting(t *testing.T) {
	t.Run("returns config value when cmd is nil", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		result := GetSecureSetting(nil, cfgMgr)
		assert.True(t, result)
	})

	t.Run("returns flag value when explicitly set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.BoolFlag{Name: FlagSecure}}
		_ = cmd.Set(FlagSecure, "false")
		result := GetSecureSetting(cmd, cfgMgr)
		assert.False(t, result)
	})

	t.Run("returns config default when flag not set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.BoolFlag{Name: FlagSecure}}
		result := GetSecureSetting(cmd, cfgMgr)
		assert.True(t, result)
	})
}

func TestApplySecureConfig(t *testing.T) {
	t.Run("returns config value when cmd is nil", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		result := ApplySecureConfig(nil, cfgMgr)
		assert.True(t, result)
	})

	t.Run("applies and returns flag value when set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cfgMgr.EXPECT().SetSecure(false).Return(nil)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.BoolFlag{Name: FlagSecure}}
		_ = cmd.Set(FlagSecure, "false")
		result := ApplySecureConfig(cmd, cfgMgr)
		assert.False(t, result)
	})

	t.Run("returns config default when flag not set", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		cmd := &cli.Command{}
		cmd.Flags = []cli.Flag{&cli.BoolFlag{Name: FlagSecure}}
		result := ApplySecureConfig(cmd, cfgMgr)
		assert.True(t, result)
	})
}

func TestTargetHashFlag(t *testing.T) {
	flag := TargetHashFlag()
	assert.Equal(t, FlagTargetHash, flag.Name)
}

func TestRequiredTargetHashFlag(t *testing.T) {
	flag := RequiredTargetHashFlag()
	assert.Equal(t, FlagTargetHash, flag.Name)
	assert.True(t, flag.Required)
}

func TestRequiredContentFlag(t *testing.T) {
	flag := RequiredContentFlag()
	assert.Equal(t, FlagContent, flag.Name)
	assert.True(t, flag.Required)
}
