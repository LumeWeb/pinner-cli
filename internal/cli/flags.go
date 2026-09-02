package cli

import (
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/flag"
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
	FlagKey         = "key"
	FlagNoCreateKey = "no-create-key"
	FlagForce       = "force"
	FlagToken       = "token"
	FlagOTP         = "otp"
)

// Operation flags
const (
	FlagName        = "name"
	FlagWait        = "wait"
	FlagNoWait      = "no-wait"
	FlagConfirm     = "confirm"
	FlagLimit       = "limit"
	FlagMemoryLimit = "memory-limit"
	FlagPage        = "page"
	FlagPageSize    = "page-size"
	FlagStatus      = "status"
	FlagWatch       = "watch"
	FlagFile        = "file"
	FlagParallel    = "parallel"
	FlagContinue    = "continue"
	FlagDryRun      = "dry-run"
	FlagSet         = "set"
	FlagClear       = "clear"
	FlagYes         = "yes"
	FlagAll         = "all"
	FlagOperation   = "operation"
	FlagSearch      = "search"
	FlagProtocol    = "protocol"
	FlagSort        = "sort"
)

// Upload CAR builder flags
const (
	FlagChunkSize = "chunk-size"
	FlagChunker   = "chunker"
	FlagMaxLinks  = "max-links"
)

// Websites flags
const (
	FlagDomain       = "domain"
	FlagRenameTo     = "rename-to"
	FlagWebsite      = "website"
	FlagCID          = "cid"
	FlagTargetHash   = "target-hash"
	FlagTargetType   = "target-type"
	FlagDNSHosting   = "dns-hosting"
	FlagNoDNSHosting = "no-dns-hosting"
	FlagPrimary      = "primary"
	FlagNoPrimary    = "no-primary"
)

// DNS flags
const (
	FlagNameservers = "nameservers"
	FlagContent     = "content"
	FlagType        = "type"
	FlagTTL         = "ttl"
	FlagDisabled    = "disabled"
)

// Platform domain flags
const (
	FlagPlatformDomain = "platform-domain"
	FlagNamespace      = "namespace"
	FlagEnabled        = "enabled"
	FlagWebsiteID      = "website-id"
)

// Metadata flags
const (
	FlagMeta      = "meta"
	FlagClearMeta = "clear-meta"
)

// Connection/Config flags
const (
	FlagSecure = "secure"
)

// Admin command flags
const (
	FlagPlanID            = "plan-id"
	FlagUserID            = "user-id"
	FlagDescription       = "description"
	FlagUploadLimit       = "upload-limit"
	FlagDownloadLimit     = "download-limit"
	FlagStorageLimit      = "storage-limit"
	FlagAmount            = "amount"
	FlagCurrency          = "currency"
	FlagGatewayID         = "gateway-id"
	FlagPosition          = "position"
	FlagRetentionDays     = "retention-days"
	FlagSource            = "source"
	FlagQuotaType         = "quota-type"
	FlagIsActive          = "is-active"
	FlagIsDefault         = "is-default"
	FlagIsPublic          = "is-public"
	FlagPrice             = "price"
	FlagCadence           = "cadence"
	FlagQuotaPlanID       = "quota-plan-id"
	FlagRollingDays       = "rolling-days"
	FlagAllowFree         = "allow-free"
	FlagPricelineID       = "priceline-id"
	FlagMode              = "mode"
	FlagDirection         = "direction"
	FlagOlderThan         = "older-than"
	FlagForceDelete       = "force-delete"
	FlagWindowType        = "window-type"
	FlagExpiry            = "expiry"
	FlagFeatures          = "features"
	FlagEnforcementPolicy = "enforcement-policy"
	FlagUploadThreshold   = "upload-threshold"
	FlagDownloadThreshold = "download-threshold"
	FlagStorageThreshold  = "storage-threshold"
	FlagWindowDuration    = "window-duration"
	FlagWindowStartHour   = "window-start-hour"
	FlagWindowTimezone    = "window-timezone"
)

