package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// defaultMCPServiceName is the systemd unit / service identifier (without the
// per-backend extension; backends append their own, e.g. ".service").
const defaultMCPServiceName = "pinner-mcp"

// serviceConfigManager builds a lazy config manager for the service install
// path, used only to persist tunnel credentials entered during install to the
// last-resort store. It returns nil on failure so the install proceeds without
// persistence rather than failing on an optional optimization.
// ServiceConfigManager returns a lazily-initialized config manager (nil on
// failure) used to persist user-supplied tunnel credentials to the last-resort
// store so later runs auto-detect them. Exported so the mcp install wizard can
// pass a manager to the spliced ServiceInstallSteps.
func ServiceConfigManager() config.Manager { return serviceConfigManager() }

func serviceConfigManager() config.Manager {
	mgr, err := config.NewManager(config.DefaultConfigPath)
	if err != nil {
		return nil
	}
	// Load the existing config before any Save()/SetTunnelCredential so
	// persistence never rewrites config.yaml from an unloaded in-memory state
	// (which would silently drop pre-existing keys).
	if err := mgr.Load(); err != nil {
		return nil
	}
	return mgr
}

// parseTunnelProvider normalizes a raw provider string into the typed enum,
// returning an error for unknown values.
func parseTunnelProvider(raw string) (tunnel.TunnelProvider, error) {
	switch tunnel.TunnelProvider(strings.ToLower(strings.TrimSpace(raw))) {
	case tunnel.TunnelProviderOpenAI, tunnel.TunnelProviderNgrok, tunnel.TunnelProviderCloudflared:
		return tunnel.TunnelProvider(strings.ToLower(strings.TrimSpace(raw))), nil
	case "localhost":
		return "", nil
	case "":
		return "", errors.New("MCP_TUNNEL_PROVIDER is required in the service environment file")
	default:
		return "", fmt.Errorf("unsupported MCP tunnel provider %q", raw)
	}
}

const (
	serviceEnvFileFlag     = "env-file"
	serviceSystemFlag      = "system"
	serviceTunnelFlag      = "tunnel"
	serviceTunnelIDFlag    = "tunnel-id"
	serviceTunnelTokenFlag = "token"
	serviceApiKeyFlag      = "api-key"
	serviceDomainFlag      = "domain"
	serviceTunnelNameFlag  = "tunnel-name"
	serviceAuthTokenFlag   = "auth-token"
	serviceOAuthFlag       = "oauth"
	// serviceNgrokAPIKeyFlag is the ngrok REST API key, distinct from the
	// authtoken (--token / NGROK_AUTHTOKEN). The REST API (api.ngrok.com) only
	// accepts an API key as its bearer credential, and it is what the install
	// wizard uses to discover the account's public (dev/reserved) domain so it
	// can resolve MCP_PUBLIC_URL from what the user actually has.
	serviceNgrokAPIKeyFlag = "ngrok-api-key"
	servicePublicURLFlag   = "public-url"
	serviceHostFlag        = "host"
	servicePortFlag        = "port"
	// serviceDevToolsFlag mirrors the `pinner mcp` serve --dev-tools switch
	// (MCP_DEV_TOOLS), so a managed install can persist it into the service env
	// file and the running server starts with dev introspection tools enabled.
	serviceDevToolsFlag = "dev-tools"
)

// ServiceOAuthFlagName is the exported name of the --oauth flag, the single
// source of truth for command-line detection of an explicit OAuth decision
// (distinct from an env-sourced MCP_OAUTH value, which IsSet cannot tell apart
// from a CLI flag).
const ServiceOAuthFlagName = serviceOAuthFlag

