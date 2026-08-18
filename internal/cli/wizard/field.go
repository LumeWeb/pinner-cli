package wizard

import (
	"context"
	"fmt"
)

// This file defines a declarative field-resolution primitive for wizard steps.
// A step describes the values it needs and the framework resolves each one in
// precedence order — provider-derived (precedence 0), CLI switch (and its
// process-env Sources), interactive prompt, persisted env file — instead of the
// step hand-rolling a per-field conditional on the state and interactivity.
//
// Provenance, two channels per value:
//
//	Decided      — set only by an operator switch or interactive prompt. A
//	               non-nil pointer is an operator decision for this run: it
//	               survives a provider switch and serializes verbatim.
//	Operational  — the current working value: provider-derived (resolved URL,
//	               loaded cloudflare state, resolved credential, a default) or
//	               folded from a persisted env file for headless reuse. It is
//	               not an operator decision. It is used to validate,
//	               prompt-prefill, and execute, and is cleared on a provider
//	               switch for fields the new provider re-derives.
//
// The split exists because configurers write derived values (ngrok API URL,
// the "pinner-mcp" TunnelName default, resolved credentials) that would be
// mistaken for operator decisions by a naive "non-nil = decided" tri-state.
//
// S MUST be a pointer type (e.g. *ServiceInstallState), matching the wizard's
// Run which passes state S by value: with a pointer S the copied value aliases
// the same struct, so Commit persists. Accessors take S (the pointer) directly.

// ValueSource abstracts where an operator-supplied value comes from. The
// wizard package does not import urfave/cli; the host adapts its command and
// env layers to this interface. Sources are strings (flags and env are
// strings); Field.Parse converts them to T.
type ValueSource interface {
	// Flag returns a value for a switch, honoring both an explicitly-passed
	// flag and its declared process-env Sources (what cli's cmd.String returns
	// today). set=false when neither is present.
	Flag(name string) (value string, set bool)
	// EnvFile returns a persisted env-file value (the MCP_* file the service
	// actually reads at runtime). ok=false when the key is absent.
	EnvFile(key string) (value string, ok bool)
}

// Prompt describes HOW to ask interactively for a field that is not decided by
// a switch and not operatively resolved. The Prompter is looked up from ctx
// (the framework's single terminal channel via PrompterFrom).
type Prompt[T any] struct {
	Label string // prompt label, e.g. "Tunnel domain (required)"
	// Mask, when non-empty (e.g. "*"), hides input (secrets). Ignored for
	// Select-style fields.
	Mask string
	// Options, when non-empty, renders a single-choice Select instead of a
	// free-text Text prompt. The current value is highlighted as the default.
	Options []string
	// CurrentString renders the current Operational value for the editable /
	// highlighted default. Required when Prompt is set.
	CurrentString func(T) string
}

// Field describes one configurable value a wizard step needs. See the provenance
// model above for Decided vs Operational. S MUST be a pointer type.
type Field[S any, T any] struct {
	Name string

	// Parse converts a source string (CLI flag, env file, or selected prompt
	// option) into a T. Return ok=false if the raw string is malformed for this
	// field. For the common string-valued fields this is an identity parse.
	Parse func(string) (T, bool)

	// Decided returns the operator-decision pointer (nil = not decided this run).
	Decided func(S) *T
	// Commit persists an operator decision (switch or prompt) into the step state.
	Commit func(S, T)

	// Operational returns the current working value (provider-derived or
	// env-folded), used for validation and prompt prefill.
	Operational func(S) T
	// SetOperational writes a provider-derived or env-folded value. It is not
	// an operator decision and does not set the Decided channel.
	SetOperational func(S, T)

	// Flag is the CLI switch name (precedence 1). "" = no switch.
	Flag string
	// EnvFileKey is the persisted env-file key (precedence 3 / headless reuse).
	// "" = not persisted.
	EnvFileKey string

	// Validate reports whether a value is complete/valid. nil field = always
	// valid. Enforced at prompt time (invalid decided values re-prompt) and on
	// headless (an invalid folded/derived value surfaces as a step error).
	Validate func(T) bool

	// Prompt is how to ask interactively when the field needs a decision.
	// nil = not promotable (a pure switch/env field).
	Prompt *Prompt[T]

	// ReDerives marks a field whose Operational value is re-derived by the
	// install when the tunnel provider changes: on a switch, the framework
	// clears its Operational value (and the reconcile purges its env key) so a
	// stale derived value never survives. Fields with ReDerives == false keep
	// their Operational value across a switch (e.g. Host — a local bind host
	// that is never stale).
	ReDerives bool

	// Derived supplies the field's Operational value from the provider's own
	// derivation (loaded cloudflare provisioned state, an ngrok URL resolved
	// from the account API, a "pinner-mcp" TunnelName default, a resolved
	// credential). It runs at precedence 0 — before the CLI switch — and only
	// when the field carries no operator decision this run. A derived value is
	// written to the Operational channel only (never Decided) and settles the
	// field: a headless run reuses it, an interactive run prefills the prompt
	// with it as the editable default. It is the declarative replacement for a
	// provider imperatively deriving a value in a step's Execute before/after
	// Gather — and, like ReDerives, a field with a Derived hook is by
	// definition re-derived on a provider switch and must not be folded from a
	// stale env file. Precedence 0 returning ok=false just falls through to the
	// switch / decision / env precedences.
	Derived func(S) (T, bool)

	// When, when non-nil, gates whether this field is evaluated at all. A
	// field whose When(s) is false is skipped entirely by Gather — it is
	// neither resolved nor prompted nor hard-errored — so a field that only
	// applies under a prior choice (a DNS-mode-specific value, a transport
	// gated on an agent set) is declared declaratively instead of the step
	// assembling a different field set by hand. Typical use: read a value the
	// step or an earlier field wrote to S and return false to suppress this
	// field. nil = always evaluated.
	When func(S) bool
}

