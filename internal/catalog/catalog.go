package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Actor identifies which kind of consumer is invoking an operation. The
// catalog uses it to enforce Interaction, Visibility, and Safety at dispatch
// time. A model agent is held to a stricter bar than a human in the app.
type Actor int

const (
	// ActorModel is an autonomous model agent (the default zero value).
	ActorModel Actor = iota
	// ActorApp is the hosting application itself (UI-driven).
	ActorApp
	// ActorHuman is a human operator at the app/CLI.
	ActorHuman
)

// ToolDescriptor is the SDK-neutral description of one operation that every
// frontend derives from. It is produced by Catalog.Describe (the discovery
// view) and by the MCP compiler as the concrete tool set. It carries only
// declared, non-executable metadata (Safety, Interaction, Visibility) plus the
// audience-aware description and JSON-Schema inputs. It carries no Handler:
// dispatch always goes through the owning Catalog.Invoke, the single enforcement
// point for Interaction, Visibility, Safety, and required-arg gates. The CLI
// compiler consumes the underlying Operation, with its Handler, to build
// runnable commands.
type ToolDescriptor struct {
	Name        string
	Title       string
	Description string // fallback MCP description (from MCPTargets fallback)
	// InputSchema is a minimal JSON Schema built from the operation's Args
	// (object with per-arg properties plus a "required" list).
	InputSchema json.RawMessage
	Safety      Safety
	Interaction Interaction
	Visibility  Visibility
	Category    string
	// MCPTargets carries per-profile description variants for the MCP surface.
	// The MCP bridge maps these to model.ToolTarget for per-request resolution.
	// The CLI compiler never reads this field.
	MCPTargets []Target
}

// Catalog is the forward-looking registry every frontend consumes. It provides
// discovery (Search/Get/Describe) and dispatch (Invoke) with interaction,
// visibility, and safety enforcement built in.
type Catalog interface {
	// Add registers an operation. It returns an error if a different
	// operation with the same name is already registered.
	Add(op Operation) error

	// Get returns the operation registered under name, if any (exact match).
	Get(name string) (Operation, bool)

	// Search returns operations matching query (case-insensitive substring on
	// Name/Summary/Description/Title) AND category AND visibility. An op is
	// included if its Visibility permits the given actor visibility v.
	Search(query, category string, v Visibility) []Operation

	// Describe returns the discovery descriptor for name (exact match), scoped
	// to the acting actor. The Description field is resolved from the
	// MCPTargets fallback (or op.Description() when no targets exist).
	// Describe applies the same visibility boundary as Search, so an app-only
	// operation is invisible to model actors and a model-only operation is
	// invisible to app/human actors. An op excluded from the caller's surface
	// returns not-found, matching Search and Invoke.
	Describe(name string, actor Actor) (ToolDescriptor, bool)

	// Invoke dispatches to the named operation's Handler after enforcing its
	// Interaction, Visibility, and Safety rules against the acting Actor.
	// A destructive operation invoked by a model returns an error that wraps
	// ErrConfirmRequired so the caller can gate on it via errors.Is.
	Invoke(ctx context.Context, name string, input map[string]any, actor Actor) (any, error)
}

// ErrConfirmRequired is a sentinel signal that a destructive operation was
// invoked by a model actor and needs explicit human confirmation. Callers
// detect it with errors.Is(err, ErrConfirmRequired) to surface a confirm flow.
var ErrConfirmRequired = errors.New("confirmation required")

// ErrHumanRequired is a sentinel signal that an InteractionHumanOnly or
// InteractionNeedsHandoff operation was invoked by a non-human actor and so
// was refused at the gate. Such flows demand out-of-band human action a model
// or app would never complete. Callers detect it with
// errors.Is(err, ErrHumanRequired) to route the refusal to a human hand-off
// rather than treating it as a failure.
var ErrHumanRequired = errors.New("operation requires a human")

// ErrSelector is a sentinel signal that a SelectionGroup resolved to the wrong
// number of selected members. A SelectionGroup requires exactly one member to
// be selected; zero or more-than-one is an ambiguous or incomplete selector
// (e.g. pins_rm given both cids and all=true, or neither). Callers detect it
// with errors.Is(err, ErrSelector) to surface a selector contract violation.
var ErrSelector = errors.New("selector group must select exactly one member")

// SensitiveSchemaKey is the JSON Schema property marking an arg whose value
// must be redacted (logs, echoed tool calls) on the model surface. It matches
// OperationArg.Sensitive, which the CLI surfaces in help text. Marking it in
// the shared schema rather than as a magic string literal keeps the marker
// strongly typed and consistent across the MCP compiler and Describe.
const SensitiveSchemaKey = "sensitive"

// NewCatalog returns a concurrency-safe in-memory Catalog registry.
func NewCatalog() Catalog {
	return &catalogImpl{ops: map[string]Operation{}}
}

// catalogImpl is the concrete registry backing the Catalog interface.
type catalogImpl struct {
	mu  sync.RWMutex
	ops map[string]Operation
}

func (c *catalogImpl) Add(op Operation) error {
	if op == nil {
		return errors.New("catalog: cannot add a nil operation")
	}
	if op.Name() == "" {
		return errors.New("catalog: operation must have a name")
	}
	if err := validateOperation(op); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.ops[op.Name()]; dup {
		return fmt.Errorf("catalog: operation %q already registered", op.Name())
	}
	c.ops[op.Name()] = op
	return nil
}

// validateOperation rejects an operation whose declared metadata is unusable at
// dispatch. It catches a malformed Default for a typed argument (e.g. "abc" for
// an int): defaultValue would otherwise silently coerce it to the zero value and
// hand the Handler a wrong value. A spec with an invalid default is a config
// bug, so reject it at registration rather than deliver a wrong value later.
func validateOperation(op Operation) error {
	for _, a := range op.Args() {
		// An AgentOnly arg is never emitted as a CLI --flag (the CLI caller
		// can never supply it), so marking it Required without a Default would
		// make the CLI command permanently fail "missing required argument".
		// Fail fast at registration: direct authors to AgentRequired for
		// MCP-only requiredness, or to a Default.
		if a.AgentOnly && a.Required && a.Default == "" {
			return fmt.Errorf("catalog: operation %q arg %q: AgentOnly arg cannot be Required without a Default (the CLI can never supply it); use AgentRequired for MCP-only requiredness or declare a Default", op.Name(), a.Name)
		}
		// Enum-constrained args must be string-typed and (when a default is
		// also declared) must have their default within the enum. Otherwise
		// resolveArg's enum check would never apply, or would reject a default
		// the arg itself declares. Both are config bugs caught at registration.
		if len(a.Enum) > 0 && a.Type != ArgTypeString {
			return fmt.Errorf("catalog: operation %q arg %q: enum declared on non-string arg type", op.Name(), a.Name)
		}
		if a.Default != "" && len(a.Enum) > 0 && !slices.Contains(a.Enum, a.Default) {
			return fmt.Errorf("catalog: operation %q arg %q: default %q not in enum %v", op.Name(), a.Name, a.Default, a.Enum)
		}
		if a.Default == "" {
			continue
		}
		if err := validateDefault(a); err != nil {
			return fmt.Errorf("catalog: operation %q arg %q: %w", op.Name(), a.Name, err)
		}
	}
	return nil
}

