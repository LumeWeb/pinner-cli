package services

import (
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// The MCP service-install fields are declared declaratively as wizard.Field so
// the tunnel-config step resolves each one with the framework's single
// precedence and provenance model (switch > existing decision > headless env
// fold) instead of imperative `if s.X == "" || !wizard.NonInteractive`
// prompts.
//
// Two-channel provenance per field (see wizard/field.go):
//
//	Decided     — an operator decision this run (CLI switch or prompt). A
//	              missing entry means "not decided this run", even when the
//	              flat Operational field holds a value folded from the env
//	              file. Survives a provider switch and serializes verbatim.
//	Operational — the current working value (env-folded or provider-derived).
//	              Not an operator decision. Cleared on a provider switch for
//	              ReDerives fields (the new provider re-derives them).
//
// The flat public fields on ServiceInstallState (TunnelID, Domain, ...) carry
// the Operational channel; the private decisions map carries the Decided
// channel.

// tunnelFieldKey enumerates the tunnel install fields.
type tunnelFieldKey int

const (
	fieldTunnelID tunnelFieldKey = iota
	fieldApiKey
	fieldDomain
	fieldTunnelName
	fieldAuthToken
	fieldTunnelToken
	fieldNgrokAPIKey
	fieldPublicURL
	fieldHost
)

// tunnelFields is the ordered list of all install fields.
var tunnelFields = []tunnelFieldKey{
	fieldTunnelID,
	fieldApiKey,
	fieldDomain,
	fieldTunnelName,
	fieldAuthToken,
	fieldTunnelToken,
	fieldNgrokAPIKey,
	fieldPublicURL,
	fieldHost,
}

// operational reads the flat Operational value for a field.
func (s *ServiceInstallState) operational(k tunnelFieldKey) string {
	switch k {
	case fieldTunnelID:
		return s.TunnelID
	case fieldApiKey:
		return s.ApiKey
	case fieldDomain:
		return s.Domain
	case fieldTunnelName:
		return s.TunnelName
	case fieldAuthToken:
		return s.AuthToken
	case fieldTunnelToken:
		return s.TunnelToken
	case fieldNgrokAPIKey:
		return s.NgrokAPIKey
	case fieldPublicURL:
		return s.PublicURL
	case fieldHost:
		return s.Host
	}
	return ""
}

// setOperational writes the flat Operational value for a field. It does not
// affect the Decided channel.
func (s *ServiceInstallState) setOperational(k tunnelFieldKey, v string) {
	switch k {
	case fieldTunnelID:
		s.TunnelID = v
	case fieldApiKey:
		s.ApiKey = v
	case fieldDomain:
		s.Domain = v
	case fieldTunnelName:
		s.TunnelName = v
	case fieldAuthToken:
		s.AuthToken = v
	case fieldTunnelToken:
		s.TunnelToken = v
	case fieldNgrokAPIKey:
		s.NgrokAPIKey = v
	case fieldPublicURL:
		s.PublicURL = v
	case fieldHost:
		s.Host = v
	}
}

// decided reads the Decided pointer for a field (nil = not decided this run).
func (s *ServiceInstallState) decided(k tunnelFieldKey) *string {
	if s == nil || s.decisions == nil {
		return nil
	}
	return s.decisions[k]
}

// commitDecided records an operator decision value for a field in both
// channels (the flat Operational value and the Decided map).
func (s *ServiceInstallState) commitDecided(k tunnelFieldKey, v string) {
	if s == nil {
		return
	}
	if s.decisions == nil {
		s.decisions = make(map[tunnelFieldKey]*string)
	}
	copied := v
	s.decisions[k] = &copied
	s.setOperational(k, v)
}

// unmarkDecided drops a field's decision (used when a value should no longer
// be treated as an operator decision, e.g. on a provider switch).
func (s *ServiceInstallState) unmarkDecided(k tunnelFieldKey) {
	if s == nil || s.decisions == nil {
		return
	}
	delete(s.decisions, k)
}

// reDerives reports whether a field is re-derived by the provider on a switch
// (its Operational value is stale after switching providers).
func reDerives(k tunnelFieldKey) bool {
	switch k {
	case fieldPublicURL, fieldTunnelToken:
		return true
	}
	return false
}

// envKey returns the persisted env-file key a field folds from ("" = not
// persisted). NgrokAPIKey is config-time only and is never persisted.
func envKey(k tunnelFieldKey) string {
	switch k {
	case fieldTunnelID:
		return "MCP_TUNNEL_ID"
	case fieldApiKey:
		return "CONTROL_PLANE_API_KEY"
	case fieldDomain:
		return "MCP_DOMAIN"
	case fieldTunnelName:
		return "MCP_TUNNEL_NAME"
	case fieldAuthToken:
		return "MCP_AUTH_TOKEN"
	case fieldTunnelToken:
		return "MCP_TUNNEL_TOKEN"
	case fieldPublicURL:
		return "MCP_PUBLIC_URL"
	case fieldHost:
		return "MCP_HOST"
	}
	return ""
}

// tunnelFieldFlag maps a field to its CLI switch ("" = no switch). NgrokAPIKey
// is only reachable via --ngrok-api-key.
var tunnelFieldFlag = map[tunnelFieldKey]string{
	fieldTunnelID:    serviceTunnelIDFlag,
	fieldApiKey:      serviceApiKeyFlag,
	fieldDomain:      serviceDomainFlag,
	fieldTunnelName:  serviceTunnelNameFlag,
	fieldAuthToken:   serviceAuthTokenFlag,
	fieldTunnelToken: serviceTunnelTokenFlag,
	fieldNgrokAPIKey: serviceNgrokAPIKeyFlag,
	fieldPublicURL:   servicePublicURLFlag,
	fieldHost:        serviceHostFlag,
}

// fieldName returns a stable, human-readable name for a field.
func fieldName(k tunnelFieldKey) string {
	switch k {
	case fieldTunnelID:
		return "TunnelID"
	case fieldApiKey:
		return "ApiKey"
	case fieldDomain:
		return "Domain"
	case fieldTunnelName:
		return "TunnelName"
	case fieldAuthToken:
		return "AuthToken"
	case fieldTunnelToken:
		return "TunnelToken"
	case fieldNgrokAPIKey:
		return "NgrokAPIKey"
	case fieldPublicURL:
		return "PublicURL"
	case fieldHost:
		return "Host"
	}
	return ""
}

// tunnelInstallField builds the framework Field view for one tunnel install
// state field, wiring each accessor to the two-channel state so Gather can
// resolve it declaratively. Prompt/Options/Validate are intentionally omitted
// here; callers attach a provider-specific Prompt and Validate per field as
// needed.
func tunnelInstallField(k tunnelFieldKey) *wizard.Field[*ServiceInstallState, string] {
	f := &wizard.Field[*ServiceInstallState, string]{
		Name:           fieldName(k),
		Flag:           tunnelFieldFlag[k],
		ReDerives:      reDerives(k),
		Parse:          func(raw string) (string, bool) { return raw, true },
		Decided:        func(s *ServiceInstallState) *string { return s.decided(k) },
		Commit:         func(s *ServiceInstallState, v string) { s.commitDecided(k, v) },
		Operational:    func(s *ServiceInstallState) string { return s.operational(k) },
		SetOperational: func(s *ServiceInstallState, v string) { s.setOperational(k, v) },
	}
	if ek := envKey(k); ek != "" {
		f.EnvFileKey = ek
	}
	return f
}

// ClearReDerivedForProvider clears every ReDerives field's Operational value
// (and any matching decision) so a provider switch leaves no stale cross-
// provider credential or endpoint. Returns true if any value was cleared. The
// new provider's flow re-derives them.
func (s *ServiceInstallState) ClearReDerivedForProvider() bool {
	if s == nil {
		return false
	}
	cleared := false
	for _, k := range tunnelFields {
		if !reDerives(k) {
			continue
		}
		if s.operational(k) != "" {
			s.setOperational(k, "")
			cleared = true
		}
		s.unmarkDecided(k)
	}
	return cleared
}

// serviceInstallValueSource adapts the host's CLI flags and persisted env file
// to the wizard.ValueSource interface so wizard.Gather can fold both into the
// two-channel state. A nil envFile (or empty env) yields no env fold.
type serviceInstallValueSource struct {
	envFile string
	flags   func(name string) (string, bool)
}

func (v *serviceInstallValueSource) Flag(name string) (string, bool) {
	if v.flags != nil {
		return v.flags(name)
	}
	return "", false
}

func (v *serviceInstallValueSource) EnvFile(key string) (string, bool) {
	if v.envFile == "" {
		return "", false
	}
	env, err := service.LoadEnvironment(v.envFile)
	if err != nil {
		return "", false
	}
	val, ok := env[key]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(val), true
}

// serviceInstallFlags adapts the host's urfave/cli command into the
// serviceInstallValueSource Flag accessor: a flag is present when it was
// explicitly set OR its (possibly process-env-sourced) value is non-empty. A
// present flag is an operator decision for wizard.Gather precedence 1.
func serviceInstallFlags(cmd *cli.Command) func(name string) (string, bool) {
	return func(name string) (string, bool) {
		if cmd == nil {
			return "", false
		}
		v := strings.TrimSpace(cmd.String(name))
		if v == "" && !cmd.IsSet(name) {
			return "", false
		}
		return v, true
	}
}

// newServiceInstallValueSource builds the install flow's wizard.ValueSource
// from the host command (its flags, incl. process-env Sources) and the persisted
// env file. Gather resolves each field's Flag against the command and its
// EnvFileKey against the env file.
func newServiceInstallValueSource(cmd *cli.Command, envFile string) *serviceInstallValueSource {
	return &serviceInstallValueSource{
		envFile: envFile,
		flags:   serviceInstallFlags(cmd),
	}
}