// FieldError is returned by Gather when a required field is unresolved on a
// headless run (cannot prompt). It names the field for a useful message.
type FieldError struct {
	Name string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("field %q is required but unresolved in non-interactive mode", e.Name)
}

// fieldOutcome is a resolved field's result after the non-prompt precedence.
type fieldOutcome[T any] struct {
	value      T
	decided    bool // operator decision this run (switch or prompt commit)
	operative  bool // has an operative value usable without re-derivation
	usedSource string
	hardError  bool // headless, required, unresolved -> fatal
}

// srcKind is the provenance of a candidate value. It selects which channel the
// value is written to and which source label (if any) it exposes to Gather.
type srcKind int

const (
	// srcDerived is a provider-derived value (precedence 0). It writes the
	// Operational channel only — never Decided.
	srcDerived srcKind = iota
	// srcFlag is an explicitly-passed CLI switch (incl. its process-env Sources).
	srcFlag
	// srcDecided is an operator decision committed by an earlier pass.
	srcDecided
	// srcEnv is a value folded from the persisted env file.
	srcEnv
	// srcPrompt is an interactive prompt (or selection) just answered.
	srcPrompt
)

// settle applies the single validation gate and provenance write for a
// candidate value. It returns true when the value is valid and accepted. It is
// the only place Validate is enforced and the only place a resolved source
// writes the Decided vs Operational channel, so every source branch shares one
// rule: an invalid candidate is discarded (never reused, never partially
// written) and the caller falls through to the next precedence or to
// classifyOutcome.
func (f *Field[S, T]) settle(oc *fieldOutcome[T], s S, v T, src srcKind, headless bool) bool {
	if f.Validate != nil && !f.Validate(v) {
		return false
	}
	oc.value = v
	switch src {
	case srcFlag, srcPrompt:
		// An operator value is both a decision and the operative value.
		f.Commit(s, v)
		f.SetOperational(s, v)
		oc.decided = true
		oc.operative = true
		if src == srcFlag {
			oc.usedSource = f.Flag
		}
	case srcDecided:
		// Already persisted on both channels; reuse exposes it as settled.
		oc.decided = true
		oc.operative = true
	case srcDerived, srcEnv:
		// Derived and env values fold into Operational only, never Decided.
		// They settle the field only on a headless run (reuse); on interactive
		// they prefill the prompt and stay un-operative so Gather re-prompts
		// with the value as the editable default.
		f.SetOperational(s, v)
		if src == srcEnv {
			oc.usedSource = "env file"
		}
		oc.operative = headless
	}
	return true
}

// classifyOutcome is the single place that maps "no candidate settled" to a
// terminal outcome. Called from resolveField (the normal tail, and the
// present-but-invalid-flag short-circuit). All hard-error / defer / interactive
// decisions live here:
//   - headless + a required field with nothing settled => hard-error, except a
//     ReDerives field, which defers to the step's Execute (it derives the value
//     via SetOperational afterwards).
//   - interactive => surface the current operational value as a prompt default;
//     a valid value settles the field, so Gather skips the prompt.
func classifyOutcome[S any, T any](oc *fieldOutcome[T], f *Field[S, T], s S, headless bool) {
	if oc.decided || oc.operative {
		return // already settled; do not clobber
	}
	if headless {
		// A field the provider derives — via a Derived hook or a ReDerives
		// marker — defers to the step's Execute rather than hard-erroring: the
		// provider populates it via SetOperational after Gather (e.g. an ngrok
		// URL resolved in Finalize). Any other unresolved required field is
		// fatal headless (it cannot be prompted).
		if !f.ReDerives && f.Derived == nil {
			oc.hardError = true
		}
		return
	}
	cur := f.Operational(s)
	oc.value = cur
	if f.Validate == nil || f.Validate(cur) {
		oc.operative = true
	}
}

// flagPresent reports whether src carries an explicit value for the given flag
// this run. It is how the precedences decide "the operator supplied a switch":
// a field whose flag is undefined or simply un-passed does not count as present,
// so a derived field with an optional flag still derives.
func flagPresent(src ValueSource, flag string) bool {
	_, ok := src.Flag(flag)
	return ok
}