// managedServiceFlags returns the tunnel/environment flags for the service
// command. Each flag declares its environment fallback via Sources so the CLI
// framework resolves flag -> env automatically, with no ad-hoc env parsing.
func managedServiceFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: serviceEnvFileFlag, Usage: "MCP service environment file"},
		&cli.BoolFlag{Name: serviceSystemFlag, Usage: "Use a system-wide service (not supported yet)"},
		&cli.StringFlag{Name: serviceTunnelFlag, Usage: "Tunnel provider: ngrok, cloudflared, or openai. openai requires --tunnel-id; ngrok requires --token or NGROK_AUTHTOKEN", Sources: cli.EnvVars("MCP_TUNNEL_PROVIDER")},
		&cli.StringFlag{Name: serviceTunnelIDFlag, Usage: "OpenAI Secure MCP Tunnel ID (required with --tunnel openai). May also be set via CONTROL_PLANE_TUNNEL_ID or the pinner config manager", Sources: cli.EnvVars("MCP_TUNNEL_ID", "CONTROL_PLANE_TUNNEL_ID")},
		&cli.StringFlag{Name: serviceTunnelTokenFlag, Usage: "Tunnel provider account token (e.g. ngrok authtoken)", Sources: cli.EnvVars("MCP_TUNNEL_TOKEN", "NGROK_AUTHTOKEN")},
		&cli.StringFlag{Name: serviceNgrokAPIKeyFlag, Usage: "ngrok REST API key (distinct from the authtoken; used to resolve the account's public domain)", Sources: cli.EnvVars("NGROK_API_KEY")},
		&cli.StringFlag{Name: serviceApiKeyFlag, Usage: "OpenAI Secure MCP Tunnel control-plane API key (persisted as CONTROL_PLANE_API_KEY)", Sources: cli.EnvVars("CONTROL_PLANE_API_KEY", "OPENAI_API_KEY")},
		&cli.StringFlag{Name: serviceDomainFlag, Usage: "Custom domain for the tunnel (required for cloudflared, optional for ngrok on paid accounts)", Sources: cli.EnvVars("MCP_DOMAIN")},
		&cli.StringFlag{Name: serviceTunnelNameFlag, Usage: "Cloudflare tunnel resource name (default: pinner-mcp)", Sources: cli.EnvVars("MCP_TUNNEL_NAME")},
		&cli.StringFlag{Name: serviceAuthTokenFlag, Usage: "Shared secret authorizing public HTTP MCP endpoints", Sources: cli.EnvVars("MCP_AUTH_TOKEN")},
		&cli.BoolFlag{Name: serviceOAuthFlag, Usage: "Enable the OAuth 2.1 handshake for OAuth-expecting MCP clients", Sources: cli.EnvVars("MCP_OAUTH")},
		&cli.StringFlag{Name: servicePublicURLFlag, Usage: "Public base URL advertised in OAuth discovery metadata", Sources: cli.EnvVars("MCP_PUBLIC_URL")},
		&cli.StringFlag{Name: serviceHostFlag, Usage: "Local bind host for the HTTP transport", Sources: cli.EnvVars("MCP_HOST")},
		&cli.IntFlag{Name: servicePortFlag, Value: 0, Usage: "Local bind port for the HTTP transport (0 picks a free port)", Sources: cli.EnvVars("MCP_PORT")},
		&cli.BoolFlag{Name: serviceDevToolsFlag, Usage: "Enable developer introspection tools (dev_host_env, dev_profile, dev_request) and capture the raw wire snapshot of the connected host; persisted as MCP_DEV_TOOLS in the service env file", Sources: cli.EnvVars("MCP_DEV_TOOLS")},
	}
}

// ServiceInstallFlags returns the tunnel/environment flags shared by the
// `pinner mcp service` command and the `pinner mcp install` HTTP composite.
// The install command appends these so flag -> env sourcing
// (MCP_AUTH_TOKEN, MCP_PUBLIC_URL, MCP_TUNNEL_PROVIDER, ...) resolves the same
// way for both paths, without duplicating any flag names.
func ServiceInstallFlags() []cli.Flag { return managedServiceFlags() }

