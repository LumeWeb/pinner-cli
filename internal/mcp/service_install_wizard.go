package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// ServiceInstallState accumulates the tunnel configuration collected by the
// interactive install wizard and written to the service environment file.
type ServiceInstallState struct {
	EnvFile     string
	Provider    TunnelProvider
	TunnelID    string
	Domain      string
	TunnelName  string
	AuthToken   string
	TunnelToken string
	// NgrokAPIKey is the ngrok REST API key (distinct from the authtoken in
	// TunnelToken). It is config-time only: the install wizard uses it to query
	// the ngrok API for the account's public (dev/reserved) domain so it can
	// resolve PublicURL. It is never written to the service environment file.
	NgrokAPIKey string
	ApiKey      string
	PublicURL   string
	Host        string
	OAuth       bool
	Port        int

	// EnvFileCreated reports that the env file at EnvFile was freshly written
	// by this install run. A host wizard (mcp install) sets it in the flattened
	// path so CollectHTTPInstall's validation-failure cleanup fires even though
	// the file exists by the time the collector runs. A pre-existing env file
	// must never have this set.
	EnvFileCreated bool
}

// serviceInstallWizardUI renders progress and prompts using pterm, reusing the
// shared wizard step-runner (pkg/cli/wizard).
type serviceInstallWizardUI struct {
	*wizard.PTermUI
}

// NewServiceInstallWizardUI builds the pterm UI for the install wizard.
func NewServiceInstallWizardUI() *serviceInstallWizardUI {
	return &serviceInstallWizardUI{PTermUI: wizard.NewPTermUI("", "")}
}

// tunnelProviderChoiceLabels returns the ordered "label - descriptor" options
// for the tunnel-provider select, with the default provider (ngrok) FIRST so the
// interactive select highlights it. ngrok is default because it needs only an
// authtoken and no extra setup — the least-friction path for exposing the remote
// MCP endpoint. Keep the leading identifier before " - " equal to the provider
// token so the step can parse the selection back with parseTunnelProvider.
// Descriptors are written for an end user (what it gives you + what you must
// have), not implementation details. Single-sourced here so the wizard and any
// test share the list.
func tunnelProviderChoiceLabels() []string {
	return []string{
		"ngrok - a public URL, from an authtoken you get free at ngrok.com",
		"cloudflared - a public URL under your own Cloudflare domain",
		"openai - connects to ChatGPT/Connectors via an OpenAI Secure MCP Tunnel ID (needs a control-plane API key)",
	}
}

// Select runs an interactive single-choice prompt, gated by NonInteractive.
type selectUI struct{}

func (selectUI) Select(label string, options []string) (int, string, error) {
	if wizard.NonInteractive {
		return 0, "", errors.New("interactive prompt requested in non-interactive mode")
	}
	sel, _ := pterm.DefaultInteractiveSelect.WithOptions(options).WithDefaultText(label).Show()
	if sel == "" {
		return 0, "", errors.New("no option selected")
	}
	for i, o := range options {
		if o == sel {
			return i, sel, nil
		}
	}
	return 0, sel, nil
}

// Text runs an interactive text prompt (masked for sensitive values).
type textUI struct{ mask string }

func (t textUI) Text(label string) (string, error) {
	if wizard.NonInteractive {
		return "", errors.New("interactive prompt requested in non-interactive mode")
	}
	input := pterm.DefaultInteractiveTextInput.WithDefaultText(label)
	if t.mask != "" {
		input = input.WithMask(t.mask)
	}
	return input.Show()
}

// RunServiceInstallWizard drives the interactive tunnel configuration wizard and
// writes the resulting environment file. Flags and environment variables are
// seeded into the state up front; prompts only collect values the user has not
// already provided via flags/env (so --oauth/--public-url/--host/--port and
// MCP_AUTH_TOKEN are never silently dropped). cfgMgr, when non-nil, is used both
// to deep-link on missing provider credentials and to persist any credential the
// user enters so later runs auto-detect it (see set-up-once semantics).
func RunServiceInstallWizard(ctx context.Context, cmd *cli.Command, envFile string, cfgMgr config.Manager) error {
	ui := NewServiceInstallWizardUI()
	state := &ServiceInstallState{EnvFile: envFile}
	// Pre-seed scalar values from flags/env so the wizard never re-prompts for
	// something already explicit.
	seedServiceFromFlagsAndEnv(cmd, state, envFile)

	_, err := wizard.Run(ctx, ui, ServiceInstallSteps(state, cmd, envFile, cfgMgr), state)
	if err != nil {
		return err
	}
	return nil
}

