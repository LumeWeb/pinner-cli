//go:build !no_tunnel

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// ServiceInstallState accumulates the tunnel configuration collected by the
// interactive install wizard and written to the service environment file.
type ServiceInstallState struct {
	EnvFile     string
	Provider    tunnel.TunnelProvider
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
	// OAuth is the operator's decision on the OAuth 2.1 handshake for the
	// remote MCP endpoint. It is tri-state: nil means OAuth was NOT decided
	// this run; &true/&false is an explicit decision. The serializer persists
	// MCP_OAUTH only when non-nil, and the env-fold seeds a persisted value
	// only when nil — so an undecided standalone install omits the key (letting
	// the runtime secure default apply) while an explicit opt-out is preserved.
	OAuth *bool
	// Port is the operator's decision on the MCP port. Tri-state like OAuth:
	// nil means undecided; &N (including &0, the "pick a free port" sentinel) is
	// an explicit decision. MCP_PORT is persisted only when non-nil.
	Port *int
	// DevTools mirrors the `pinner mcp` serve --dev-tools switch. Tri-state like
	// OAuth: nil means undecided; &true/&false is an explicit decision. The
	// serializer persists MCP_DEV_TOOLS only when non-nil, so an undecided
	// install omits the key (leaving the runtime default) while an explicit
	// opt-in/opt-out is preserved across re-runs.
	DevTools *bool

	// EnvFileCreated reports that the env file at EnvFile was freshly written
	// by this install run. A host wizard (mcp install) sets it in the flattened
	// path so CollectHTTPInstall's validation-failure cleanup fires even though
	// the file exists by the time the collector runs. A pre-existing env file
	// must never have this set.
	EnvFileCreated bool

	// ProviderDecided reports that the tunnel provider was fixed by a decisive
	// source this run: an explicit --tunnel switch, a headless selection, or an
	// interactive select. It exists because "no tunnel" (localhost) is
	// represented by the EMPTY provider string, which is otherwise identical to
	// "undecided". Without it, a seed fold of a persisted MCP_TUNNEL_PROVIDER
	// from a prior install would clobber an operator's explicit localhost choice
	// back to the old tunnel provider, and a reconcile could not tell a
	// deliberate downgrade-to-localhost from an untouched state. It mirrors the
	// tri-state "decided this run" discipline used for OAuth/Port/DevTools, but
	// as a plain bool because a decided value of "localhost" is still empty.
	ProviderDecided bool

	// decisions is the Decided channel of the two-channel provenance model (see
	// fieldform). For each install field it records an operator decision made
	// this run via a CLI switch or prompt, distinct from the flat Operational
	// field (which may also hold a value folded from the env file or derived by
	// a provider). A missing key means the field was not decided this run. Keyed
	// by the field's stable Name string. See service_install_fields.go.
	decisions map[string]*string
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
		"localhost - serve on your machine, no public URL or tunnel needed",
		"ngrok - a public URL, from an authtoken you get free at ngrok.com",
		"cloudflared - a public URL under your own Cloudflare domain",
		"openai - connects to ChatGPT/Connectors via an OpenAI Secure MCP Tunnel ID (needs a control-plane API key)",
	}
}

// providerChoiceLabel returns the full choice label for a provider token (the
// text the interactive Select highlights as the current default on a re-run),
// or "" if the provider is unknown/unset. It derives from
// tunnelProviderChoiceLabels — the single source of the label strings — by
// matching the leading provider token (the identifier before " - "), so the
// labels never drift between the option list and the highlighted default.
func providerChoiceLabel(p tunnel.TunnelProvider) string {
	prefix := "localhost - "
	if p != "" {
		prefix = string(p) + " - "
	}
	for _, label := range tunnelProviderChoiceLabels() {
		if strings.HasPrefix(label, prefix) {
			return label
		}
	}
	return ""
}

