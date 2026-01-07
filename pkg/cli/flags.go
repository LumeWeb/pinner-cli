package cli

import (
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// Flag name constants

// Global/Output flags
const (
	FlagJSON    = "json"
	FlagVerbose = "verbose"
	FlagQuiet   = "quiet"
	FlagUnmask  = "unmask"
)

// Authentication flags
const (
	FlagAuthToken   = "auth-token"
	FlagEmail       = "email"
	FlagPassword    = "password"
	FlagFirstName   = "first-name"
	FlagLastName    = "last-name"
	FlagOTPCode     = "otp-code"
	FlagKeyName     = "key-name"
	FlagNoCreateKey = "no-create-key"
	FlagForce       = "force"
	FlagToken       = "token"
	FlagOTP         = "otp"
)

// Operation flags
const (
	FlagName        = "name"
	FlagWait        = "wait"
	FlagConfirm     = "confirm"
	FlagLimit       = "limit"
	FlagMemoryLimit = "memory-limit"
	FlagStatus      = "status"
	FlagWatch       = "watch"
	FlagFile        = "file"
	FlagParallel    = "parallel"
	FlagContinue    = "continue"
	FlagDryRun      = "dry-run"
	FlagSet         = "set"
	FlagClear       = "clear"
)

// Connection/Config flags
const (
	FlagSecure = "secure"
)

// GlobalFlags returns flags that are available to all commands.
func GlobalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  FlagJSON,
			Usage: "Output JSON instead of human-readable",
		},
		&cli.BoolFlag{
			Name:    FlagVerbose,
			Aliases: []string{"v"},
			Usage:   "Show detailed output",
		},
		&cli.BoolFlag{
			Name:    FlagQuiet,
			Aliases: []string{"q"},
			Usage:   "Suppress non-error output",
		},
		&cli.BoolFlag{
			Name:  FlagUnmask,
			Usage: "Show sensitive data (tokens, passwords, secrets) unmasked",
		},
		&cli.StringFlag{
			Name:    FlagAuthToken,
			Usage:   "Auth token to override config (env: PINNER_AUTH_TOKEN)",
			Sources: cli.EnvVars("PINNER_AUTH_TOKEN"),
		},
		SecureFlag(),
	}
}

// NameFlag returns a flag for setting a custom name.
func NameFlag(usage string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagName,
		Usage: usage,
	}
}

// WaitFlag returns a flag for waiting for operation completion.
func WaitFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagWait,
		Usage: "Wait for the pinning operation to complete before returning",
	}
}

// ConfirmFlag returns a flag to skip confirmation prompts.
func ConfirmFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagConfirm,
		Usage: "Skip confirmation prompts and proceed immediately",
	}
}

// LimitFlag returns a flag for limiting results.
func LimitFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagLimit,
		Usage: "Maximum number of results to return",
		Value: 10,
	}
}

// StatusFlag returns a flag for filtering by pin status.
func StatusFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagStatus,
		Usage: "Filter by pin status (queued, pinning, pinned, failed)",
	}
}

// MemoryLimitFlag returns a flag for setting the memory limit for CAR generation.
func MemoryLimitFlag() *cli.Uint64Flag {
	return &cli.Uint64Flag{
		Name:    FlagMemoryLimit,
		Usage:   "Memory limit for CAR generation in megabytes (e.g., 100, 256, 1024)",
		Value:   uint64(config.DefaultMemoryLimitMB),
		Sources: cli.EnvVars("PINNER_MEMORY_LIMIT"),
	}
}

// SecureFlag returns a flag for setting the secure flag for HTTPS connections.
func SecureFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:    FlagSecure,
		Usage:   "Use HTTPS for API connections (default: true)",
		Value:   true,
		Sources: cli.EnvVars("PINNER_SECURE"),
	}
}

// WatchFlag returns a flag for continuous monitoring of pins.
func WatchFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagWatch,
		Usage: "Continuously monitor and update pin status (useful for watching uploads)",
	}
}

// FileFlag returns a flag for reading CIDs from a file.
func FileFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagFile,
		Usage: "Read CIDs from a file (one per line)",
	}
}

// ParallelFlag returns a flag for setting parallel operation count.
func ParallelFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagParallel,
		Usage: "Number of parallel operations (default: 1)",
		Value: 1,
	}
}

// ContinueFlag returns a flag to continue on error.
func ContinueFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagContinue,
		Usage: "Continue processing even if some operations fail",
	}
}

// DryRunFlag returns a flag for dry-run mode.
func DryRunFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagDryRun,
		Usage: "Preview operations without making any changes",
	}
}

// GetAuthToken returns the auth token from command flag, environment variable, or config (in that order).
func GetAuthToken(cmd *cli.Command, cfgMgr config.Manager) string {
	if cmd != nil {
		token := cmd.String(FlagAuthToken)
		if token != "" {
			return token
		}
	}
	return cfgMgr.Config().AuthToken
}

// GetSecureSetting returns the secure setting from CLI flag or config.
// CLI flag takes precedence over config, but does NOT persist to disk.
// Returns the secure setting (true for HTTPS, false for HTTP).
func GetSecureSetting(cmd *cli.Command, cfgMgr config.Manager) bool {
	if cmd == nil {
		return cfgMgr.Config().Secure
	}

	// Secure flag overrides config if explicitly set (runtime override only)
	if cmd.IsSet(FlagSecure) {
		return cmd.Bool(FlagSecure)
	}

	// Use config default
	return cfgMgr.Config().Secure
}

// ApplySecureConfig applies secure flag configuration to the config manager and persists it.
// This should only be used for explicit configuration changes (e.g., setup wizard, config set).
// For runtime CLI flag overrides, use GetSecureSetting() instead.
// Deprecated: Use GetSecureSetting() for runtime overrides, or explicit SetSecure() with validation.
func ApplySecureConfig(cmd *cli.Command, cfgMgr config.Manager) bool {
	// For runtime overrides, use GetSecureSetting() which doesn't persist
	// This function is deprecated and should only be used for explicit config changes
	if cmd != nil && cmd.IsSet(FlagSecure) {
		secure := cmd.Bool(FlagSecure)
		_ = cfgMgr.SetSecure(secure)
		return secure
	}
	return cfgMgr.Config().Secure
}