// Admin command name constants
const (
	CmdPriceLines         = "price-lines"
	CmdPricingPlans       = "pricing-plans"
	CmdPricingPlanPeriods = "pricing-plan-periods"
	CmdSubscribers        = "subscribers"
	CmdCredits            = "credits"
	CmdOverview           = "overview"
	CmdQuota              = "quota"
	CmdBilling            = "billing"
	CmdList               = "list"
	CmdGet                = "get"
	CmdCreate             = "create"
	CmdUpdate             = "update"
	CmdDelete             = "delete"
	CmdAddPlan            = "add-plan"
	CmdDeletePlan         = "delete-plan"
	CmdUpdatePlanPosition = "update-plan-position"
	CmdCancel             = "cancel"
	CmdAbortCancel        = "abort-cancel"
	CmdChangePlan         = "change-plan"
	CmdPause              = "pause"
	CmdResume             = "resume"
	CmdListGateway        = "list-gateway"
	CmdListUser           = "list-user"
	CmdRestore            = "restore"
	CmdPurge              = "purge"
	CmdUserBalance        = "user-balance"
	CmdUserDeletedCredits = "user-deleted-credits"
	CmdSetDefault         = "set-default"
	CmdReconcile          = "reconcile"
	CmdCleanup            = "cleanup"
	CmdPlans              = "plans"
	CmdAllowances         = "allowances"
	CmdUserConfigs        = "user-configs"
	CmdStats              = "stats"
	CmdReset              = "reset"
	CmdSync               = "sync"
	CmdSyncAll            = "sync-all"
	CmdBlock              = "block"
	CmdUnblock            = "unblock"
	CmdWebsites           = "websites"
	CmdAdmin              = "admin"
	CmdPprof              = "pprof"
	CmdPlatformDomains    = "platform-domains"
	CmdSocialProviders    = "social-providers"
	CmdSetBlockRate       = "set-block-rate"
	CmdSetMutexFraction   = "set-mutex-fraction"
	CmdIndex              = "index"
	CmdCmdline            = "cmdline"
	CmdGoroutine          = "goroutine"
	CmdHeap               = "heap"
	CmdMutex              = "mutex"
	CmdCPU                = "cpu"
	CmdSymbol             = "symbol"
	CmdThreadcreate       = "threadcreate"
	CmdTrace              = "trace"
	CmdStatus             = "status"
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
		flag.SensitiveStringFlag(&cli.StringFlag{
			Name:    FlagAuthToken,
			Usage:   "Auth token to override config",
			Sources: cli.EnvVars("PINNER_AUTH_TOKEN"),
		}),
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

// RequiredNameFlag returns a required flag for setting a name.
func RequiredNameFlag(usage string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagName,
		Usage:    usage,
		Required: true,
	}
}

// OptionalNameFlag returns an optional flag for setting a name.
func OptionalNameFlag(usage string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagName,
		Usage: usage,
	}
}

// WaitFlag returns a flag for waiting for operation completion.
// Deprecated: Use NoWaitFlag for upload/pin commands. WaitFlag is kept for
// IPNS publish which uses opt-in wait semantics.
func WaitFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagWait,
		Usage: "Wait for the operation to complete before returning",
	}
}

// NoWaitFlag returns a flag for skipping the wait for operation completion.
// By default, upload and pin commands wait for pinning to complete.
// Use --no-wait to return immediately after submitting the request.
func NoWaitFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagNoWait,
		Usage: "Return immediately without waiting for pinning to complete",
	}
}

// WaitFlagHidden returns a hidden --wait flag for backward compatibility.
// Waiting is now the default behavior, so this flag is a no-op.
func WaitFlagHidden() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:   FlagWait,
		Usage:  "Wait for operation to complete (default behavior, flag is a no-op)",
		Hidden: true,
	}
}

func ConfirmFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:   FlagConfirm,
		Usage:  "Skip confirmation prompts and proceed immediately",
		Hidden: true,
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

// PageFlag returns a flag for pagination page number (1-based).
func PageFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagPage,
		Usage: "Page number (1-based)",
		Value: 1,
	}
}

// PageSizeFlag returns a flag for pagination page size.
func PageSizeFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagPageSize,
		Usage: "Number of results per page",
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
		Usage:   "Memory limit for CAR generation in megabytes",
		Value:   uint64(config.DefaultMemoryLimitMB),
		Sources: cli.EnvVars("PINNER_MEMORY_LIMIT"),
	}
}

// SecureFlag returns a flag for setting the secure flag for HTTPS connections.
func SecureFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:    FlagSecure,
		Usage:   "Use HTTPS for API connections",
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

// ChunkSizeFlag returns a flag for setting the UnixFS chunk size in bytes.
func ChunkSizeFlag() *cli.Int64Flag {
	return &cli.Int64Flag{
		Name:  FlagChunkSize,
		Usage: "Chunk size in bytes for UnixFS file splitting (default: 1048576)",
	}
}

// ChunkerFlag returns a flag for setting the DAG layout strategy.
func ChunkerFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagChunker,
		Usage: "DAG layout strategy: balanced (default) or trickle",
	}
}

// MaxLinksFlag returns a flag for setting the max links per DAG node.
func MaxLinksFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagMaxLinks,
		Usage: "Maximum number of links per DAG node (default: 174)",
	}
}

// ForceFlag returns a flag for forcing operations.
func ForceFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagForce,
		Usage: "Force operation without confirmation",
	}
}

// YesFlag returns a flag for auto-accepting confirmation prompts
// (e.g. the unpin-all count-typing prompt) non-interactively.
func YesFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagYes,
		Usage: "Auto-accept confirmation prompts without interaction",
	}
}

// VaultExpiryFlag returns a flag for setting vault share link expiry.
func VaultExpiryFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagExpiry,
		Usage: "Share link expiry duration (e.g., 7d, 30d, 1h, 0 for never)",
		Value: defaultExpiry(),
	}
}