// ManagedServiceCommand returns the rootless MCP service lifecycle command.
func ManagedServiceCommand() *cli.Command {
	return &cli.Command{
		Name:  "service",
		Usage: "Manage the rootless MCP systemd user service",
		Flags: managedServiceFlags(),
		Commands: []*cli.Command{
			{Name: "validate", Usage: "Validate MCP service configuration", Action: serviceValidateAction},
			{Name: "install", Usage: "Install and enable the MCP user service", Action: serviceInstallAction},
			{Name: "uninstall", Usage: "Disable and remove the MCP user service", Action: serviceUninstallAction},
			{Name: "start", Usage: "Start the MCP user service", Action: serviceStartAction},
			{Name: "stop", Usage: "Stop the MCP user service", Action: serviceStopAction},
			{Name: "restart", Usage: "Restart the MCP user service", Action: serviceRestartAction},
			{Name: "status", Usage: "Show MCP user service status", Action: serviceStatusAction},
			{Name: "logs", Usage: "Show MCP user service logs", Flags: []cli.Flag{&cli.BoolFlag{Name: "follow", Usage: "Follow service logs"}}, Action: serviceLogsAction},
		},
	}
}

func serviceValidateAction(ctx context.Context, cmd *cli.Command) error {
	// Headless read-only validate: never spawn a browser for missing
	// credentials, only print the deep-link URL.
	_, err := resolveManagedService(ctx, cmd, true, true)
	return err
}

func serviceInstallAction(ctx context.Context, cmd *cli.Command) error {
	envFile, svc, err := resolveManagedServiceForInstall(ctx, cmd)
	if err != nil {
		return err
	}
	// Stop an already-installed service before reinstalling (releases the
	// running process and its tunnel endpoint, and applies the new unit
	// cleanly), then install and start — Install never auto-starts.
	if err := installManagedService(ctx, svc); err != nil {
		return err
	}
	fmt.Printf("MCP service installed and started (environment file: %s).\n", envFile)
	fmt.Printf("Run `pinner mcp service status` to check it and `pinner mcp service logs` to inspect output.\n")
	return nil
}

func serviceUninstallAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	return svc.Uninstall(ctx)
}

func serviceStartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	return svc.Start(ctx)
}

func serviceStopAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	return svc.Stop(ctx)
}

func serviceRestartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	return svc.Restart(ctx)
}

func serviceStatusAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	status, err := svc.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("MCP service: %s\n", status.Summary)
	return nil
}

func serviceLogsAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false, false)
	if err != nil {
		return err
	}
	return svc.Logs(ctx, cmd.Bool("follow"))
}

// resolveServiceEnvFile returns the resolved service env file path from the
// --env-file flag, defaulting to ~/.config/pinner/mcp.env.
func resolveServiceEnvFile(cmd *cli.Command) (string, error) {
	if cmd.Bool(serviceSystemFlag) {
		return "", errors.New("system-wide MCP services are not implemented; use the rootless user service")
	}
	envFile := cmd.String(serviceEnvFileFlag)
	if envFile == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		envFile = filepath.Join(dir, "pinner", defaultMCPEnvFileName)
	}
	return expandServicePath(envFile), nil
}

// ResolveServiceEnvFile is the exported form of resolveServiceEnvFile, used by
// the mcp install wizard when it splices the tunnel-config steps so they can
// write to the concrete on-disk env file path.
func ResolveServiceEnvFile(cmd *cli.Command) (string, error) {
	return resolveServiceEnvFile(cmd)
}

