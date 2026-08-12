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
	Description string // respects audience (AgentDescription for model actors)
	// InputSchema is a minimal JSON Schema built from the operation's Args
	// (object with per-arg properties plus a "required" list).
	InputSchema json.RawMessage
	Safety      Safety
	Interaction Interaction
	Visibility  Visibility
	Category    string
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
	// to the acting actor. The Description field respects the audience:
	// AgentDescription when the caller is a model actor, Description otherwise.
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

// validateDefault checks that a non-empty string Default parses for its ArgType.
func validateDefault(a OperationArg) error {
	switch a.Type {
	case ArgTypeBool:
		if a.Default != "true" && a.Default != "false" {
			return fmt.Errorf("invalid bool default %q", a.Default)
		}
	case ArgTypeInt:
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
	return descriptorFor(op, actor), true
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

// descriptorFor builds a ToolDescriptor from an Operation. With a model actor the
// Description field uses AgentDescription (falling back to Description when the
// agent-specific text is empty); with a human/app actor it uses Description. It
// is the single builder used by both Catalog.Describe and the MCP compiler, so
// both produce an identical descriptor for the same operation and actor.
func descriptorFor(op Operation, actor Actor) ToolDescriptor {
	desc := op.Description()
	if actor == ActorModel && op.AgentDescription() != "" {
		desc = op.AgentDescription()
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
	}
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
		return nil, fmt.Errorf("operation %q requires a human: refused for actor %s", name, actor)
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
	// confirmation. Signal that via the ErrConfirmRequired sentinel.
	if op.Safety() == SafetyDestructive && actor == ActorModel {
		return nil, fmt.Errorf("operation %q is destructive: %w", name, ErrConfirmRequired)
	}

	h := op.Handler()
	if h == nil {
		return nil, fmt.Errorf("operation %q has no handler", name)
	}
	// Required-arg validation is shared with the CLI surface (firstMissingRequiredArg):
	// an arg is mandatory only when Required AND has no Default (the default
	// satisfies it via normalizeInputDefaults below), and a present-but-empty or
	// nil string/slice counts as missing too. Both frontends therefore reject the
	// same inputs before a Handler runs.
	if missing := firstMissingRequiredArg(op.Args(), input); missing != nil {
		return nil, fmt.Errorf("operation %q: missing required argument %q", name, missing.Name)
	}
	normalized, err := normalizeInputDefaults(op.Args(), input)
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", name, err)
	}
	return h.Execute(ctx, normalized)
}

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
		// silent invalid value.
		if len(a.Enum) > 0 {
			for _, e := range a.Enum {
				if s == e {
					return s, stateFilled, nil
				}
			}
			return nil, stateInvalid, fmt.Errorf("value %q not in enum %v", s, a.Enum)
		}
		return s, stateFilled, nil
	case ArgTypeBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, stateInvalid, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, stateFilled, nil
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
	case ArgTypeString:
		return ""
	case ArgTypeInt:
		return 0
	case ArgTypeFloat:
		return float64(0)
	case ArgTypeBool:
		return false
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

// firstMissingRequiredArg returns the first required arg whose resolved state
// is Absent, Null, or Empty (Required with no default, and not a filled value).
// It is the enforcement point shared by Invoke and the CLI actionFor so both
// frontends reject the same inputs before a Handler runs. Returns nil when every
// required arg is satisfied. Classification comes from resolveArg, the same
// function normalizeInputDefaults uses.
func firstMissingRequiredArg(args []OperationArg, input map[string]any) *OperationArg {
	for i := range args {
		a := args[i]
		if !isRequiredArg(a) {
			continue
		}
		raw, present := input[a.Name]
		_, st, _ := resolveArg(a, raw, present)
		if st == stateAbsent || st == stateNull || st == stateEmpty {
			return &args[i]
		}
	}
	return nil
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
		raw, present := out[a.Name]
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
	return out, nil
}

// defaultValue parses an OperationArg's string Default into the Go value of the
// matching ArgType. Invalid values fall back to the zero value rather than
// erroring, matching how urfave treats a bad DefaultText (display-only).
func defaultValue(a OperationArg) any {
	switch a.Type {
	case ArgTypeBool:
		return a.Default == "true"
	case ArgTypeInt:
		n, _ := strconv.Atoi(a.Default)
		return n
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
		p := map[string]any{"type": jsonType(a.Type)}
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
	// Requiredness comes from requiredArgNames, the shared predicate
	// (isRequiredArg) also used by Invoke and the CLI flagFor/actionFor, so the
	// schema, dispatch, and CLI agree on which args are actually mandatory. An
	// arg that is Required but declares a Default is satisfied by the default
	// and so is not required from the caller's perspective.
	required := requiredArgNames(args)
	sch := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		sch["required"] = required
	}
	// If we fail to marshal (cannot happen for these fixed types), fall back
	// to an empty object schema rather than returning nil.
	raw, err := json.Marshal(sch)
	if err != nil {
		raw = []byte(`{"type":"object","properties":{}}`)
	}
	return raw
}

// jsonType maps an ArgType to its JSON Schema type string. Slices become
// "array"; everything else maps to its scalar JSON type.
func jsonType(t ArgType) string {
	switch t {
	case ArgTypeString:
		return "string"
	case ArgTypeBool:
		return "boolean"
	case ArgTypeInt:
		return "integer"
	case ArgTypeFloat:
		return "number"
	case ArgTypeDuration:
		return "string"
	case ArgTypeStringSlice:
		return "array"
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