// defaultExpiry returns the default expiry duration, overridable via PINNER_EXPIRY_DEFAULT env var.
func defaultExpiry() string {
	if v := os.Getenv("PINNER_EXPIRY_DEFAULT"); v != "" {
		return v
	}
	return "7d"
}

// FlagProfile is the vault profile selection flag name.
const FlagProfile = "profile"

// ProfileFlag returns a vault-scoped flag for selecting which vault profile to operate on.
func ProfileFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    FlagProfile,
		Aliases: []string{"p"},
		Usage:   "Vault profile name (selects which vault to operate on)",
		Sources: cli.EnvVars("PINNER_PROFILE"),
	}
}

// DomainFlag returns a flag for the website domain.
func DomainFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagDomain,
		Usage: "Domain name",
	}
}

// RequiredDomainFlag returns a required flag for the website domain.
func RequiredDomainFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagDomain,
		Usage:    "Domain name for the DNS zone (e.g. example.com)",
		Required: true,
	}
}

// RenameDomainFlag returns a flag for renaming a website to a new domain.
// It pairs with the positional <domain> selector on `websites update`, which
// selects the website; this flag is the rename target.
func RenameDomainFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagRenameTo,
		Usage: "New domain to rename the website to",
	}
}

// WebsiteFlag returns a flag for selecting the parent website (id or domain).
// Used by `websites domains` subcommands to identify the website the domain
// binding belongs to, disambiguating it from the domain being acted on.
func WebsiteFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    FlagWebsite,
		Aliases: []string{"w"},
		Usage:   "Website (id or domain) the domain binding belongs to",
	}
}

// TargetHashFlag returns a flag for the target CID.
// Deprecated: Use CIDFlag instead.
func TargetHashFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagTargetHash,
		Usage: "Target CID for the website",
	}
}

// RequiredTargetHashFlag returns a required flag for the target CID.
// Deprecated: Use RequiredCIDFlag instead.
func RequiredTargetHashFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagTargetHash,
		Usage:    "Target CID for the website",
		Required: true,
	}
}

// CIDFlag returns a flag for the target CID.
func CIDFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagCID,
		Usage: "CID for the website",
	}
}

// RequiredCIDFlag returns a required flag for the target CID.
func RequiredCIDFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagCID,
		Usage:    "CID for the website",
		Required: true,
	}
}

// TargetTypeFlag returns a flag for the target type.
func TargetTypeFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagTargetType,
		Usage: "Target type (ipfs, ipns, etc.)",
		Value: "ipfs",
	}
}

// DNSHostingFlag returns a flag for enabling DNS hosting.
func DNSHostingFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagDNSHosting,
		Usage: "Enable DNS hosting for this website",
	}
}

// NoDNSHostingFlag returns a flag for disabling DNS hosting.
func NoDNSHostingFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagNoDNSHosting,
		Usage: "Disable DNS hosting for this website",
	}
}

// PrimaryFlag returns a flag for promoting a binding to primary.
func PrimaryFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagPrimary,
		Usage: "Promote this binding to the website's primary domain",
	}
}

// NoPrimaryFlag returns a flag for demoting a binding from primary.
func NoPrimaryFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagNoPrimary,
		Usage: "Demote this binding from the website's primary domain",
	}
}

// NameserversFlag returns a flag for custom nameservers.
func NameserversFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagNameservers,
		Usage: "Comma-separated list of nameservers (e.g., ns1.example.com,ns2.example.com)",
	}
}

// ContentFlag returns a flag for DNS record content.
func ContentFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagContent,
		Usage: "DNS record content",
	}
}

// RequiredContentFlag returns a required flag for DNS record content.
func RequiredContentFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagContent,
		Usage:    "DNS record content",
		Required: true,
	}
}

// TTLFlag returns a flag for DNS record TTL.
func TTLFlag() *cli.UintFlag {
	return &cli.UintFlag{
		Name:  FlagTTL,
		Usage: "DNS record TTL in seconds (default: 3600)",
		Value: 3600,
	}
}

// DisabledFlag returns a flag for disabling DNS records.
func DisabledFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagDisabled,
		Usage: "Disable the DNS record",
	}
}

// TypeFlag returns a flag for DNS record type.
func TypeFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagType,
		Usage: "DNS record type (A, AAAA, CNAME, TXT, MX, NS, etc.)",
	}
}

// RequiredTypeFlag returns a required flag for DNS record type.
func RequiredTypeFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     FlagType,
		Usage:    "DNS record type (A, AAAA, CNAME, TXT, MX, NS, etc.)",
		Required: true,
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

// MetaFlag returns a flag for setting metadata as key=value pairs.
func MetaFlag() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:  FlagMeta,
		Usage: "Set metadata as key=value (repeatable)",
	}
}

// ClearMetaFlag returns a flag for clearing all metadata.
func ClearMetaFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagClearMeta,
		Usage: "Clear all metadata",
	}
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