// validateServiceEnvironment checks the env file permissions and contents and
// returns its resolved tunnel provider. It requires the file to already exist.
// The file is the source of truth: a systemd service reads ONLY this file at
// runtime (not the caller's process environment), so required credentials must
// be present in the file itself — never just in os.Getenv, which would let an
// install report success while the running service gets an empty value.
func validateServiceEnvironment(envFile string, nonInteractive bool) (tunnel.TunnelProvider, error) {
	info, err := os.Stat(envFile)
	if err != nil {
		return "", fmt.Errorf("MCP service environment file %q is unavailable: %w", envFile, err)
	}
	// Windows has no Unix permission bits: os.Chmod(0600) only toggles the
	// read-only attribute and Stat reports 0666 for every file, so the
	// group/world-readable check is meaningless there. On Unix the file must
	// not be readable by group or others (WriteServiceEnvironment writes 0600).
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("MCP service environment file %q is group/world-readable; run chmod 600 %s", envFile, envFile)
	}
	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		return "", err
	}
	providerRaw := strings.TrimSpace(env["MCP_TUNNEL_PROVIDER"])
	if providerRaw == "" {
		// No tunnel provider: localhost mode. No tunnel credentials
		// are required, and the server auto-generates an OAuth
		// secret at runtime when --oauth is set.
		return "", nil
	}
	provider, err := parseTunnelProvider(providerRaw)
	if err != nil {
		return "", err
	}
	// Validation is a read-only command: when nonInteractive (headless service
	// validate / --agent) never spawn a browser, only print the deep-link URL.
	// Interactive install/wizard paths may open the browser to guide the user.
	deeplink := func(operation, missing string) {
		if nonInteractive {
			tunnel.PrintTunnelDeepLink(operation, missing)
			return
		}
		tunnel.OpenTunnelDeepLink(operation, missing)
	}
	switch provider {
	case tunnel.TunnelProviderOpenAI:
		tunnelID := strings.TrimSpace(env["MCP_TUNNEL_ID"])
		if tunnelID == "" {
			deeplink("openai", "tunnel_id")
			return "", fmt.Errorf("MCP_TUNNEL_ID is required for the OpenAI tunnel (create one in the OpenAI Tunnels page)")
		}
		if !tunnel.OpenAITunnelID.MatchString(tunnelID) {
			deeplink("openai", "tunnel_id")
			return "", fmt.Errorf("invalid OpenAI tunnel ID %q", tunnelID)
		}
		if strings.TrimSpace(env["CONTROL_PLANE_API_KEY"]) == "" && strings.TrimSpace(env["OPENAI_API_KEY"]) == "" {
			deeplink("openai", "api_key")
			return "", fmt.Errorf("CONTROL_PLANE_API_KEY or OPENAI_API_KEY must be present in %s for the OpenAI tunnel (use --api-key to persist it; create a Runtime API key in the OpenAI API keys page)", envFile)
		}
	case tunnel.TunnelProviderNgrok, tunnel.TunnelProviderCloudflared:
		if strings.TrimSpace(env["MCP_AUTH_TOKEN"]) == "" {
			return "", errors.New("MCP_AUTH_TOKEN is required for public HTTP MCP tunnels")
		}
		if provider == tunnel.TunnelProviderNgrok && strings.TrimSpace(env["NGROK_AUTHTOKEN"]) == "" && strings.TrimSpace(env["MCP_TUNNEL_TOKEN"]) == "" {
			deeplink("ngrok", "authtoken")
			return "", errors.New("NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN is required for the ngrok tunnel")
		}
		// Only cloudflared runs as an external subprocess; ngrok is embedded
		// via the Go SDK and needs no binary on PATH.
		if provider == tunnel.TunnelProviderCloudflared {
			if _, err := exec.LookPath(string(provider)); err != nil {
				return "", fmt.Errorf("%s executable not found on PATH: %w", provider, err)
			}
			if strings.TrimSpace(env["MCP_DOMAIN"]) == "" {
				return "", errors.New("MCP_DOMAIN is required for cloudflared")
			}
		}
	}
	return provider, nil
}

func resolveManagedService(_ context.Context, cmd *cli.Command, validate, nonInteractive bool) (service.Service, error) {
	envFile, err := resolveServiceEnvFile(cmd)
	if err != nil {
		return nil, err
	}
	if !validate {
		return newManagedService(cmd, envFile, "")
	}
	provider, err := validateServiceEnvironment(envFile, nonInteractive)
	if err != nil {
		return nil, err
	}
	return newManagedService(cmd, envFile, provider)
}