// intPtr returns a pointer to v. It is the boxed form a nullable-int
// (ArgTypeNullableInt) arg carries so a Handler can distinguish an omitted
// arg (nil) from an explicit value including 0.
func intPtr(v int) *int { return &v }

// validateDefault checks that a non-empty string Default parses for its ArgType.
func validateDefault(a OperationArg) error {
	switch a.Type {
	case ArgTypeBool, ArgTypeNullableBool:
		if a.Default != "true" && a.Default != "false" {
			return fmt.Errorf("invalid bool default %q", a.Default)
		}
	case ArgTypeInt, ArgTypeNullableInt:
		if _, err := strconv.Atoi(a.Default); err != nil {
			return fmt.Errorf("invalid int default %q: %w", a.Default, err)
		}
	case ArgTypeFloat:
		if _, err := strconv.ParseFloat(a.Default, 64); err != nil {
			return fmt.Errorf("invalid float default %q: %w", a.Default, err)
		}
	case ArgTypeDuration:
		if _, err := time.ParseDuration(a.Default); err != nil {
			return fmt.Errorf("invalid duration default %q: %w", a.Default, err)
		}
	}
	// String and StringSlice accept any default (StringSlice is comma-split;
	// a single segment is valid).
	return nil
}

func (c *catalogImpl) Get(name string) (Operation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	op, ok := c.ops[name]
	return op, ok
}