// Select runs an interactive single-choice prompt, gated by NonInteractive.
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
	// Bind the pterm prompter so the wizard's steps ask the user through the
	// shared prompt channel (like any other wizard), never via private widgets.
	ctx = fieldform.WithPrompter(ctx, wizard.NewPtermPrompter())
	// Pre-seed scalar values from flags/env so the wizard never re-prompts for
	// something already explicit.
	seedServiceFromFlagsAndEnv(cmd, state, envFile)

	_, err := wizard.Run(ctx, ui, ServiceInstallSteps(state, cmd, envFile, cfgMgr), state)
	if err != nil {
		return err
	}
	return nil
}

// serviceInstallStepsPrompter returns the prompt channel bound to ctx, or a
// nil-guard prompter when none is bound. A step only *reaches* the prompter when
// it genuinely needs user input; when no channel is bound (e.g. a direct step
// drive in a non-interactive test where every value resolves from config/env),
// the missing channel surfaces as a clear error only if a prompt is attempted.
func serviceInstallStepsPrompter(ctx context.Context) fieldform.Prompter {
	if p := fieldform.PrompterFrom(ctx); p != nil {
		return p
	}
	return nilPrompt{}
}

// nilPrompt is a fieldform.Prompter that errors on any method call: a step reached
// it meaning it needs input, but no prompt channel is bound to the run context.
type nilPrompt struct{}

func (nilPrompt) Select(string, []string, string) (int, string, error) {
	return 0, "", errors.New("interactive prompt requested but no prompt channel is bound")
}
func (nilPrompt) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, errors.New("interactive prompt requested but no prompt channel is bound")
}
func (nilPrompt) Confirm(string, bool) (bool, error) {
	return false, errors.New("interactive prompt requested but no prompt channel is bound")
}
func (nilPrompt) Text(string, string, string) (string, error) {
	return "", errors.New("interactive prompt requested but no prompt channel is bound")
}