// resolveManagedServiceForInstall bootstraps the env file (from flags when a
// --tunnel flag is given, or via the interactive wizard otherwise) when it does
// not yet exist, then resolves the managed service with validation.
func resolveManagedServiceForInstall(ctx context.Context, cmd *cli.Command) (string, service.Service, error) {
	envFile, err := resolveServiceEnvFile(cmd)
	if err != nil {
		return "", nil, err
	}
	created := false
	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		created = true
		// A lazy config manager (nil on failure) is threaded into the wizard and
		// bootstrap so any tunnel credential the user supplies is persisted to
		// the last-resort store and auto-detected on later runs.
		cfgMgr := serviceConfigManager()
		if cmd.String(serviceTunnelFlag) != "" {
			if err := bootstrapServiceEnvironment(cmd, envFile, cfgMgr); err != nil {
				return "", nil, err
			}
		} else {
			if err := RunServiceInstallWizard(ctx, cmd, envFile, cfgMgr); err != nil {
				return "", nil, err
			}
		}
	} else if err != nil {
		return "", nil, fmt.Errorf("inspect MCP service environment file %q: %w", envFile, err)
	}
	provider, err := validateServiceEnvironment(envFile, false)
	if err != nil {
		// A freshly bootstrapped file that fails completeness validation would
		// otherwise strand the user with a partial/corrupt env file on re-run.
		// Only remove it when we created it; never touch a pre-existing file.
		if created {
			_ = os.Remove(envFile)
		}
		return "", nil, err
	}
	svc, err := newManagedService(cmd, envFile, provider)
	if err != nil {
		return "", nil, err
	}
	return envFile, svc, nil
}

// explicitServiceEnvFromFlags returns the service env keys the operator set
// EXPLICITLY on the CLI or via their MCP_* source env vars. Only explicitly-set
// keys are included so the result can be safely overlaid onto an EXISTING env
// file without clobbering values a prior run persisted. This is the single
// source of truth for "which install flags override saved config"; every path
// that must honor operator intent (fresh bootstrap AND re-run reconcile) goes
// through it.
//
// OAuth is deliberately NOT defaulted here: the secure default-on is applied
// only when creating a fresh file (see bootstrapServiceEnvironment). On a
// re-run, an unset --oauth must leave whatever MCP_OAUTH the file already has.
func explicitServiceEnvFromFlags(cmd *cli.Command) (service.Environment, error) {
	provider, err := parseTunnelProvider(cmd.String(serviceTunnelFlag))
	if err != nil && provider == "" {
		provider = ""
	} else if err != nil {
		return nil, err
	}
	env := service.Environment{}
	setIf := func(key, flag string) {
		// Persist only non-empty values. A flag that IsSet but evaluates empty
		// (e.g. --public-url "" ) must NOT overwrite a saved non-empty value in
		// an existing env file — doing so would take a working install to a
		// broken/unconfigurable state. OAuth/Port are handled below from their
		// typed (bool/int) values, so they are unaffected by this guard.
		if cmd.IsSet(flag) {
			if v := strings.TrimSpace(cmd.String(flag)); v != "" {
				env[key] = v
			}
		}
	}
	if provider != "" {
		env["MCP_TUNNEL_PROVIDER"] = string(provider)
	}
	setIf("MCP_TUNNEL_ID", serviceTunnelIDFlag)
	setIf("CONTROL_PLANE_API_KEY", serviceApiKeyFlag)
	setIf("MCP_DOMAIN", serviceDomainFlag)
	setIf("MCP_TUNNEL_NAME", serviceTunnelNameFlag)
	setIf("MCP_AUTH_TOKEN", serviceAuthTokenFlag)
	setIf("MCP_TUNNEL_TOKEN", serviceTunnelTokenFlag)
	setIf("MCP_PUBLIC_URL", servicePublicURLFlag)
	setIf("MCP_HOST", serviceHostFlag)
	if cmd.IsSet(serviceOAuthFlag) {
		if cmd.Bool(serviceOAuthFlag) {
			env["MCP_OAUTH"] = "true"
		} else {
			env["MCP_OAUTH"] = "false"
		}
	}
	if cmd.IsSet(serviceDevToolsFlag) {
		env["MCP_DEV_TOOLS"] = strconv.FormatBool(cmd.Bool(serviceDevToolsFlag))
	}
	if cmd.IsSet(servicePortFlag) {
		env["MCP_PORT"] = strconv.Itoa(cmd.Int(servicePortFlag))
	}
	return env, nil
}

