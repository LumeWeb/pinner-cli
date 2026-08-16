package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/cloudflare"
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
	ApiKey      string
	PublicURL   string
	Host        string
	OAuth       bool
	Port        int
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
// MCP_AUTH_TOKEN are never silently dropped).
func RunServiceInstallWizard(ctx context.Context, cmd *cli.Command, envFile string) error {
	ui := NewServiceInstallWizardUI()
	state := &ServiceInstallState{EnvFile: envFile}
	// Pre-seed scalar values from flags/env so the wizard never re-prompts for
	// something already explicit.
	seedFromFlagsAndEnv(cmd, state, envFile)

	// If this wizard run provisions a fresh Cloudflare tunnel and a later step
	// fails (e.g. the env-file write), roll it back so a re-run is not blocked
	// by an orphaned tunnel + CNAME + stale state file. provisionedAPIKey is
	// the Cloudflare API token used at provision time (the tunnel-scoped run
	// token is not a valid API token for resource cleanup).
	var provisionedAPIKey string
	rollbackTunnel := func() {
		if provisionedAPIKey == "" {
			return
		}
		if st, lerr := LoadCloudflareTunnelState(); lerr == nil && st != nil {
			if c, cerr := cloudflare.New(provisionedAPIKey); cerr == nil {
				if st.DNSRecordID != "" {
					_ = c.DeleteDNSRoute(context.WithoutCancel(ctx), cloudflare.Zone{ID: st.ZoneID}, st.DNSRecordID)
				}
				_ = c.DeleteTunnel(context.WithoutCancel(ctx), cloudflare.Account{ID: st.AccountID}, st.TunnelID)
			}
			if p, perr := tunnelStatePath(); perr == nil {
				_ = os.Remove(p)
			}
		}
	}

	steps := []wizard.Step[*ServiceInstallState]{
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Tunnel provider",
			ExecuteFunc: func(_ context.Context, s *ServiceInstallState) error {
				if s.Provider != "" {
					return nil
				}
				sel := selectUI{}
				_, choice, err := sel.Select("MCP tunnel provider (exposes the remote MCP endpoint)", []string{
					string(TunnelProviderCloudflared),
					string(TunnelProviderNgrok),
					string(TunnelProviderOpenAI),
				})
				if err != nil {
					return err
				}
				s.Provider, err = parseTunnelProvider(choice)
				return err
			},
		},
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Tunnel-specific configuration",
			ExecuteFunc: func(_ context.Context, s *ServiceInstallState) error {
				text := textUI{}
				switch s.Provider {
				case TunnelProviderOpenAI:
					if s.TunnelID == "" {
						id, err := text.Text("OpenAI Secure MCP Tunnel ID")
						if err != nil {
							return err
						}
						s.TunnelID = strings.TrimSpace(id)
					}
					if !openAITunnelID.MatchString(s.TunnelID) {
						return fmt.Errorf("invalid OpenAI tunnel ID %q", s.TunnelID)
					}
					// The control-plane API key must be persisted to the file (the
					// running service reads only the env file, not this process's
					// environment). s.ApiKey is pre-seeded from the --api-key flag
					// (whose env Sources include CONTROL_PLANE_API_KEY/OPENAI_API_KEY).
					if s.ApiKey == "" {
						key, err := textUI{mask: "*"}.Text("OpenAI Secure MCP Tunnel control-plane API key")
						if err != nil {
							return err
						}
						s.ApiKey = strings.TrimSpace(key)
					}
				case TunnelProviderCloudflared:
					// Reuse an already-provisioned tunnel when one exists;
					// otherwise deep-link a token and provision the named
					// tunnel + DNS route via the SDK, persisting the scoped
					// credential.
					//
					// Gate on whether a provisioned tunnel actually exists
					// (LoadCloudflareTunnelState) rather than on s.Domain or
					// s.TunnelToken being empty: seedFromFlagsAndEnv pre-fills
					// s.Domain from the --domain flag (env MCP_DOMAIN) and
					// s.TunnelToken from --token (env MCP_TUNNEL_TOKEN /
					// NGROK_AUTHTOKEN), so a shell exporting either would
					// incorrectly skip both reuse and provisioning and leave
					// the environment without a tunnel.
					existing, lerr := LoadCloudflareTunnelState()
					if lerr == nil && existing != nil {
						// A tunnel is provisioned: reuse it rather than requiring
						// the user to re-pass --domain/MCP_DOMAIN. If the caller
						// explicitly requested a different hostname, fail loudly
						// rather than silently serving a domain that differs from
						// the one that was provisioned and validated.
						if s.Domain != "" && !strings.EqualFold(bareHostname(s.Domain), bareHostname(existing.Hostname)) {
							return fmt.Errorf("requested domain %q does not match provisioned tunnel hostname %q; re-run `pinner mcp tunnel install --domain %s` to re-provision", s.Domain, existing.Hostname, s.Domain)
						}
						s.Domain = existing.Hostname
						s.TunnelName = existing.TunnelName
						s.TunnelToken = existing.Token
					} else if errors.Is(lerr, os.ErrNotExist) || (lerr == nil && existing == nil) {
						// No tunnel provisioned yet (no persisted state, or a
						// nil error with nil state): provision a new one.
						state, apiKey, perr := provisionCloudflaredForWizard(ctx, cmd, cloudflare.New)
						if perr != nil {
							return perr
						}
						s.Domain = state.Hostname
						s.TunnelName = state.TunnelName
						s.TunnelToken = state.Token
						// Remember the API token so the env-write step can roll
						// the tunnel back if it fails.
						provisionedAPIKey = apiKey
					} else {
						// Existing state exists but is corrupt or unreadable
						// (parse failure, permission, I/O). Surface it rather
						// than silently provisioning a fresh tunnel that would
						// orphan the existing tunnel and its DNS CNAME.
						return fmt.Errorf("load provisioned Cloudflare tunnel state: %w", lerr)
					}
				case TunnelProviderNgrok:
					if s.TunnelName == "" {
						name, err := text.Text("Tunnel resource name (optional)")
						if err != nil {
							return err
						}
						s.TunnelName = strings.TrimSpace(name)
					}
					// ngrok validation requires NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN;
					// s.TunnelToken is pre-seeded from the --token flag (whose env
					// Sources include both vars), so only prompt when it is empty so
					// the written env file actually passes validation.
					if s.TunnelToken == "" {
						tok, err := textUI{mask: "*"}.Text("ngrok authtoken / MCP tunnel token")
						if err != nil {
							return err
						}
						s.TunnelToken = strings.TrimSpace(tok)
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
				seedFromFlagsAndEnv(cmd, s, envFile)
				env := serviceInstallStateToEnv(s)
				if err := WriteServiceEnvironment(s.EnvFile, env); err != nil {
					// A fresh cloudflared tunnel may have been provisioned in
					// the previous step; roll it back so a failed env write
					// does not orphan the tunnel + CNAME + state file.
					rollbackTunnel()
					return fmt.Errorf("write MCP service environment file: %w", err)
				}
				return nil
			},
		},
	}

	_, err := wizard.Run(ctx, ui, steps, state)
	if err != nil {
		return err
	}
	return nil
}

// seedFromFlagsAndEnv copies values the user already supplied via flags (which
// the framework resolves against each flag's declared env Sources) into the
// wizard state so the interactive prompts never overwrite or silently drop an
// explicit option.
func seedFromFlagsAndEnv(cmd *cli.Command, s *ServiceInstallState, _ string) {
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