func (c *catalogImpl) Search(query, category string, v Visibility) []Operation {
	q := strings.ToLower(strings.TrimSpace(query))
	cat := strings.ToLower(strings.TrimSpace(category))

	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Operation
	for _, op := range c.ops {
		if !visibleTo(op.Visibility(), v) {
			continue
		}
		if cat != "" && !strings.EqualFold(op.Category(), cat) {
			continue
		}
		if q != "" && !matchesQuery(op, q) {
			continue
		}
		out = append(out, op)
	}
	// Map iteration order is nondeterministic; sort so CLI help, MCP tool
	// lists, and discovery output are stable across runs.
	slices.SortFunc(out, func(a, b Operation) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

// visibleTo reports whether an op with visibility ov is discoverable by a
// consumer searching with visibility v. A model search (VisibilityModel)
// excludes app-only ops; the app (VisibilityApp) sees both app-only and
// both-visible ops; a search with VisibilityBoth sees everything.
func visibleTo(ov, v Visibility) bool {
	switch v {
	case VisibilityModel:
		return ov == VisibilityModel || ov == VisibilityBoth
	case VisibilityAppOnly:
		return ov == VisibilityAppOnly || ov == VisibilityBoth
	default: // VisibilityBoth or unknown: unrestricted
		return true
	}
}

// matchesQuery reports whether the operation matches the lower-cased query as
// a substring of Name, Summary, Description, or Title.
func matchesQuery(op Operation, q string) bool {
	return strings.Contains(strings.ToLower(op.Name()), q) ||
		strings.Contains(strings.ToLower(op.Summary()), q) ||
		strings.Contains(strings.ToLower(op.Description()), q) ||
		strings.Contains(strings.ToLower(op.Title()), q)
}

func (c *catalogImpl) Describe(name string, actor Actor) (ToolDescriptor, bool) {
	op, ok := c.Get(name)
	if !ok {
		return ToolDescriptor{}, false
	}
	// The visibility boundary matches the Search surface for the acting actor's
	// audience, so Describe and the compilers (which Search by surface) agree on
	// what each actor is allowed to see:
	//   - a model actor searches the VisibilityModel surface, so sees Model + Both
	//   - an app/human actor searches the VisibilityAppOnly surface, so sees
	//     AppOnly + Both, not model-only ops
	// An op excluded from the actor's surface returns not-found, as if it had
	// never been registered.
	if !visibleTo(op.Visibility(), actorVisibility(actor)) {
		return ToolDescriptor{}, false
	}
	return descriptorFor(op, actor, nil), true
}

// actorVisibility maps an actor to the Visibility of the search surface that
// actor's frontend uses, so Describe can apply the exact same boundary as
// Search / the compilers. A model agent gets the agent-facing surface; the app
// and human operator get the app-facing surface.
func actorVisibility(actor Actor) Visibility {
	switch actor {
	case ActorModel:
		return VisibilityModel
	default: // ActorApp, ActorHuman
		return VisibilityAppOnly
	}
}

// descriptorFor builds a ToolDescriptor from an Operation, selecting the
// description by audience so a non-model surface (app/human) never exposes
// agent-style MCPTargets text.
//   - actor == ActorModel: resolve the MCP description from MCPTargets — the
//     fallback target (empty Require, Visible=true) provides the static
//     description carried on the descriptor.
//   - actor != ActorModel: use the plain op.Description() (the human/CLI
//     description), so the CLI and MCP app surface never expose agent text.
//
// Per-request resolution in the MCP layer (DescribeFor) may override the model
// description with a more specific target's description. The CLI compiler does
// not use descriptorFor — it reads op.Description() directly.
//
// profile is the opaque startup/transport profile used to resolve a
// DescFunc-only fallback target (see FallbackFunc). It is optional: nil skips
// DescFunc resolution and falls back to the CLI description.
func descriptorFor(op Operation, actor Actor, profile any) ToolDescriptor {
	desc := op.Description()
	if actor == ActorModel {
		desc = fallbackDescription(op.Description(), op.MCPTargets(), profile)
	}
	return ToolDescriptor{
		Name:        op.Name(),
		Title:       op.Title(),
		Description: desc,
		InputSchema: inputSchemaFromArgs(op.Name(), op.Args()),
		Safety:      op.Safety(),
		Interaction: op.Interaction(),
		Visibility:  op.Visibility(),
		Category:    op.Category(),
		MCPTargets:  op.MCPTargets(),
	}
}

// fallbackDescription returns the fallback target's Description from targets
// (the one with empty Require and Visible=true). If no fallback target
// exists, it returns cliDesc as a safety net so the MCP descriptor always
// has a non-empty description.
//
// A fallback target may be a DescFunc-only variant (empty Description, the
// resolver in DescFunc) built via FallbackFunc. DescFunc is resolved against
// profile — the startup/transport profile when compiling the static surface,
// nil elsewhere — so agent-critical guidance composed from discrete DSL
// segments is not dropped from the static/non-profile descriptor. When profile
// is nil (an unknown profile), the DescFunc fallback is skipped and cliDesc is
// returned, matching the pre-FallbackFunc behavior.
func fallbackDescription(cliDesc string, targets []Target, profile any) string {
	for _, t := range targets {
		if len(t.Require) == 0 && t.Visible {
			if t.Description != "" {
				return t.Description
			}
			if t.DescFunc != nil && profile != nil {
				if s := t.DescFunc(profile); s != "" {
					return s
				}
			}
		}
	}
	return cliDesc
}

func (c *catalogImpl) Invoke(ctx context.Context, name string, input map[string]any, actor Actor) (any, error) {
	op, ok := c.Get(name)
	if !ok {
		return nil, fmt.Errorf("catalog: unknown operation %q", name)
	}

	// Interaction: an operation that requires a human, either HumanOnly
	// (interactive prompts) or NeedsHandoff (external browser/device, two-call
	// split), cannot be driven by anything but a human. A model agent in
	// particular must not run it, since the flow demands out-of-band human
	// action the model would never complete.
	if (op.Interaction() == InteractionHumanOnly || op.Interaction() == InteractionNeedsHandoff) && actor != ActorHuman {
		return nil, fmt.Errorf("operation %q requires a human: refused for actor %s: %w", name, actor, ErrHumanRequired)
	}

	// Visibility: an op excluded from an actor's surface must not be executable
	// via Invoke for that actor. This uses the same visibleTo predicate as
	// Search and Describe, so an op a model cannot discover on the model surface
	// (AppOnly) and an op an app/human cannot discover on the app surface
	// (Model) are both refused here.
	if !visibleTo(op.Visibility(), actorVisibility(actor)) {
		return nil, fmt.Errorf("operation %q is not visible to actor %s", name, actor)
	}

	// Safety: a destructive operation invoked by a model needs explicit human
	// confirmation. If the operation declares an agent-settable `confirm` arg
	// (a bool "confirm" that is NOT AgentRequired) and the model has supplied
	// that confirmation (confirm=true), the confirmation is already present and
	// the op runs headless (e.g. vault_version_restore's confirm=true is the
	// rollback contract). Ops with no confirm arg, or with an AgentRequired
	// confirm that by contract only a human sets (ipns_keys_delete,
	// admin_platform_domains_delete), still return the ErrConfirmRequired
	// sentinel so the caller surfaces a confirm hand-off.
	if op.Safety() == SafetyDestructive && actor == ActorModel && !destructiveConfirmSatisfied(op, input) {
		return nil, fmt.Errorf("operation %q is destructive: %w", name, ErrConfirmRequired)
	}

	h := op.Handler()
	if h == nil {
		return nil, fmt.Errorf("operation %q has no handler", name)
	}
	// Required-arg validation is shared with the CLI surface (firstMissingRequiredArg):
	// an arg is mandatory only when Required AND has no Default (the default
	// satisfies it via NormalizeOperationInput below), and a present-but-empty or
	// nil string/slice counts as missing too. Both frontends therefore reject the
	// same inputs before a Handler runs.
	normalized, err := NormalizeOperationInput(op, input)
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", name, err)
	}
	return h.Execute(ctx, normalized)
}

// destructiveConfirmSatisfied reports whether a destructive operation invoked
// by a model already carries explicit confirmation in the supplied input. An
// op satisfies it when it declares a bool `confirm` argument that is
// agent-settable (NOT AgentRequired — AgentRequired marks "only a human sets
// this", which a model can never self-satisfy) and the input's confirm is
// truthy. Ops with no confirm arg, or with an AgentRequired confirm, return
// false so the caller surfaces the confirm hand-off.
func destructiveConfirmSatisfied(op Operation, input map[string]any) bool {
	for _, a := range op.Args() {
		if a.Name != "confirm" || a.AgentRequired {
			continue
		}
		if a.Type == ArgTypeBool {
			return BoolArg(input, "confirm", false)
		}
	}
	return false
}

// NormalizeOperationInput coerce-renders an operation's raw input into its
// declared arg shapes and fills declared defaults, using the same single
// resolveArg state machine as the CLI and MCP dispatch surfaces. It is the
// standalone entry point callers that invoke an Operation.Handler directly
// (e.g. the MCP vault setup handlers, which route around Catalog.Invoke) must
// use so aliasing, coercion, and defaults apply identically to the Invoke path.
func NormalizeOperationInput(op Operation, input map[string]any) (map[string]any, error) {
	if m := firstMissingRequiredArg(op.Args(), input); m != nil {
		return nil, fmt.Errorf("missing required argument %q", m.Name)
	}
	if key := unknownOperationArg(op.Args(), input); key != "" {
		valid := make([]string, 0, len(op.Args())*2)
		for _, a := range op.Args() {
			valid = append(valid, a.Name)
			if alias := camelCase(a.Name); alias != a.Name {
				valid = append(valid, alias)
			}
		}
		return nil, fmt.Errorf("unrecognized argument %q (valid: %s)", key, strings.Join(valid, ", "))
	}
	normalized, err := normalizeInputDefaults(op.Args(), input)
	if err != nil {
		return nil, err
	}
	// Strip reserved plumbing keys that no catalog handler reads, so they are
	// recognized for the strict unknown-argument check above yet never leak
	// into a handler's input map. ReservedAuthTokenKey is deliberately NOT
	// stripped: the catalogops service selectors (e.g. websites.go via
	// authTokenFromInput) read it from the normalized input passed to the
	// Handler to honor the per-invocation --auth-token override, which would
	// otherwise fall back to config.
	//
	// The strip is guarded by isDeclaredArgKey so a reserved key name that an
	// operation legitimately declares as an OperationArg (e.g. an op with an
	// arg literally named "request_state") always wins over the transport
	// reservation: its declared value must reach the Handler rather than be
	// mistaken for plumbing and deleted.
	if !isDeclaredArgKey(op.Args(), ReservedRequestStateKey) {
		delete(normalized, ReservedRequestStateKey)
	}
	return normalized, nil
}

// isDeclaredArgKey reports whether key matches an operation arg's declared
// name or its camelCase alias. It is used to disambiguate a reserved plumbing
// key (auth_token / request_state) from an operation that declares an arg with
// the same literal name, so the declared arg always takes precedence and is
// never stripped as transport plumbing.
func isDeclaredArgKey(args []OperationArg, key string) bool {
	for _, a := range args {
		if a.Name == key {
			return true
		}
		if alias := camelCase(a.Name); alias == key {
			return true
		}
	}
	return false
}

// unknownOperationArg returns the first input key that matches no declared
// operation arg (in either its kebab-case name or camelCase alias) and no
// reserved input key, or "" when every input key is recognized. This turns a
// typo'd/unknown parameter (e.g. `limit` when the declared arg is
// `page-size`) into a loud error instead of a silent no-op where the handler
// reads from a default.
func unknownOperationArg(args []OperationArg, input map[string]any) string {
	known := make(map[string]struct{}, len(args)*2)
	for _, a := range args {
		known[a.Name] = struct{}{}
		if alias := camelCase(a.Name); alias != a.Name {
			known[alias] = struct{}{}
		}
	}
	for r := range reservedInputKeys {
		known[r] = struct{}{}
	}
	for k := range input {
		if _, ok := known[k]; !ok {
			return k
		}
	}
	return ""
}

// ReservedInputKeys are input-map keys that plumbing layers (the CLI
// --auth-token override, the MCP transport's cross-round request_state
// recovery token) inject into an operation's input but that are not declared
// OperationArgs. NormalizeOperationInput treats them as recognized so strict
// unknown-argument rejection does not reject legitimate transport plumbing.
//
// Boundary note: the MCP SDK's officialToolHandler also merges per-elicitation
// ids into args (args[id]=content) on a retried input_required round-trip. Those
// are intentionally NOT reserved here: only non-catalog wizard handles emit
// elicitations (they decode via decodeToolArgs, which is lenient), and no
// catalog-reachable operation emits a form elicitation, so a legitimate
// elicitation id can never reach this strict check. A client-forged id on a
// catalog call is an unrecognized argument and SHOULD be rejected. Keep this
// reserved set in sync with the injection sites in internal/mcp/sdk_official.go.
var reservedInputKeys = map[string]struct{}{
	ReservedAuthTokenKey:    {},
	ReservedRequestStateKey: {},
}

// ReservedAuthTokenKey is the shared name of the reserved --auth-token input
// key. It lives here (the transport-agnostic layer) so operation packages can
// reference it without an import cycle.
const ReservedAuthTokenKey = "auth_token"

// ReservedRequestStateKey is the reserved input key the MCP transport injects
// on a retried tool call carrying the client-echoed request_state token, used
// to recover cross-round session context after an input_required result.
const ReservedRequestStateKey = "request_state"

// argState is the classification of what a supplied argument value means.
// Every consumer, required-arg enforcement, default filling, and shape
// coercion, derives its decision from a single resolveArg result.
type argState int

const (
	// stateAbsent: the key is not present in the input at all.
	stateAbsent argState = iota
	// stateNull: present but JSON null (nil). Treated as absent for required
	// args, but coerced to the declared zero-shape for the Handler.
	stateNull
	// stateEmpty: present and explicitly empty ("" / []string{} / []any{}).
	stateEmpty
	// stateFilled: present, non-empty, and coerced to the declared Go shape.
	stateFilled
	// stateInvalid: present but the wrong shape / an uncoercible type or element.
	stateInvalid
)

// resolveArg classifies a single raw argument value into its argState and, for
// every non-invalid state, the final Go value the Handler should receive. The
// states Absent/Null/Empty/Filled/Invalid form a complete partition, and the
// coercion that produces the handler value happens together with the
// classification.
//
// The input is `any` (Handler.Execute takes map[string]any; MCP decodes to
// map[string]any; CLI funnels through any), so a generic resolveArg[T] cannot
// bind T statically at the call sites. An explicit type switch is therefore
// required here.
func resolveArg(a OperationArg, raw any, present bool) (value any, st argState, err error) {
	if !present {
		return nil, stateAbsent, nil
	}
	if raw == nil {
		// JSON null: absent for required-arg purposes, but coerce to the
		// declared zero-shape so the Handler never sees a naked nil.
		return zeroShape(a.Type), stateNull, nil
	}
	switch a.Type {
	case ArgTypeStringSlice:
		s, err := coerceStringSlice(raw)
		if err != nil {
			return nil, stateInvalid, err
		}
		if len(s) == 0 {
			return s, stateEmpty, nil
		}
		return s, stateFilled, nil
	case ArgTypeString:
		s, ok := raw.(string)
		if !ok {
			return nil, stateInvalid, fmt.Errorf("expected string, got %T", raw)
		}
		if s == "" {
			return s, stateEmpty, nil
		}
		// Enum-constrained args reject out-of-range values with an error: a
		// model passing "ZZZ" for a DNS-record type arg gets an error, not a
		// silent invalid value. The match is case-insensitive so a value whose
		// semantic is case-insensitive by definition (e.g. a DNS record type:
		// "txt" == "TXT") passes the gate and the handler normalizes the casing
		// before it reaches the wire. The returned value keeps the caller's
		// original casing; normalization is the handler's job.
		if len(a.Enum) > 0 {
			for _, e := range a.Enum {
				if strings.EqualFold(s, e) {
					return s, stateFilled, nil
				}
			}
			return nil, stateInvalid, fmt.Errorf("value %q not in enum %v", s, a.Enum)
		}
		return s, stateFilled, nil
	case ArgTypeRawJSON:
		// Pass-through: the value arrives as-is (a JSON string from the CLI, a
		// decoded array/object/scalar from MCP JSON). The Handler is
		// responsible for parsing it into its domain shape (e.g. a predicate
		// list). No coercion, no enum gate — the schema language is entirely
		// the Handler's.
		if s, ok := raw.(string); ok {
			if s == "" {
				return s, stateEmpty, nil
			}
			return s, stateFilled, nil
		}
		return raw, stateFilled, nil
	case ArgTypeFlexibleID:
		// A string-or-integer id (e.g. the numeric id ipns_keys_list emits).
		// Accept a string as-is; accept any JSON/native integer by rendering
		// its decimal form so a StrArg/StrFlexibleArg reader always sees a
		// string. This bridges the drift where the backend key id is an int
		// while the arg the handler reads is a string.
		switch v := raw.(type) {
		case string:
			if v == "" {
				return v, stateEmpty, nil
			}
			return v, stateFilled, nil
		case int:
			return strconv.FormatInt(int64(v), 10), stateFilled, nil
		case int8:
			return strconv.FormatInt(int64(v), 10), stateFilled, nil
		case int32:
			return strconv.FormatInt(int64(v), 10), stateFilled, nil
		case int64:
			return strconv.FormatInt(v, 10), stateFilled, nil
		case uint:
			return strconv.FormatUint(uint64(v), 10), stateFilled, nil
		case uint64:
			return strconv.FormatUint(v, 10), stateFilled, nil
		case float64:
			// JSON has no integer type, so an int arrives as float64. Reject
			// fractional values (a bare decimal is not a valid id). Enforce
			// the int64 range explicitly with float comparisons (math.MinInt64
			// / MaxInt64 promote to their representable float64 bounds) rather
			// than by converting and round-tripping: int64(v) for an
			// out-of-range v is implementation-defined in Go (it wraps on some
			// platforms and clamps on others), so a round-trip test is not a
			// reliable overflow guard.
			if v != math.Trunc(v) {
				return nil, stateInvalid, fmt.Errorf("expected string or integer, got %v", v)
			}
			if v < math.MinInt64 || v >= math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer id %v out of range", v)
			}
			return strconv.FormatInt(int64(v), 10), stateFilled, nil
		case json.Number:
			if i, err := v.Int64(); err == nil {
				return strconv.FormatInt(i, 10), stateFilled, nil
			}
			return nil, stateInvalid, fmt.Errorf("expected string or integer, got %v", raw)
		default:
			return nil, stateInvalid, fmt.Errorf("expected string or integer, got %T", raw)
		}
	case ArgTypeBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, stateInvalid, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, stateFilled, nil
	case ArgTypeNullableBool:
		switch v := raw.(type) {
		case bool:
			return &v, stateFilled, nil
		case *bool:
			return v, stateFilled, nil
		default:
			return nil, stateInvalid, fmt.Errorf("expected bool, got %T", raw)
		}
	case ArgTypeInt:
		// The CLI gives int; an MCP caller's int arrives as float64; a Go
		// caller through Invoke may pass any native integer type or
		// json.Number. Normalize every accepted shape to int64, then apply the
		// platform-int round-trip guard below so a value that fits int64 but
		// not this build's native int (e.g. 5e9 on 32-bit) is rejected instead
		// of silently wrapped.
		var i64 int64
		switch n := raw.(type) {
		case int:
			i64 = int64(n)
		case int8, int16, int32, int64:
			switch v := n.(type) {
			case int8:
				i64 = int64(v)
			case int16:
				i64 = int64(v)
			case int32:
				i64 = int64(v)
			case int64:
				i64 = v
			}
		case uint:
			u := uint64(n)
			if u > math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer %v out of range", n)
			}
			i64 = int64(u)
		case uint64:
			if n > math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer %v out of range", n)
			}
			i64 = int64(n)
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, stateInvalid, fmt.Errorf("expected integer, got %v", n)
			}
			i64 = i
		case float64:
			// JSON has no integer type, so an MCP caller's int arrives as
			// float64. Reject fractional and out-of-int64-range values instead
			// of truncating (30.7 -> 30) or silently overflowing.
			//
			// Compare against math.MinInt64/MaxInt64 exactly: float64(math.MaxInt)
			// would round 2^63-1 up to 2^63, letting a value of exactly 2^63
			// through where int(n) overflows silently. n >= math.MaxInt64 (which
			// promotes to the float64 2^63) correctly excludes that boundary.
			if n != math.Trunc(n) {
				return nil, stateInvalid, fmt.Errorf("expected integer, got %v", n)
			}
			if n < math.MinInt64 || n >= math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer %v out of range", n)
			}
			i64 = int64(n)
		default:
			return nil, stateInvalid, fmt.Errorf("expected int, got %T", raw)
		}
		// Platform guard: on a 32-bit build `int` is 32 bits, so int(i64) alone
		// would silently wrap a value like 5e9 (valid int64) into a wrong native
		// int. Round-trip through the platform int to reject anything beyond it.
		if int64(int(i64)) != i64 {
			return nil, stateInvalid, fmt.Errorf("integer %d out of range for this platform", i64)
		}
		return int(i64), stateFilled, nil
	case ArgTypeNullableInt:
		// Like ArgTypeInt but the Handler receives a *int, so it can
		// distinguish an omitted arg (nil) from an explicit 0. The CLI
		// surfaces absent as nil and provided as *int; an MCP/Go caller's
		// int arrives as float64/int64/etc. and is coerced to *int.
		switch n := raw.(type) {
		case *int:
			return n, stateFilled, nil
		case *int64:
			return intPtr(int(*n)), stateFilled, nil
		case int:
			return intPtr(n), stateFilled, nil
		case int64:
			return intPtr(int(n)), stateFilled, nil
		case int32:
			return intPtr(int(n)), stateFilled, nil
		case uint64:
			if n > math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer %v out of range", n)
			}
			i := int64(n)
			if int64(int(i)) != i {
				return nil, stateInvalid, fmt.Errorf("integer %d out of range for this platform", i)
			}
			return intPtr(int(i)), stateFilled, nil
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, stateInvalid, fmt.Errorf("expected integer, got %v", n)
			}
			if int64(int(i)) != i {
				return nil, stateInvalid, fmt.Errorf("integer %d out of range for this platform", i)
			}
			return intPtr(int(i)), stateFilled, nil
		case float64:
			if n != math.Trunc(n) {
				return nil, stateInvalid, fmt.Errorf("expected integer, got %v", n)
			}
			if n < math.MinInt64 || n >= math.MaxInt64 {
				return nil, stateInvalid, fmt.Errorf("integer %v out of range", n)
			}
			i := int64(n)
			if int64(int(i)) != i {
				return nil, stateInvalid, fmt.Errorf("integer %d out of range for this platform", i)
			}
			return intPtr(int(i)), stateFilled, nil
		default:
			return nil, stateInvalid, fmt.Errorf("expected int, got %T", raw)
		}
	case ArgTypeFloat:
		// CLI gives float64; MCP JSON decode gives float64; a Go caller may
		// pass any numeric type. Widen every accepted shape to float64.
		switch n := raw.(type) {
		case float64:
			return n, stateFilled, nil
		case float32:
			return float64(n), stateFilled, nil
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// Widen any native integer to float64. All values fit float64's
			// exponent range; near int64 extremes float64 cannot represent every
			// integer exactly, which is inherent to a float arg.
			switch v := n.(type) {
			case int:
				return float64(v), stateFilled, nil
			case int8:
				return float64(v), stateFilled, nil
			case int16:
				return float64(v), stateFilled, nil
			case int32:
				return float64(v), stateFilled, nil
			case int64:
				return float64(v), stateFilled, nil
			case uint:
				return float64(v), stateFilled, nil
			case uint8:
				return float64(v), stateFilled, nil
			case uint16:
				return float64(v), stateFilled, nil
			case uint32:
				return float64(v), stateFilled, nil
			case uint64:
				return float64(v), stateFilled, nil
			default:
				return nil, stateInvalid, fmt.Errorf("expected float, got %T", raw)
			}
		case json.Number:
			f, err := n.Float64()
			if err != nil {
				return nil, stateInvalid, fmt.Errorf("expected float, got %v", n)
			}
			return f, stateFilled, nil
		default:
			return nil, stateInvalid, fmt.Errorf("expected float, got %T", raw)
		}
	case ArgTypeDuration:
		// CLI gives time.Duration; MCP JSON decode gives a string.
		switch d := raw.(type) {
		case time.Duration:
			return d, stateFilled, nil
		case string:
			if d == "" {
				return time.Duration(0), stateEmpty, nil
			}
			parsed, perr := time.ParseDuration(d)
			if perr != nil {
				return nil, stateInvalid, fmt.Errorf("invalid duration %q: %w", d, perr)
			}
			return parsed, stateFilled, nil
		default:
			return nil, stateInvalid, fmt.Errorf("expected duration, got %T", raw)
		}
	default:
		return nil, stateInvalid, fmt.Errorf("unknown arg type %d", a.Type)
	}
}