// ReconcileServiceEnvironmentFromFlags overlays the operator's explicitly-set
// install flags onto an existing service env file, preserving every other key.
// Used on the skip path of mcp install: a re-run against an existing complete
// env file must let an explicit --oauth / --port / --host / --public-url win
// over what a prior run persisted, instead of silently ignoring it.
func ReconcileServiceEnvironmentFromFlags(cmd *cli.Command, envFile string) error {
	overlay, err := explicitServiceEnvFromFlags(cmd)
	if err != nil {
		return err
	}
	if len(overlay) == 0 {
		return nil
	}
	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		return fmt.Errorf("load service environment %q for reconcile: %w", envFile, err)
	}
	for k, v := range overlay {
		env[k] = v
	}
	if err := service.WriteEnvironment(envFile, env); err != nil {
		return fmt.Errorf("reconcile MCP service environment file: %w", err)
	}
	return nil
}

// ReconcileServiceEnvironmentFromInstallState overlays the tunnel credentials
// an operator just prompted/confirmed in an interactive re-run (s) onto the
// existing service env file, preserving every other key (MCP_OAUTH, MCP_PORT,
// unmodeled keys, secrets from prior runs). This is what makes a re-run act as
// a real reconfiguration: without it the config step's prompted values would be
// discarded because the write step is a no-op on a pre-existing file.
//
// prevProvider must be the MCP_TUNNEL_PROVIDER that was on disk BEFORE any flag
// reconcile ran in this install — NOT re-read from the file after the flags
// reconcile, which may already have overwritten it with the --tunnel switch
// value. Passing the pre-reconcile provider lets a provider switch purge the
// previous provider's orphaned keys even when the flags reconcile already
// wrote the new provider into the file.
//
// explicitPublicURL reports whether the operator passed --public-url this run.
// When true, the resolved MCP_PUBLIC_URL is an explicit operator decision that
// is valid under any provider, so a provider switch must NOT purge it. When
// false, it is a per-provider DERIVED value (ngrok API/SDK, cloudflared config)
// that is stale under a different provider and must be purged so the collector
// re-derives the correct endpoint.
//
// Per-provider key ownership (what to purge) and state-field ownership (what to
// scrub from the overlay) come from the provider registry — NEVER a switch on
// the provider value here.
func ReconcileServiceEnvironmentFromInstallState(envFile string, s *ServiceInstallState, prevProvider tunnel.TunnelProvider, explicitPublicURL bool) error {
	prevSet := prevProvider != ""
	switching := prevSet && s.Provider != "" && prevProvider != s.Provider
	// A deliberate switch to no-tunnel (localhost): the previous provider is on
	// disk but the operator decided an EMPTY provider this run. Only meaningful
	// when ProviderDecided is true — an UNDECIDED empty provider must not purge
	// anything, since no decision was made.
	downgradingToLocalhost := prevSet && s.ProviderDecided && s.Provider == ""

	overlay := serviceInstallStateToEnv(s)
	purge := switching || downgradingToLocalhost
	if len(overlay) == 0 && !purge {
		// Nothing to overlay AND nothing to purge: a strict no-op that must not
		// even require the env file to exist.
		return nil
	}
	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		return fmt.Errorf("load service environment %q for reconcile: %w", envFile, err)
	}

	if purge {
		// A provider switch must purge the previous provider's modeled keys so
		// the file carries no orphaned credentials for a tunnel that no longer
		// exists. Its derived MCP_PUBLIC_URL is purged too — unless explicitly
		// supplied this run, in which case the operator's decision wins.
		for _, k := range TunnelProviderEnvKeys(prevProvider) {
			delete(env, k)
		}
		// The provider identity key is always removed on a downgrade to
		// localhost (serviceInstallStateToEnv writes nothing for an empty
		// provider, so without this the stale MCP_TUNNEL_PROVIDER would keep
		// the service starting a tunnel). On a provider->provider switch the
		// overlay below writes the new value, overwriting it.
		delete(env, "MCP_TUNNEL_PROVIDER")
		if !explicitPublicURL {
			delete(env, "MCP_PUBLIC_URL")
		}
	}
	if switching {
		// Scrub the previous provider's fields out of the state so the overlay
		// below does not resurrect them (a seed fold loads every persisted
		// value, regardless of provider, into s).
		TunnelProviderCleanState(s.Provider, s)
		// MCP_PUBLIC_URL derived for the previous provider is never valid for
		// the new one. Clear it (so the collector re-derives under the new
		// provider) UNLESS the operator passed --public-url this run — an
		// explicit URL is provider-agnostic and must survive the switch.
		if !explicitPublicURL {
			s.PublicURL = ""
		}
		// Rebuild the overlay after the state scrub so it no longer carries the
		// previous provider's credentials.
		overlay = serviceInstallStateToEnv(s)
	}
	if downgradingToLocalhost {
		// Also scrub the provider-specific credential fields from the state so
		// the overlay cannot resurrect another tunnel's credentials under a
		// no-tunnel config. The shared auth token/MCP_OAUTH survive (they are
		// valid for a localhost OAuth serving), but the per-provider identity
		// and the derived URL do not.
		s.TunnelID = ""
		s.ApiKey = ""
		s.Domain = ""
		s.TunnelName = ""
		s.TunnelToken = ""
		if !explicitPublicURL {
			s.PublicURL = ""
		}
		overlay = serviceInstallStateToEnv(s)
	}
	// For the CURRENT provider, clear stale alternate-name spellings of
	// credentials the overlay models — e.g. an old NGROK_AUTHTOKEN next to the
	// new MCP_TUNNEL_TOKEN (ResolveNgrokToken prefers NGROK_AUTHTOKEN, so a
	// lingering old value would win over the operator's reconfiguration). An
	// alias is cleared ONLY when its canonical key is present in the overlay:
	// a legacy-only env file (just NGROK_AUTHTOKEN, no MCP_TUNNEL_TOKEN) never
	// has its only token removed. A distinct LIVE credential the state does not
	// persist (e.g. NGROK_API_KEY, read at collect time for URL resolution) is
	// not an alias and survives a same-provider re-run — it is only purged by a
	// switch away from the provider.
	for _, k := range TunnelProviderEnvKeys(s.Provider) {
		canon := TunnelProviderEnvKeyAlias(s.Provider, k)
		if canon != "" && overlay[canon] != "" {
			delete(env, k)
		}
	}
	for k, v := range overlay {
		env[k] = v
	}
	if err := service.WriteEnvironment(envFile, env); err != nil {
		return fmt.Errorf("reconcile MCP service environment file: %w", err)
	}
	return nil
}

