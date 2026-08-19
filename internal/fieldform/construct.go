package fieldform

import (
	"fmt"
)

// This file is the functional surface of the field framework. The intent: a host
// declares a field as a single function call carrying only the irreducible
// inputs — the typed Operational accessors (get/set, the reflection-free handle
// to a struct member) plus the field's Name and optional declarative metadata
// (Meta). All the surrounding meta-programming (Decide/Commit derivation,
// type-aware Parse, prompt defaults, Field projection, erasure to AnyField for
// mixed-type sets, JSON-schema emission) is hidden inside these constructors.
//
// Two shapes drive which constructor a field uses:
//
//   - Value-typed state fields (Domain string) keep Decided distinct from
//     Operational via a host-level decisions map, because the value itself
//     cannot encode nil=undecided. They are declared with Str, passing the
//     host's single Decided binding:
//
//	dec := fieldform.Decided[*Config, string]{
//	    Read:  func(s *Config, n string) *string { return s.decisions[n] },
//	    Write: func(s *Config, n, v string) { s.decisions[n] = &v },
//	}
//	fieldform.Str(dec, "Domain", get, set, fieldform.Meta{Flag: "domain"})
//
//   - Pointer-typed state fields (OAuth *bool, Port *int) encode the decision
//     channel in the pointer itself (nil = undecided). They need NO Decided
//     binding and use Bool/Int/Enum:
//
//	fieldform.Bool("OAuth", getOAuth, setOAuth, fieldform.Meta{Flag: "oauth"})
//
// Either way the returned value is an already-erased AnyField[S] ready for
// GatherAny and FormSchema.

// Meta is the optional declarative metadata of a field. A zero value is fine.
// It is a single plain value rather than a set of positional args, so a field
// declaration reads as `Str(dec, "Domain", get, set, Meta{Flag:"domain"})`.
type Meta struct {
	Flag       string   // CLI switch (precedence 1). "" = none.
	EnvFileKey string   // persisted env-file key (precedence 3). "" = none.
	ReDerives  bool     // cleared on provider switch.
	Required   bool     // mandatory in a form (FormSchema Required array). Default optional.
	Mask       string   // non-empty (e.g. "*") renders secret input -> schema format password.
	Default    any      // precedence-4 fallback default, applied only when no flag/decision/env resolves the field (see Field.DefaultVal).
	Options    []string // static choice list (Enum/Str select, Multi).
	Validate   func(any) bool
	When       func(any) bool
}

// Decided binds the host's operator-decision persistence for VALUE-typed fields:
// typically a name-keyed map on the state. A host builds ONE of these and passes
// it to every Str field; Str derives its Decide/Commit closures from it by Name
// (so env-fold stays undecided while a switched/prompted value is decided).
type Decided[S any, T any] struct {
	// Read returns the decision pointer for a field name (nil = not decided).
	Read func(S, string) *T
	// Write records a decision for a field name. It should only write the
	// decision channel; Str's Commit applies the value write via the setter.
	Write func(S, string, T)
}

// Str declares a VALUE-typed string field whose Decided channel is a separate
// name-keyed store (the host's Decided binding), so an env-folded value is not
// mistaken for an operator decision.
func Str[S any](dec Decided[S, string], name string, get func(S) string, set func(S, string), m Meta) AnyField[S] {
	spec := fieldSpec[S, string]{
		Name:       name,
		Flag:       m.Flag,
		EnvFileKey: m.EnvFileKey,
		ReDerives:  m.ReDerives,
		Required:   m.Required,
		Get:        get,
		Set:        set,
		Decide:     func(s S) *string { return dec.Read(s, name) },
		Commit:     func(s S, v string) { dec.Write(s, name, v); set(s, v) },
		Parse:      func(raw string) (string, bool) { return raw, true },
	}
	applyPromptMeta(&spec, m)
	return Erase(spec.Field())
}

// Bool declares a POINTER-typed bool field; the pointer itself is both the
// Operational value and the Decided channel (nil = undecided).
func Bool[S any](name string, get func(S) *bool, set func(S, bool), m Meta) AnyField[S] {
	spec := fieldSpec[S, bool]{
		Name:       name,
		Flag:       m.Flag,
		EnvFileKey: m.EnvFileKey,
		ReDerives:  m.ReDerives,
		Required:   m.Required,
		Get:        func(s S) bool { return derefT(get(s)) },
		Set:        set,
		Decide:     get,
		Commit:     func(s S, v bool) { set(s, v) },
		Parse:      func(raw string) (bool, bool) { return parseT[bool](raw) },
	}
	applyPromptMeta(&spec, m)
	return Erase(spec.Field())
}

