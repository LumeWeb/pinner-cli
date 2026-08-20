package services

import (
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// The MCP service-install fields are declared declaratively through the shared
// fieldform functional constructors (Str). Each field is one self-contained
// declaration wiring the two-channel provenance model:
//
//	Decided     — an operator decision this run (CLI switch or prompt), kept in
//	              ServiceInstallState.decisions keyed by the field's Name. A
//	              missing entry means "not decided this run", distinct from the
//	              flat Operational field below.
//	Operational — the current working value (env-folded or provider-derived),
//	              held in the flat public fields. Cleared on provider switch for
//	              ReDerives fields.
//
// The flat public fields on ServiceInstallState (TunnelID, Domain, ...) carry
// the Operational channel; the private decisions map (keyed by Name) carries the
// Decided channel. The old tunnelFieldKey enum + seven parallel switch/map
// tables are replaced by one ordered registry (installFieldByNameKey built from
// fieldform.Str); adding a field is one entry.
//
// decisions channel: a single fieldform.Decided binding over the name-keyed map,
// reused by every Str field so env-fold stays undecided while a switched or
// prompted value is decided.

// ---- Two-channel accessors on the state ----

// decidedFor reads the Decided pointer for a field by name (nil = not decided).
func (s *ServiceInstallState) decidedFor(name string) *string {
	if s == nil || s.decisions == nil {
		return nil
	}
	return s.decisions[name]
}

// commitDecided records an operator decision for a named field in both channels:
// the Decided map, and (via the registry's field Setter) the Operational value.
func (s *ServiceInstallState) commitDecided(name, v string) {
	if s == nil {
		return
	}
	if s.decisions == nil {
		s.decisions = make(map[string]*string)
	}
	copied := v
	s.decisions[name] = &copied
	if f := installFieldByNameKey[name]; f != nil {
		f.SetOperational(s, v)
	}
}

// unmarkDecided drops a field's decision (value stays; used on provider switch).
func (s *ServiceInstallState) unmarkDecided(name string) {
	if s == nil || s.decisions == nil {
		return
	}
	delete(s.decisions, name)
}

// decisionsBinding is the shared Decided channel for all install fields: a
// name-keyed map on the state. Write records only the decision map; the field's
// Str Commit applies the value write via its own Setter.
var decisionsBinding = fieldform.Decided[*ServiceInstallState, string]{
	Read:  func(s *ServiceInstallState, name string) *string { return s.decidedFor(name) },
	Write: func(s *ServiceInstallState, name, v string) { s.decidedFor(name); s.commitDecidedMap(name, v) },
}

// oauthBinding and portBinding are the Decided channels for the pointer-typed
// OAuth/Port fields. They reuse the same name-keyed string map as decisionsBinding,
// serializing the bool/int decision to/from the string entry. The names "OAuth"
// and "Port" are unique across the field set, so the serialized form never reads
// back as a different field's type.
var oauthBinding = fieldform.Decided[*ServiceInstallState, bool]{
	Read: func(s *ServiceInstallState, name string) *bool {
		r := s.decidedFor(name)
		if r == nil {
			return nil
		}
		v := *r == "true"
		return &v
	},
	Write: func(s *ServiceInstallState, name string, v bool) {
		s.commitDecidedMap(name, strconv.FormatBool(v))
	},
}

var portBinding = fieldform.Decided[*ServiceInstallState, int]{
	Read: func(s *ServiceInstallState, name string) *int {
		r := s.decidedFor(name)
		if r == nil {
			return nil
		}
		v, err := strconv.Atoi(*r)
		if err != nil {
			return nil
		}
		return &v
	},
	Write: func(s *ServiceInstallState, name string, v int) {
		s.commitDecidedMap(name, strconv.Itoa(v))
	},
}

// commitDecidedMap records only the Decided map entry (no value write) — used by
// the Str Commit path, which applies the value write through the field Setter.
func (s *ServiceInstallState) commitDecidedMap(name, v string) {
	if s == nil {
		return
	}
	if s.decisions == nil {
		s.decisions = make(map[string]*string)
	}
	copied := v
	s.decisions[name] = &copied
}

// ---- The ordered field registry ----

// installField is the per-field functional factory: the caller declares only the
// typed Operational accessors (get/set), the Name, and the declarative Meta;
// fieldform.Str derives Parse, Decide (via decisionsBinding), Commit, Prompt and
// the JSON-schema entry. It returns the already-erased AnyField.
func installField(get func(*ServiceInstallState) string, set func(*ServiceInstallState, string), name, flag, envKey string, reDerives bool) fieldform.AnyField[*ServiceInstallState] {
	return fieldform.Str(decisionsBinding, name, get, set, fieldform.Meta{
		Flag: flag, EnvFileKey: envKey, ReDerives: reDerives,
	})
}

// installFieldEntries is the single source of truth: the ordered, declarative
// field set. Adding a field is one installField call here.
func installFieldEntries() []fieldform.AnyField[*ServiceInstallState] {
	return []fieldform.AnyField[*ServiceInstallState]{
		installField(func(s *ServiceInstallState) string { return s.TunnelID }, func(s *ServiceInstallState, v string) { s.TunnelID = v }, "TunnelID", serviceTunnelIDFlag, "MCP_TUNNEL_ID", false),
		installField(func(s *ServiceInstallState) string { return s.ApiKey }, func(s *ServiceInstallState, v string) { s.ApiKey = v }, "ApiKey", serviceApiKeyFlag, "CONTROL_PLANE_API_KEY", false),
		installField(func(s *ServiceInstallState) string { return s.Domain }, func(s *ServiceInstallState, v string) { s.Domain = v }, "Domain", serviceDomainFlag, "MCP_DOMAIN", false),
		installField(func(s *ServiceInstallState) string { return s.TunnelName }, func(s *ServiceInstallState, v string) { s.TunnelName = v }, "TunnelName", serviceTunnelNameFlag, "MCP_TUNNEL_NAME", false),
		installField(func(s *ServiceInstallState) string { return s.AuthToken }, func(s *ServiceInstallState, v string) { s.AuthToken = v }, "AuthToken", serviceAuthTokenFlag, "MCP_AUTH_TOKEN", false),
		installField(func(s *ServiceInstallState) string { return s.TunnelToken }, func(s *ServiceInstallState, v string) { s.TunnelToken = v }, "TunnelToken", serviceTunnelTokenFlag, "MCP_TUNNEL_TOKEN", true),
		installField(func(s *ServiceInstallState) string { return s.NgrokAPIKey }, func(s *ServiceInstallState, v string) { s.NgrokAPIKey = v }, "NgrokAPIKey", serviceNgrokAPIKeyFlag, "", false),
		installField(func(s *ServiceInstallState) string { return s.PublicURL }, func(s *ServiceInstallState, v string) { s.PublicURL = v }, "PublicURL", servicePublicURLFlag, "MCP_PUBLIC_URL", true),
		installField(func(s *ServiceInstallState) string { return s.Host }, func(s *ServiceInstallState, v string) { s.Host = v }, "Host", serviceHostFlag, "MCP_HOST", false),
	}
}

// installFieldByNameKey resolves the typed *Field for a named install field via
// the exported AnyField.Declared() path. nil for an unknown name.
var installFieldByNameKey = func() map[string]*fieldform.Field[*ServiceInstallState, string] {
	m := make(map[string]*fieldform.Field[*ServiceInstallState, string], len(installFieldEntries()))
	for _, anyf := range installFieldEntries() {
		if f, ok := anyf.Declared().(*fieldform.Field[*ServiceInstallState, string]); ok {
			m[anyf.FieldName()] = f
		}
	}
	return m
}()

// installFieldByName returns the typed *Field for a named install field
// (used by configurers that attach a provider-specific Prompt/Validate). Returns
// nil for an unknown name.
func installFieldByName(name string) *fieldform.Field[*ServiceInstallState, string] {
	return installFieldByNameKey[name]
}

// installFieldValue reads a named field's Operational value directly from the
// state (delegating to the field's Get), for code that must check a value
// without running a full resolve.
func installFieldValue(name string, s *ServiceInstallState) string {
	if f := installFieldByName(name); f != nil && s != nil {
		return f.Operational(s)
	}
	return ""
}

// installFormFields is the full, heterogeneous form field set for the MCP
// input_required form: the nine string-valued install fields PLUS the tri-state
// OAuth (*bool) and Port (*int) decisions. The pointer-typed Bool/Int
// constructors keep the Decided channel in the name-keyed decisions map (via
// oauthBinding/portBinding) and the pointer as the Operational value only, so an
// operator decision is persisted verbatim (nil = not decided this run, &false /
// &0 = a legitimate explicit decision) while a fold never looks like a decision.
// These two are decision-only (no EnvFileKey), matching how serviceInstallStateToEnv
// persists them only when the operator supplied a value. FormSchema over this set
// emits the same keys an interactive CLI gather would prompt for.
func installFormFields() []fieldform.AnyField[*ServiceInstallState] {
	oauth := fieldform.Bool[*ServiceInstallState](oauthBinding, "OAuth",
		func(s *ServiceInstallState) *bool { return s.OAuth },
		func(s *ServiceInstallState, v bool) { s.OAuth = &v },
		fieldform.Meta{Flag: serviceOAuthFlag})
	port := fieldform.Int[*ServiceInstallState](portBinding, "Port",
		func(s *ServiceInstallState) *int { return s.Port },
		func(s *ServiceInstallState, v int) { s.Port = &v },
		fieldform.Meta{Flag: servicePortFlag})
	return append(installFieldEntries(), oauth, port)
}

// installFormSchema returns the JSON schema for the MCP input_required form over
// installFormFields — the same shared emitter the CLI gather path drives, from
// one field declaration set.
func installFormSchema() *jsonschema.Schema {
	return fieldform.FormSchema(installFormFields())
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
	for _, f := range installFieldByNameKey {
		if !f.ReDerives {
			continue
		}
		if f.Operational(s) != "" {
			f.SetOperational(s, "")
			cleared = true
		}
		s.unmarkDecided(f.Name)
	}
	return cleared
}

// serviceInstallValueSource adapts the host's CLI flags and persisted env file
// to the fieldform.ValueSource interface so fieldform.GatherAny can fold both
// into the two-channel state. A nil envFile (or empty env) yields no env fold.
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
// present flag is an operator decision for fieldform.GatherAny precedence 1.
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

// newServiceInstallValueSource builds the install flow's fieldform.ValueSource
// from the host command (its flags, incl. process-env Sources) and the persisted
// env file.
func newServiceInstallValueSource(cmd *cli.Command, envFile string) *serviceInstallValueSource {
	return &serviceInstallValueSource{
		envFile: envFile,
		flags:   serviceInstallFlags(cmd),
	}
}