// bootstrapServiceEnvironment writes a fresh 0600 env file from the tunnel
// config provided via flags. It requires MCP_TUNNEL_PROVIDER. cfgMgr, when
// non-nil, also persists any supplied ngrok/openai credential to the last-resort
// config-manager store so later runs auto-detect it.
func bootstrapServiceEnvironment(cmd *cli.Command, envFile string, cfgMgr config.Manager) error {
	provider, err := parseTunnelProvider(cmd.String(serviceTunnelFlag))
	if err != nil {
		if provider == "" {
			return errors.New("MCP service environment file does not exist; provide --tunnel (openai|ngrok|cloudflared) to bootstrap it")
		}
		return err
	}

	env, err := explicitServiceEnvFromFlags(cmd)
	if err != nil {
		return err
	}
	env["MCP_TUNNEL_PROVIDER"] = string(provider)
	// OAuth is the secure default for a public remote MCP endpoint: a brand-new
	// file defaults to enabling it (an explicit --oauth=false overrides via the
	// explicit overlay above). This ensures a headless `--tunnel` bootstrap does
	// not leave a public endpoint authenticated only by the shared token as a
	// Bearer.
	if _, set := env["MCP_OAUTH"]; !set {
		env["MCP_OAUTH"] = "true"
	}

	if err := service.WriteEnvironment(envFile, env); err != nil {
		return fmt.Errorf("bootstrap MCP service environment file: %w", err)
	}
	fmt.Printf("Created MCP service environment file %s (0600).\n", envFile)

	// Persist credentials supplied via flags to the last-resort store so the
	// values survive to later runs even if the env file is regenerated.
	if provider == tunnel.TunnelProviderOpenAI {
		tunnel.PersistTunnelCredential(cfgMgr, "openai", "tunnel_id", env["MCP_TUNNEL_ID"])
		tunnel.PersistTunnelCredential(cfgMgr, "openai", "api_key", env["CONTROL_PLANE_API_KEY"])
	} else if provider == tunnel.TunnelProviderNgrok {
		tunnel.PersistTunnelCredential(cfgMgr, "ngrok", "token", env["MCP_TUNNEL_TOKEN"])
	}
	return nil
}