// Int declares a POINTER-typed int field; the pointer is both the Operational
// value and the Decided channel (nil = undecided).
func Int[S any](name string, get func(S) *int, set func(S, int), m Meta) AnyField[S] {
	spec := fieldSpec[S, int]{
		Name:       name,
		Flag:       m.Flag,
		EnvFileKey: m.EnvFileKey,
		ReDerives:  m.ReDerives,
		Required:   m.Required,
		Get:        func(s S) int { return derefT(get(s)) },
		Set:        set,
		Decide:     get,
		Commit:     func(s S, v int) { set(s, v) },
		Parse:      func(raw string) (int, bool) { return parseT[int](raw) },
	}
	applyPromptMeta(&spec, m)
	return Erase(spec.Field())
}

// Enum declares a POINTER-typed selectable field of type T (e.g. `string`) whose
// choices are the static options. Parse maps a raw source string to T and
// validates membership against the options.
func Enum[S any, T comparable](name string, get func(S) *T, set func(S, T), options []T, m Meta) AnyField[S] {
	spec := fieldSpec[S, T]{
		Name:       name,
		Flag:       m.Flag,
		EnvFileKey: m.EnvFileKey,
		ReDerives:  m.ReDerives,
		Required:   m.Required,
		Get:        func(s S) T { return derefT(get(s)) },
		Set:        set,
		Decide:     get,
		Commit:     func(s S, v T) { set(s, v) },
		Parse:      mapEnumParse[T](options),
		Prompt: &Prompt[T]{
			Label:         name,
			Options:       toOptionStrs(options),
			CurrentString: func(v T) string { return enumString(v) },
		},
	}
	applyPromptMeta(&spec, m)
	return Erase(spec.Field())
}

// applyPromptMeta folds Meta.Mask/Options/Validate/Default into the spec's
// Prompt and validation fields so every constructor shares the same handling.
func applyPromptMeta[S any, T any](spec *fieldSpec[S, T], m Meta) {
	if m.Mask != "" || m.Options != nil {
		if spec.Prompt == nil {
			spec.Prompt = &Prompt[T]{
				Label:         spec.Name,
				CurrentString: defaultCurrentString[T],
			}
		}
		if m.Mask != "" {
			spec.Prompt.Mask = m.Mask
		}
		if m.Options != nil {
			spec.Prompt.Options = m.Options
		}
	}
	if m.Validate != nil {
		validate := m.Validate // capture
		spec.Validate = func(v T) bool { return validate(v) }
	}
	if m.When != nil {
		when := m.When
		spec.When = func(s S) bool { return when(s) }
	}
	if m.Default != nil {
		// Meta.Default must be a T for the field's type; convert via reflect-free
		// type switch so a typed Accessor can set the Derived precedence-0 source.
		applyDefault(spec, m.Default)
	}
}

// applyDefault stores Meta.Default as the field's fallback DefaultVal (applied
// at precedence 4, after flag/decision/env). The default must be a value of the
// field's type T; a non-nil, mismatched default is a caller error and fails
// loudly rather than being silently dropped.
func applyDefault[S any, T any](spec *fieldSpec[S, T], d any) {
	def, ok := d.(T)
	if !ok {
		panic(fmt.Sprintf("fieldform: Meta.Default for %q is %T, want %T", spec.Name, d, *new(T)))
	}
	spec.DefaultVal = &def
}

// ---- small helpers shared by the constructors ----

func defaultCurrentString[T any](v T) string {
	return enumString(v)
}

func enumString[T any](v T) string {
	// Handles plain string and defined string-kinds (e.g. `type P string`)
	// uniformly for option text and raw-string comparison. Other T kinds
	// (bool/int) fall back to fmt.Sprint for the enum label.
	switch t := any(v).(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func toOptionStrs[T any](options []T) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		out = append(out, enumString(o))
	}
	return out
}

func mapEnumParse[T comparable](options []T) func(string) (T, bool) {
	return func(raw string) (T, bool) {
		var zero T
		for _, o := range options {
			if enumString(o) == raw {
				return o, true
			}
		}
		return zero, false
	}
}

// Erase wraps a typed *Field[S, T] as AnyField[S] so heterogeneous field sets
// resolve in one GatherAny pass and feed FormSchema. Hosts that build a field
// via the Str/Bool/Int/Enum constructors get an already-erased AnyField and do
// not need this; it is for hosts composing with the lower-level Field directly.
func Erase[S any, T any](f *Field[S, T]) AnyField[S] {
	return erase(f)
}