// ServiceInstallSteps returns the ordered steps that collect and persist the
// MCP service (tunnel) configuration into state. It is exported so a host
// wizard (mcp install) can splice these steps directly into its own run and
// share its welcome, numbering, and completion, instead of nesting a second,
// independent wizard (which double-prints the continue prompt and renumbers
// from 1). RunServiceInstallWizard runs them standalone.
func ServiceInstallSteps(state *ServiceInstallState, cmd *cli.Command, envFile string, cfgMgr config.Manager) []wizard.Step[*ServiceInstallState] {
	return []wizard.Step[*ServiceInstallState]{
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Tunnel provider",
			ExecuteFunc: func(_ context.Context, s *ServiceInstallState) error {
				if s.Provider != "" {
					return nil
				}
				sel := selectUI{}
				// ngrok is listed first so the interactive select defaults to it (see
				// tunnelProviderChoiceLabels).
				_, choice, err := sel.Select("MCP tunnel provider (exposes the remote MCP endpoint)", tunnelProviderChoiceLabels())
				if err != nil {
					return err
				}
				// The select returns the descriptive label; the provider token is
				// the leading identifier before " - ".
				if i := strings.Index(choice, " - "); i > 0 {
					choice = choice[:i]
				}
				s.Provider, err = parseTunnelProvider(choice)
				return err
			},
		},
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Tunnel-specific configuration",
			ExecuteFunc: func(_ context.Context, s *ServiceInstallState) error {
				text := textUI{}
				// Dispatch provider-specific collection (IDs, domains,
				// credentials) through the provider registry's Configurer
				// instead of a switch on the provider value, so each provider
				// owns its install behaviour and the step stays provider-agnostic.
				if spec, ok := providers.spec(s.Provider); ok && spec.Configurer != nil {
					if err := spec.Configurer(context.Background(), text, s, cfgMgr); err != nil {
						return err
					}
				}
				// Prefer the MCP_AUTH_TOKEN environment variable over an
				// interactive prompt so the secret is never typed into or
				// echoed from the terminal session.
				if s.AuthToken == "" {
					secret, err := textUI{mask: "*"}.Text("Shared auth token / secret for the public MCP endpoint")
					if err != nil {
						return err
					}
					s.AuthToken = strings.TrimSpace(secret)
				}
				return nil
			},
		},
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Write service environment file",
			ExecuteFunc: func(_ context.Context, s *ServiceInstallState) error {
				seedServiceFromFlagsAndEnv(cmd, s, envFile)
				env := serviceInstallStateToEnv(s)
				if err := service.WriteEnvironment(s.EnvFile, env); err != nil {
					return fmt.Errorf("write MCP service environment file: %w", err)
				}
				return nil
			},
		},
	}
}

// SeedServiceFromFlagsAndEnv copies values the user already supplied via flags
// (which the framework resolves against each flag's declared env Sources) into
// the wizard state so the interactive prompts never overwrite or silently drop
// an explicit option. It must run BEFORE the tunnel-config steps so a
// --auth-token/--token/--domain (or MCP_AUTH_TOKEN/NGROK_AUTHTOKEN) provided on
// the command line is not re-prompted. Exported so the mcp install wizard can
// pre-seed the embedded service state it splices.
func SeedServiceFromFlagsAndEnv(cmd *cli.Command, s *ServiceInstallState, envFile string) {
	seedServiceFromFlagsAndEnv(cmd, s, envFile)
}

// seedServiceFromFlagsAndEnv is SeedServiceFromFlagsAndEnv's internal form.
func seedServiceFromFlagsAndEnv(cmd *cli.Command, s *ServiceInstallState, _ string) {
	if s.Provider == "" {
		if p, err := parseTunnelProvider(cmd.String(serviceTunnelFlag)); err == nil {
			s.Provider = p
		}
	}
	set := func(flag string, dst *string) {
		if *dst == "" {
			*dst = strings.TrimSpace(cmd.String(flag))
		}
	}
	set(serviceTunnelIDFlag, &s.TunnelID)
	set(serviceApiKeyFlag, &s.ApiKey)
	set(serviceDomainFlag, &s.Domain)
	set(serviceTunnelNameFlag, &s.TunnelName)
	set(serviceAuthTokenFlag, &s.AuthToken)
	set(serviceTunnelTokenFlag, &s.TunnelToken)
	set(serviceNgrokAPIKeyFlag, &s.NgrokAPIKey)
	set(servicePublicURLFlag, &s.PublicURL)
	set(serviceHostFlag, &s.Host)
	if cmd.IsSet(serviceOAuthFlag) {
		s.OAuth = cmd.Bool(serviceOAuthFlag)
	} else if strings.EqualFold(cmd.String(serviceOAuthFlag), "true") {
		s.OAuth = true
	}
	if cmd.IsSet(servicePortFlag) {
		s.Port = cmd.Int(servicePortFlag)
	} else if v := strings.TrimSpace(cmd.String(servicePortFlag)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Port = n
		}
	}
}

func serviceInstallStateToEnv(s *ServiceInstallState) ServiceEnvironment {
	env := ServiceEnvironment{"MCP_TUNNEL_PROVIDER": string(s.Provider)}
	if v := s.TunnelID; v != "" {
		env["MCP_TUNNEL_ID"] = v
	}
	if v := s.ApiKey; v != "" {
		env["CONTROL_PLANE_API_KEY"] = v
	}
	if v := s.Domain; v != "" {
		env["MCP_DOMAIN"] = v
	}
	if v := s.TunnelName; v != "" {
		env["MCP_TUNNEL_NAME"] = v
	}
	if v := s.AuthToken; v != "" {
		env["MCP_AUTH_TOKEN"] = v
	}
	// ngrok credential: written as MCP_TUNNEL_TOKEN (validateServiceEnvironment
	// accepts either NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN).
	if v := s.TunnelToken; v != "" {
		env["MCP_TUNNEL_TOKEN"] = v
	}
	if v := s.PublicURL; v != "" {
		env["MCP_PUBLIC_URL"] = v
	}
	if v := s.Host; v != "" {
		env["MCP_HOST"] = v
	}
	if s.OAuth {
		env["MCP_OAUTH"] = "true"
	}
	if s.Port != 0 {
		env["MCP_PORT"] = strconv.Itoa(s.Port)
	}
	return env
}