// newManagedService builds the service for the given env file and provider.
// Env handling is entirely per-backend: the config carries only the env file
// path, and each platform file consumes it (systemd: EnvironmentFile, launchd:
// sources it via a wrapper, Windows SCM: loads it into the service registry).
func newManagedService(cmd *cli.Command, envFile string, provider tunnel.TunnelProvider) (service.Service, error) {
	cfg, err := serviceConfigForInstall(cmd, envFile, provider)
	if err != nil {
		return nil, err
	}
	// service.New auto-detects the host's init system from the per-platform
	// backend registry (systemd on Linux, launchd on macOS, SCM on Windows).
	return service.New(cfg)
}

// RestartManagedService restarts the managed MCP service so a freshly written
// MCP_AUTH_TOKEN in its env file takes effect on the running endpoint. It is a
// no-op when the install state carries no backing service to restart (no env
// file or provider). Callers gate on whether a managed service was actually
// started (e.g. effectiveManagedService) before invoking it. The caller's ctx
// is honored so an interrupted install (Ctrl-C) can cancel the restart.
func RestartManagedService(ctx context.Context, cmd *cli.Command, s *ServiceInstallState) error {
	if s == nil || s.EnvFile == "" || s.Provider == "" {
		return nil
	}
	svc, err := newManagedService(cmd, s.EnvFile, s.Provider)
	if err != nil {
		return err
	}
	return svc.Restart(ctx)
}

// serviceConfigForInstall builds the service.Config for the managed MCP
// service: the pinner executable run as `pinner mcp`, referencing the tunnel
// credentials via envFile (a path). Each platform backend chooses its own
// service-file path (systemd user unit on Linux, LaunchAgent plist on macOS,
// SCM on Windows), so no ServiceFile is pinned here, and loads the env file
// itself in its own platform file. Public HTTP tunnel providers (ngrok,
// cloudflared) pass --http so the server is exposed over HTTP; the embedded
// OpenAI tunnel speaks the MCP transport directly and must not add --http.
// Returns the pure config so tests can assert the ExecStart arguments without
// touching a live service backend.
func serviceConfigForInstall(cmd *cli.Command, envFile string, provider tunnel.TunnelProvider) (service.Config, error) {
	execPath, err := os.Executable()
	if err != nil {
		return service.Config{}, fmt.Errorf("resolve pinner executable: %w", err)
	}
	args := []string{"mcp"}
	// Localhost (empty provider) and public HTTP tunnel providers (ngrok,
	// cloudflared) expose the server over HTTP; the embedded OpenAI tunnel
	// speaks the transport directly, so it must not add --http.
	if provider != tunnel.TunnelProviderOpenAI {
		args = append(args, "--http")
	}
	return service.Config{
		Name:        defaultMCPServiceName,
		Description: "Pinner MCP service",
		ExecPath:    execPath,
		Arguments:   args,
		EnvFile:     envFile,
		UserMode:    true,
	}, nil
}

func expandServicePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func servicePort(env ServiceEnvironment) int {
	value := serviceEnvValue(env, "MCP_PORT", "0")
	port, _ := strconv.Atoi(value)
	return port
}