// flagSetOnCmdLine reports whether --<name> was passed explicitly on the
// command line. It scans the raw process arguments because
// (*cli.Command).IsSet(name) is also true when the flag is sourced from a
// declared env var (Sources: ...), and urfave/cli v3 exposes no CLI-vs-env
// distinction. A persisted env value is inherited configuration, not an
// operator decision for this run — it only PREFILLS the value, it must never
// hard-decide (or re-clobber) it. Only a literal --flag token on the command
// line counts as a decision. Single shared helper for every flag that needs
// the CLI-vs-env distinction (currently --tunnel and --oauth).
func flagSetOnCmdLine(name string) bool {
	flag := "--" + name
	for _, a := range os.Args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// OAuthFlagSetOnCmdLine reports whether --oauth was passed explicitly on the
// command line (see flagSetOnCmdLine). Kept exported for the cli package, which
// asserts it directly.
func OAuthFlagSetOnCmdLine() bool { return flagSetOnCmdLine(serviceOAuthFlag) }

// tunnelFlagSetOnCmdLine reports whether --tunnel was passed explicitly on the
// command line (see flagSetOnCmdLine). Only an explicit CLI switch hard-decides
// the provider; a persisted MCP_TUNNEL_PROVIDER env value merely prefills.
func tunnelFlagSetOnCmdLine() bool { return flagSetOnCmdLine(serviceTunnelFlag) }

// promptOAuthForInstall asks the operator about enabling the OAuth 2.1
// handshake. Uses the shared prompter channel so both mcp install and the
// standalone service install wizard ask through the same terminal. Skipped
// in non-interactive mode and when --oauth was explicitly passed on the
// command line (an explicit operator decision that seeds the tri-state
// directly).
func promptOAuthForInstall(ctx context.Context, s *ServiceInstallState) error {
	if fieldform.NonInteractive {
		return nil
	}
	if OAuthFlagSetOnCmdLine() {
		return nil
	}
	p := serviceInstallStepsPrompter(ctx)
	assumed := true // secure default-on for a remote endpoint
	if s.OAuth != nil {
		assumed = *s.OAuth
	}
	enabled, err := p.Confirm("Enable the OAuth 2.1 handshake for OAuth-expecting MCP clients (ChatGPT, Claude.ai, Copilot, Vertex)?", assumed)
	if err != nil {
		return err
	}
	s.OAuth = &enabled
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
			ExecuteFunc: func(ctx context.Context, s *ServiceInstallState) error {
				// Headless: reuse a resolved provider instead of prompting. It is
				// already fixed (from a --tunnel switch or a persisted env value a
				// headless run reuses), so mark it decided to keep later seed
				// folds from re-deciding it.
				if s.Provider != "" && fieldform.NonInteractive {
					s.ProviderDecided = true
					return nil
				}
				p := serviceInstallStepsPrompter(ctx)
				// ngrok is listed first so the interactive select defaults to it (see
				// tunnelProviderChoiceLabels); on an interactive re-run the current
				// provider is highlighted so the operator can keep or change it.
				_, choice, err := p.Select("MCP tunnel provider (exposes the remote MCP endpoint)", tunnelProviderChoiceLabels(), providerChoiceLabel(s.Provider))
				if err != nil {
					return err
				}
				// The select returns the descriptive label; the provider token is
				// the leading identifier before " - ".
				if i := strings.Index(choice, " - "); i > 0 {
					choice = choice[:i]
				}
				s.Provider, err = parseTunnelProvider(choice)
				if err == nil {
					// The operator made an explicit choice this run — including the
					// empty provider (localhost). Mark it decided so a later seed
					// fold of a persisted MCP_TUNNEL_PROVIDER cannot clobber it.
					s.ProviderDecided = true
				}
				return err
			},
		},
		wizard.StepFunc[*ServiceInstallState]{
			Name_: "Tunnel-specific configuration",
			ExecuteFunc: func(ctx context.Context, s *ServiceInstallState) error {
				// Localhost (no tunnel): skip credentials, just prompt for OAuth.
				if s.Provider == "" {
					return promptOAuthForInstall(ctx, s)
				}

				spec, ok := providers.spec(s.Provider)
				p := serviceInstallStepsPrompter(ctx)

				// Resolve the provider's field set — provider fields PLUS the
				// shared auth token — through the fieldform.Gather primitive,
				// applying one precedence model (switch > existing decision >
				// headless env fold, prompting with the current value as an
				// editable default) instead of hand-rolled `if s.X == ""`
				// prompts.
				if ok && spec.Fields != nil {
					src := newServiceInstallValueSource(cmd, envFile)

					// Open the provider's setup deep-links for any credential
					// that is still unresolved, so the browser is ready before
					// Gather prompts for it (the preprompt UX the imperative
					// configurers provided).
					fireProviderDeepLinks(s.Provider, s)

					fields := append([]fieldform.Field[*ServiceInstallState, string]{}, spec.Fields(ctx, s, cfgMgr)...)

					// The shared auth token, preferred from MCP_AUTH_TOKEN (env
					// fold) over an interactive prompt so the secret is never
					// typed into or echoed from the terminal session.
					auth := *installFieldByName("AuthToken")
					auth.Prompt = promptText("Shared auth token / secret for the public MCP endpoint", "*")
					fields = append(fields, auth)

					if _, _, err := fieldform.Gather(ctx, src, s, fields); err != nil {
						return err
					}

					// Post-Gather provider side-effects (derived values, last-
					// resort persistence). Not field-shaped, so it lives here.
					if spec.Finalize != nil {
						if err := spec.Finalize(ctx, p, s, cfgMgr); err != nil {
							return err
						}
					}
					return promptOAuthForInstall(ctx, s)
				}

				// Prefer the MCP_AUTH_TOKEN environment variable over an
				// interactive prompt so the secret is never typed into or
				// echoed from the terminal session.
				if s.AuthToken == "" {
					secret, err := p.Text("Shared auth token / secret for the public MCP endpoint", "*", "")
					if err != nil {
						return err
					}
					s.AuthToken = strings.TrimSpace(secret)
				}
				return promptOAuthForInstall(ctx, s)
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

// IsServiceInstallSeeded reports whether the service state already carries every
// value the tunnel-config step would collect, so a host wizard (mcp install)
// can render the step "Seeded" and skip its Execute (which would otherwise
// prompt through the provider's Fields and collect the shared auth token —
// aborting a non-interactive --service --tunnel bootstrap on a stray prompt).
//
// This delegates to the provider's registered ConfigSeeded predicate in the
// tunnel registry — the SINGLE source of per-provider completeness, kept next
// to each provider's Fields/Finalize instead of a switch on the provider value
// in the host. Every requirement the install flow would prompt for must be
// present (the provider's own credentials, the shared auth token every public
// tunnel needs, any value that only the provider derives such as an ngrok
// public URL, and shape validation such as the OpenAI tunnel-ID format), or
// the step stays un-seeded and prompts.
func IsServiceInstallSeeded(s *ServiceInstallState) bool {
	if s == nil {
		return false
	}
	return TunnelProviderConfigSeeded(s.Provider, s)
}

// seedServiceFromFlagsAndEnv is SeedServiceFromFlagsAndEnv's internal form.
func seedServiceFromFlagsAndEnv(cmd *cli.Command, s *ServiceInstallState, _ string) {
	// Resolve the provider, honoring the two-channel provenance discipline used
	// everywhere in the install flows: a provider the operator already decided
	// this run (ProviderDecided — possibly localhost, the empty value) is never
	// clobbered. seedServiceFromFlagsAndEnv is re-invoked before EVERY wrapped
	// tunnel step, so without this guard an env-sourced MCP_TUNNEL_PROVIDER
	// would resurrect a stale tunnel over the operator's localhost choice.
	if !s.ProviderDecided {
		if tunnelFlagSetOnCmdLine() {
			// An explicit --tunnel CLI switch is a hard decision for this run.
			if p, err := parseTunnelProvider(cmd.String(serviceTunnelFlag)); err == nil {
				s.Provider = p
				s.ProviderDecided = true
			}
		} else if s.Provider == "" {
			// No CLI switch: fold a persisted MCP_TUNNEL_PROVIDER only while
			// undecided, as a prefill the interactive select can still change.
			if p, err := parseTunnelProvider(cmd.String(serviceTunnelFlag)); err == nil {
				s.Provider = p
			}
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
		v := cmd.Bool(serviceOAuthFlag)
		s.OAuth = &v
	} else if strings.EqualFold(cmd.String(serviceOAuthFlag), "true") {
		v := true
		s.OAuth = &v
	}
	if cmd.IsSet(serviceDevToolsFlag) {
		v := cmd.Bool(serviceDevToolsFlag)
		s.DevTools = &v
	} else if strings.EqualFold(cmd.String(serviceDevToolsFlag), "true") {
		v := true
		s.DevTools = &v
	}
	if cmd.IsSet(servicePortFlag) {
		n := cmd.Int(servicePortFlag)
		s.Port = &n
	} else if v := strings.TrimSpace(cmd.String(servicePortFlag)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Port = &n
		}
	}
}

func serviceInstallStateToEnv(s *ServiceInstallState) ServiceEnvironment {
	env := ServiceEnvironment{}
	// Provider is written only when decided; an undecided (empty) provider must
	// not write MCP_TUNNEL_PROVIDER= (which would clobber a persisted value on
	// a re-run reconcile or write a meaningless empty line on a fresh install).
	if s.Provider != "" {
		env["MCP_TUNNEL_PROVIDER"] = string(s.Provider)
	}
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
	// MCP_OAUTH / MCP_PORT are persisted only when the operator DECIDED them
	// this run (tri-state: non-nil). An undecided value is omitted so the key
	// is preserved as-is in an existing file, or left to the runtime secure
	// default (authentication on) on a fresh file. A nil-false or nil-zero is
	// a legitimate decision (opt-out / --port 0) and is written verbatim.
	if s.OAuth != nil {
		env["MCP_OAUTH"] = strconv.FormatBool(*s.OAuth)
	}
	if s.DevTools != nil {
		env["MCP_DEV_TOOLS"] = strconv.FormatBool(*s.DevTools)
	}
	if s.Port != nil {
		env["MCP_PORT"] = strconv.Itoa(*s.Port)
	}
	return env
}