// coerceStringSlice turns a StringSlice raw value into []string. []string
// passes through; []any (the MCP JSON shape) is converted element-wise, erroring
// on a non-string element rather than dropping it. nil is handled by resolveArg
// before calling here (it maps to the empty shape). Any non-slice value where a
// slice is declared is a caller error.
func coerceStringSlice(raw any) ([]string, error) {
	switch t := raw.(type) {
	case []string:
		return t, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("slice element %v is not a string", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a slice for StringSlice arg, got %T", raw)
	}
}

// zeroShape returns the declared Go zero value for an ArgType. Used to coerce a
// JSON null into the shape a Handler can safely type-assert (e.g. []string for a
// StringSlice), matching how []any{} is coerced.
func zeroShape(t ArgType) any {
	switch t {
	case ArgTypeStringSlice:
		return []string{}
	case ArgTypeString, ArgTypeFlexibleID:
		return ""
	case ArgTypeRawJSON:
		return nil
	case ArgTypeInt:
		return 0
	case ArgTypeFloat:
		return float64(0)
	case ArgTypeBool:
		return false
	case ArgTypeNullableBool:
		// A nil *bool is the "omitted" state: the Handler can distinguish it
		// from an explicit false, which is the whole point of a nullable bool.
		return (*bool)(nil)
	case ArgTypeNullableInt:
		// A nil *int is the "omitted" state; an explicit value (including 0)
		// is a non-nil *int. Same tri-state rationale as a nullable bool.
		return (*int)(nil)
	case ArgTypeDuration:
		return time.Duration(0)
	default:
		return nil
	}
}

// isRequiredArg reports whether an argument is mandatory from the caller's
// perspective. An arg is required only when it is declared Required AND has no
// Default: a declared default satisfies the arg via normalizeInputDefaults
// before the Handler runs, so from the caller's angle it is optional. Invoke,
// the CLI compiler (flagFor/actionFor), and the MCP/JSON-Schema builder all
// derive requiredness from this one predicate.
func isRequiredArg(a OperationArg) bool {
	return a.Required && a.Default == ""
}

// requiredArgNames lists the mandatory args in declaration order. It backs both
// the JSON-Schema "required" array (inputSchemaFromArgs) and any frontend that
// needs the set of required flags, keeping the schema and dispatch consistent on
// which args are optional.
func requiredArgNames(args []OperationArg) []string {
	var out []string
	for _, a := range args {
		if isRequiredArg(a) {
			out = append(out, a.Name)
		}
	}
	return out
}

// mcpRequiredNames lists the args the MCP surface must require: the shared
// required set plus any AgentRequired arg. AgentRequired is MCP-only; it is
// never part of requiredArgNames, so the CLI compiler never turns it into a
// required urfave flag. But the MCP JSON-Schema and dispatch must still reject
// an agent that omits it.
func mcpRequiredNames(args []OperationArg) []string {
	out := requiredArgNames(args)
	for _, a := range args {
		if a.AgentRequired {
			out = append(out, a.Name)
		}
	}
	return out
}

// firstMissingRequiredArg returns the first required arg whose resolved state
// is Absent, Null, or Empty (Required with no default, and not a filled value).
// It is the enforcement point shared by Invoke and the CLI actionFor so both
// frontends reject the same inputs before a Handler runs. Returns nil when every
// required arg is satisfied. Classification comes from resolveArg, the same
// function normalizeInputDefaults uses.
func firstMissingRequiredArg(args []OperationArg, input map[string]any) *OperationArg {
	return firstMissingArg(args, input, func(a OperationArg) bool { return isRequiredArg(a) })
}

// firstMissingMCPRequiredArg is the MCP-surface variant of
// firstMissingRequiredArg: it also enforces AgentRequired args, which the CLI
// never requires. Returns nil when every MCP-required arg is satisfied.
func firstMissingMCPRequiredArg(args []OperationArg, input map[string]any) *OperationArg {
	return firstMissingArg(args, input, func(a OperationArg) bool { return isRequiredArg(a) || a.AgentRequired })
}

// ValidateMCPRequired is the AgentRequired enforcement point for the MCP
// dispatch layer (DispatchCatalogOp). AgentRequired marks an arg required on the
// MCP surface only; it is deliberately absent from the shared
// NormalizeOperationInput, which the CLI path uses, so AgentRequired can never
// leak into a non-MCP invocation. Returns an error naming the first missing
// MCP-required arg, or nil when all are satisfied.
func ValidateMCPRequired(op Operation, input map[string]any) error {
	if m := firstMissingMCPRequiredArg(op.Args(), input); m != nil {
		return fmt.Errorf("missing required argument %q", m.Name)
	}
	return nil
}

func firstMissingArg(args []OperationArg, input map[string]any, required func(OperationArg) bool) *OperationArg {
	for i := range args {
		a := args[i]
		if !required(a) {
			continue
		}
		raw, present := lookupArgInput(a, input)
		_, st, _ := resolveArg(a, raw, present)
		if st == stateAbsent || st == stateNull || st == stateEmpty {
			return &args[i]
		}
	}
	return nil
}

// lookupArgInput resolves an operation arg against a raw input map, matching
// both the declared (kebab) name and its camelCase alias, so a model sending
// either spelling satisfies the arg. Returns the value and whether it was
// present under either key.
func lookupArgInput(a OperationArg, input map[string]any) (any, bool) {
	if raw, ok := input[a.Name]; ok {
		return raw, true
	}
	if alias := camelCase(a.Name); alias != a.Name {
		if raw, ok := input[alias]; ok {
			return raw, true
		}
	}
	return nil, false
}

// camelCase converts a kebab-case name to camelCase (e.g. "device-name" ->
// "deviceName"). Used to accept the camelCase spelling of an arg a model may
// send in place of the declared kebab name.
func camelCase(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	upper := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' {
			upper = true
			continue
		}
		if upper {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			upper = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

// normalizeInputDefaults mutates a copy of input, using resolveArg to (1) coerce
// every present value into its declared ArgType Go shape and (2) fill declared
// defaults for Absent/Null/Empty values. It guarantees the Handler receives an
// identical, correctly-typed input regardless of which frontend dispatched. A
// CLI-provided []string, an MCP-provided []any, a JSON null, and an explicit
// []any{} all converge on []string, so a Handler type-asserting []string never
// panics, and a supplied non-empty value is never clobbered by a default.
func normalizeInputDefaults(args []OperationArg, input map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(args))
	for k, v := range input {
		out[k] = v
	}
	for _, a := range args {
		// Resolve the arg under either the declared (kebab) name or its
		// camelCase alias (e.g. "deviceName" for "device-name"): agents
		// commonly use camelCase, and aliasing both to the declared name keeps
		// every Handler reading a single, consistent key without per-op
		// dual-read hacks. The required-arg check shares lookupArgInput, so the
		// schema, dispatch, and Handler agree on both spellings.
		raw, present := lookupArgInput(a, out)
		if alias := camelCase(a.Name); alias != a.Name {
			// Drop the alias from the handler input regardless of which key
			// supplied the value, so a model's camelCase spelling is
			// canonicalized to the declared name and never leaks a duplicate.
			delete(out, alias)
		}
		value, st, err := resolveArg(a, raw, present)
		if err != nil {
			return nil, fmt.Errorf("operation arg %q: %w", a.Name, err)
		}
		switch st {
		case stateFilled:
			// Present, non-empty, coerced; keep it.
			out[a.Name] = value
		case stateInvalid:
			// Unreachable (resolveArg returned err above), kept for clarity.
			return nil, fmt.Errorf("operation arg %q: invalid value", a.Name)
		default: // Absent / Null / Empty
			// A declared default satisfies the arg; otherwise coerce to the
			// declared zero-shape (e.g. []string{} for a StringSlice, "" for a
			// string) so the Handler always sees the declared type, including
			// for an omitted optional arg, whose resolveArg value is nil.
			// A naked nil would panic a Handler type-asserting `.(string)`.
			if a.Default != "" {
				out[a.Name] = defaultValue(a)
			} else {
				out[a.Name] = zeroShape(a.Type)
			}
		}
	}
	if err := enforceSelectionGroups(args, out); err != nil {
		return nil, err
	}
	return out, nil
}

// enforceSelectionGroups validates that every non-empty SelectionGroup has at
// most one selected member. Selection is decided per-type against the
// already-normalized values: a bool counts only when true (a default-filled or
// explicit false is NOT a selection; a mode is on/absent, never a data arg), a
// slice counts only when non-empty, and a numeric/scalar counts only when
// non-zero/non-empty. More than one selected member is a selector-contract
// violation surfaced as ErrSelector; the destructive-ambiguity direction that
// a frontend cannot safely resolve (e.g. pins_rm given both cids and all=true,
// where the handler would otherwise silently unpin-all). The empty direction
// (zero selected) is intentionally NOT rejected here: it is an incomplete input
// the operation's own handler validates with a descriptive message, so the gate
// does not shadow that. Running here (inside normalizeInputDefaults) means both
// Catalog.Invoke and direct NormalizeOperationInput callers share one
// enforcement point.
func enforceSelectionGroups(args []OperationArg, out map[string]any) error {
	// Group members by SelectionGroup, preserving stable order.
	groups := map[string][]OperationArg{}
	var order []string
	for _, a := range args {
		if a.SelectionGroup == "" {
			continue
		}
		if _, seen := groups[a.SelectionGroup]; !seen {
			order = append(order, a.SelectionGroup)
		}
		groups[a.SelectionGroup] = append(groups[a.SelectionGroup], a)
	}
	for _, g := range order {
		var selected []string
		for _, a := range groups[g] {
			if selectorMemberSelected(a, out[a.Name]) {
				selected = append(selected, a.Name)
			}
		}
		if len(selected) > 1 {
			return fmt.Errorf("%w: group %q selected %v", ErrSelector, g, selected)
		}
	}
	return nil
}

// selectorMemberSelected reports whether a normalized selector member value
// counts as selected for its ArgType. Bool requires the value to be true (a
// false is not a selection), slice requires non-empty, and string/numeric/scalar
// require a present, non-zero/non-empty value (a default-filled zero is not a
// selection). time.Duration is a named int64 so its own case is required; it
// never matches the int64 type-switch arm.
func selectorMemberSelected(a OperationArg, value any) bool {
	switch a.Type {
	case ArgTypeBool:
		b, _ := value.(bool)
		return b
	case ArgTypeNullableBool:
		// Treated like a bool in selection groups: only an explicit true is a
		// selected member. nil / false / omitted are all "not selected".
		p, _ := value.(*bool)
		return p != nil && *p
	case ArgTypeNullableInt:
		// Any explicit value (including 0) counts as selected; only the nil/absent
		// state is "not selected". An int of 0 is still an explicit choice.
		p, _ := value.(*int)
		return p != nil
	case ArgTypeStringSlice:
		s, _ := value.([]string)
		return len(s) > 0
	default: // String, Int, Float, Duration, etc.
		switch v := value.(type) {
		case string:
			return v != ""
		case int:
			return v != 0
		case int64:
			return v != 0
		case float64:
			return v != 0
		case time.Duration:
			return v != 0
		default:
			return value != nil
		}
	}
}

// defaultValue parses an OperationArg's string Default into the Go value of the
// matching ArgType. Invalid values fall back to the zero value rather than
// erroring, matching how urfave treats a bad DefaultText (display-only).
func defaultValue(a OperationArg) any {
	switch a.Type {
	case ArgTypeBool:
		return a.Default == "true"
	case ArgTypeNullableBool:
		v := a.Default == "true"
		return &v
	case ArgTypeInt:
		n, _ := strconv.Atoi(a.Default)
		return n
	case ArgTypeNullableInt:
		n, _ := strconv.Atoi(a.Default)
		return intPtr(n)
	case ArgTypeFloat:
		f, _ := strconv.ParseFloat(a.Default, 64)
		return f
	case ArgTypeDuration:
		d, _ := time.ParseDuration(a.Default)
		return d
	case ArgTypeStringSlice:
		// urfave's StringSlice splits a value on commas (SliceFlagSeparator),
		// so --tags a,b reaches the Handler as ["a","b"]. Split the declared
		// default the same way to keep the default and explicit paths
		// shape-identical, dropping empty segments from trailing commas.
		parts := strings.Split(a.Default, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	case ArgTypeRawJSON:
		// A raw-JSON arg has no scalar default; a declared Default (a JSON
		// string) is returned as a string so CLI/`--where` parity holds.
		return a.Default
	default: // ArgTypeString
		return a.Default
	}
}

// inputSchemaFromArgs builds a minimal, correct JSON Schema object describing
// the operation's inputs. Each OperationArg becomes a property typed by its
// ArgType; required args are collected into the top-level "required" array.
func inputSchemaFromArgs(name string, args []OperationArg) json.RawMessage {
	props := make(map[string]any, len(args))
	for _, a := range args {
		// A RawSchema arg overrides the entire property object: the flat
		// ArgType-to-JSON-Type mapping cannot express a rich structured schema
		// (e.g. an array of predicate objects), so the author supplies it
		// verbatim. It must be a JSON object. The sensitive marker and
		// description are merged in so a raw schema can still carry them.
		if len(a.RawSchema) > 0 {
			var raw any
			if err := json.Unmarshal(a.RawSchema, &raw); err != nil || raw == nil {
				raw = map[string]any{}
			}
			p, ok := raw.(map[string]any)
			if !ok {
				p = map[string]any{}
			}
			if a.Sensitive {
				p[SensitiveSchemaKey] = true
			}
			props[a.Name] = p
			continue
		}
		p := map[string]any{"type": jsonType(a.Type)}
		// A string-slice arg renders as a JSON Schema "array". Every array type
		// must declare its element schema or strict hosts (e.g. AWS Bedrock and
		// several MCP validators) reject the tool at registration with "array
		// type must have items". Only ArgTypeStringSlice maps to "array" today.
		if a.Type == ArgTypeStringSlice {
			p["items"] = map[string]any{"type": "string"}
		}
		// Advertise allowed values for an enum-constrained arg, so a model on
		// the MCP surface gets the guidance instead of guessing (which would
		// only be rejected at dispatch).
		if len(a.Enum) > 0 {
			vals := make([]any, len(a.Enum))
			for i, e := range a.Enum {
				vals[i] = e
			}
			p["enum"] = vals
		}
		// Carry agent-oriented help through the schema as the property
		// description. The MCP surface is the agent, so AgentHelp wins when
		// declared; otherwise the human Help still gives the model something.
		desc := a.Help
		if a.AgentHelp != "" {
			desc = a.AgentHelp
		}
		if desc != "" {
			p["description"] = desc
		}
		// Carry the sensitive marker through the schema so the MCP/adapter layer
		// can redact a secret arg's value from logs and echoed tool calls. The
		// CLI already flags it in help text; without this, the arg would be
		// indistinguishable from a normal one on the model surface.
		if a.Sensitive {
			p[SensitiveSchemaKey] = true
		}
		props[a.Name] = p
	}
	// Requiredness comes from mcpRequiredNames: the shared predicate
	// (isRequiredArg) plus any AgentRequired arg. This is the MCP JSON-Schema,
	// so the required array must reflect what the agent surface enforces; the
	// CLI flag generation uses the shared requiredArgNames, untouched by
	// AgentRequired. An arg that is Required but declares a Default is
	// satisfied by the default and so is not required from the agent's
	// perspective.
	required := mcpRequiredNames(args)
	sch := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		sch["required"] = required
	}
	// NOTE: exactly-one-of SelectionGroup members are deliberately NOT expressed
	// as a root-level JSON Schema oneOf: several hosting surfaces (AWS Bedrock,
	// Copilot, and other MCP/model gateways) implement only a subset of JSON
	// Schema and reject oneOf/anyOf/allOf at the top level of input_schema. The
	// XOR contract is instead enforced at dispatch by enforceSelectionGroups
	// (feeding ErrSelector), which is the authoritative gate.
	//
	// If we fail to marshal (cannot happen for these fixed types), fall back
	// to an empty object schema rather than returning nil.
	raw, err := json.Marshal(sch)
	if err != nil {
		raw = []byte(`{"type":"object","properties":{}}`)
	}
	return raw
}

// jsonType maps an ArgType to its JSON Schema "type". Slices become "array";
// everything else maps to its scalar JSON type. A nullable bool maps to the
// array ["boolean","null"] so the schema admits both an explicit boolean and
// JSON null (the "omitted" state survives as null, mirroring the *bool shape
// the Handler receives).
func jsonType(t ArgType) any {
	switch t {
	case ArgTypeString:
		return "string"
	case ArgTypeBool:
		return "boolean"
	case ArgTypeNullableBool:
		return []string{"boolean", "null"}
	case ArgTypeNullableInt:
		// Same tri-state rationale as a nullable bool: the schema admits an
		// explicit integer or JSON null (the "omitted" state survives as null,
		// mirroring the *int shape the Handler receives).
		return []string{"integer", "null"}
	case ArgTypeInt:
		return "integer"
	case ArgTypeFloat:
		return "number"
	case ArgTypeDuration:
		return "string"
	case ArgTypeStringSlice:
		return "array"
	case ArgTypeRawJSON:
		// The JSON type is "any": the resolved value may be a string (CLI) or
		// a decoded array/object (MCP). The paired RawSchema advertises the
		// concrete structured shape on the MCP surface.
		return []string{"string", "array", "object"}
	case ArgTypeFlexibleID:
		// Accept a string or integer id so a model can pass either the form
		// ipns_keys_list emits (integer) or a string id/name.
		return []string{"string", "integer"}
	default:
		return "string"
	}
}

func (a Actor) String() string {
	switch a {
	case ActorModel:
		return "model"
	case ActorApp:
		return "app"
	case ActorHuman:
		return "human"
	default:
		return fmt.Sprintf("Actor(%d)", int(a))
	}
}