// resolveField applies one field's precedence: provider-derived (precedence 0) >
// switch (incl. Sources) > existing operator decision > headless env fold. Each
// precedence settles via f.settle (the single validation + provenance gate) and
// returns on acceptance; anything that does not settle funnels once into
// classifyOutcome, which owns the hard-error / defer / interactive outcome. It
// does not prompt; Gather does, since it has the Prompter from ctx.
func resolveField[S any, T any](src ValueSource, s S, f *Field[S, T], headless bool) (fieldOutcome[T], error) {
	oc := fieldOutcome[T]{}

	// -- precedence 0: provider-derive the Operational value ----------------
	// Resolves a derived value (cloudflare provisioned state, ngrok URL, a
	// default) BEFORE the switch, and only when the field carries no operator
	// decision this run and no explicit --flag was passed. An explicit operator
	// switch beats provider derivation (precedence 1 > precedence 0), so a
	// derived value must never silently discard a flag the operator actually
	// supplied. A defined-but-unpassed flag does not count as present, so an
	// optional flag still falls through to derivation. A derived value settles
	// the field (headless reuses it, interactive prefills the prompt default),
	// so a provider-derived field is never hard-errored for being unresolved
	// before derivation can fill it — the gap that forced providers to derive
	// imperatively around Gather. ok=false falls through to the lower
	// precedences.
	if f.Derived != nil && !flagPresent(src, f.Flag) && f.Decided(s) == nil {
		if v, ok := f.Derived(s); ok && f.settle(&oc, s, v, srcDerived, headless) {
			return oc, nil
		}
	}

	// -- precedence 1: CLI switch (incl. process-env Sources) ----------------
	// A present flag decides the field, valid or not: if it settles we are done;
	// if it is invalid, classifyOutcome produces the terminal outcome (hard-error
	// headless, prompt/defer interactive). We do not fall through to lower
	// precedence sources, because the operator explicitly supplied a value.
	if f.Flag != "" {
		if raw, ok := src.Flag(f.Flag); ok {
			if v, parsed := f.Parse(raw); parsed && f.settle(&oc, s, v, srcFlag, headless) {
				return oc, nil
			}
			classifyOutcome(&oc, f, s, headless)
			return oc, nil
		}
	}

	// -- precedence 2: existing operator decision ---------------------------
	// Reused only while it still satisfies Validate; an invalid decision (e.g.
	// entered under a stricter prior rule) falls through so headless hard-errors
	// and interactive re-prompts, matching the "invalid decided values
	// re-prompt" contract.
	if cur := f.Decided(s); cur != nil && f.settle(&oc, s, *cur, srcDecided, headless) {
		return oc, nil
	}

	// -- precedence 3: fold the persisted env as the current value ----------
	// Runs on headless and interactive: a re-run prefills its prompts with (and
	// on headless reuses) the persisted config. Folds into the Operational
	// channel only, never Decided, and only for fields not marked ReDerives and
	// without a Derived hook (the new provider derives those itself and must
	// not get a stale cross-provider value). settle's srcEnv rule completes the
	// behavior: settle-on-headless, prefill-on-interactive.
	if f.EnvFileKey != "" && !f.ReDerives && f.Derived == nil && f.Decided(s) == nil {
		if raw, ok := src.EnvFile(f.EnvFileKey); ok {
			if v, parsed := f.Parse(raw); parsed && f.settle(&oc, s, v, srcEnv, headless) {
				return oc, nil
			}
		}
	}

	// Single outcome policy shared by every path above.
	classifyOutcome(&oc, f, s, headless)
	return oc, nil
}

// Gather resolves a set of fields against the value source for a step, driving
// interactive prompts through the Prompter bound to ctx. It implements the
// "switch > prompt > env" precedence loop once, so steps declare fields instead
// of hand-rolling it. It is the typed convenience over GatherAny: the loop,
// gating (Field.When), derivation (Field.Derived), and prompting all live in
// GatherAny / AnyField, so a heterogeneous field set resolves the same way.
//
// It returns (seededSources, fullyDecided, error):
//   - seededSources are the distinct source labels used (a switch name, or
//     "env file") for the "Seeded from <sw>" banner.
//   - fullyDecided is true when every field has an operative value and the
//     honest-source rule holds: either no env-file source was used or the run
//     is headless. An env-file-sourced value keeps the step un-seeded on an
//     interactive run (prompts with defaults) but fully-seeded on headless.
//   - error is returned only for a hard headless failure (required unresolved).
//
// S MUST be a pointer type; the wizard's Run passes S by value but with
// pointer-S the copied value aliases the same struct, so commits persist.
// Callers must pass a non-nil, live state.
func Gather[S any, T any](ctx context.Context, src ValueSource, s S, fields []Field[S, T]) ([]string, bool, error) {
	anyf := make([]AnyField[S], len(fields))
	for i := range fields {
		anyf[i] = erase(&fields[i])
	}
	return GatherAny(ctx, src, s, anyf)
}
