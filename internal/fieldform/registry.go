package fieldform

import (
	"context"
	"fmt"
	"slices"

	"github.com/invopop/jsonschema"
)

// This file adds a field-registry primitive (fieldSpec) and a type-erased
// driver (AnyField) so a wizard step can define every field once and resolve a
// heterogeneous set (string, bool, []Agent, enum...) in a single pass.
//
// A host commonly needs a step that mixes types (a string CID next to a bool
// DNS-hosted, a []Agent next to a string scope). gatherAny resolves such a set
// through AnyField erasure; fieldSpec is the single-source-of-truth template
// that collapses the parallel per-field switch/map structures a host would
// otherwise hand-roll (enum key, ordinal slice, operational/setOperational/
// envKey/reDerives/flag/name switches).

// fieldSpec is the single canonical definition of one field. A host builds a
// registry of these once and derives every wizard.Field view (and the
// Operational/Decided accessor closures) from the one descriptor, so adding a
// field is one entry instead of edits to many parallel structures.
type fieldSpec[S any, T any] struct {
	// Name is the stable field identifier (also used as the prompt fallback
	// label and the seed source).
	Name string
	// Flag is the CLI switch (precedence 1). "" = no switch.
	Flag string
	// EnvFileKey is the persisted env-file key (precedence 3 / headless reuse).
	// "" = not persisted.
	EnvFileKey string
	// ReDerives marks an Operational value that is re-derived on a provider
	// switch (cleared so a stale cross-provider value never survives).
	ReDerives bool
	// Required marks the field as mandatory in the form: FormSchema lists it in
	// the object's Required array. Default false = optional (form auto-advances).
	Required bool
	// DefaultVal is the field-declared fallback default (Meta.Default), applied
	// at precedence 4 after flag/decision/env. nil = no default.
	DefaultVal *T
	// Parse converts a source string (flag, env, prompt option) into T.
	Parse func(string) (T, bool)
	// ParseMulti converts the checked option labels of a Multi field into T.
	// Required only when Prompt.Multi is true.
	ParseMulti func([]string) (T, bool)
	// Validate reports whether T is complete/valid. nil = always valid.
	Validate func(T) bool
	// Get reads the current Operational value.
	Get func(S) T
	// Set writes the Operational value (not Decided).
	Set func(S, T)
	// Decide returns the operator-decision pointer (nil = none this run).
	Decide func(S) *T
	// Commit persists an operator decision (switch or prompt).
	Commit func(S, T)
	// Prompt is how to ask interactively. nil = not promotable.
	Prompt *Prompt[T]
	// OptionsFunc supplies the prompt's choice list at prompt time (API/FS-derived).
	OptionsFunc func(ctx context.Context, src ValueSource, s S) (options []string, err error)
	// Derived supplies the Operational value at precedence 0 (see Field.Derived).
	Derived func(S) (T, bool)
	// When gates whether the field is evaluated at all (see Field.When).
	When func(S) bool
}

// Field builds the *Field[S, T] view described by the spec, wiring the
// accessor closures to the single spec definition.
func (s fieldSpec[S, T]) Field() *Field[S, T] {
	return &Field[S, T]{
		Name:           s.Name,
		Parse:          s.Parse,
		ParseMulti:     s.ParseMulti,
		Decided:        s.Decide,
		Commit:         s.Commit,
		Operational:    s.Get,
		SetOperational: s.Set,
		Flag:           s.Flag,
		EnvFileKey:     s.EnvFileKey,
		Validate:       s.Validate,
		Prompt:         s.Prompt,
		OptionsFunc:    s.OptionsFunc,
		ReDerives:      s.ReDerives,
		Required:       s.Required,
		DefaultVal:     s.DefaultVal,
		Derived:        s.Derived,
		When:           s.When,
	}
}

// derefT dereferences a tri-state pointer value, returning T's zero value when
// the pointer is nil (undecided / not yet set).
func derefT[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// parseT parses a raw source string (CLI flag, env file, prompt option) into T
// via an explicit type switch over the supported field types. No reflection.
func parseT[T any](raw string) (T, bool) {
	var zero T
	var v any
	switch any(zero).(type) {
	case string:
		v = raw
	case bool:
		v = raw == "true" || raw == "1"
	case int:
		n := 0
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
			return zero, false
		}
		v = n
	default:
		return zero, false
	}
	return v.(T), true
}

// AnyField erases the T of a Field so a heterogeneous field set resolves in one
// pass. Callers use Fields / erase to build the slice; the framework dispatches
// each field's own typed resolution.
type AnyField[S any] interface {
	// applies reports whether the field is gated on for the current state
	// (Field.When). A non-applicable field is skipped entirely.
	applies(s S) bool
	// resolve runs the field's full resolution (precedences + interactive
	// prompt) and returns the type-erased outcome the driver aggregates.
	//
	// The driver calls resolve only after applies(s) is true.
	resolve(ctx context.Context, src ValueSource, s S, headless bool) (resolvedField, error)
	// FieldName returns the field's stable name (for FormSchema property keys).
	FieldName() string
	// Schema returns the field's JSON-schema entry (shared emitter).
	Schema() *jsonschema.Schema
	// Required reports whether the field is mandatory in a form (FormSchema
	// lists it in the object's Required array, which keys MCP elicitation).
	Required() bool
	// Declared unwraps the erased field back to its concrete typed *Field so a
	// host can read ReDerives / Operational / SetOperational directly (e.g. a
	// provider-switch clean-up that iterates the registry). The returned value
	// is the underlying *Field[S, T] as any; non-erased implementations return
	// the field unchanged.
	Declared() any
}

// resolvedField is the type-erased outcome of one field's resolution, carrying
// exactly what the gatherAny driver needs to aggregate.
type resolvedField struct {
	name       string
	decided    bool
	operative  bool
	usedSource string
	hardError  bool
	value      any
}

// erasedField wraps a typed *Field[S, T] to implement AnyField[S].
type erasedField[S any, T any] struct {
	f *Field[S, T]
}

func (e *erasedField[S, T]) applies(s S) bool {
	return e.f.When == nil || e.f.When(s)
}

func (e *erasedField[S, T]) resolve(ctx context.Context, src ValueSource, s S, headless bool) (resolvedField, error) {
	oc, err := resolveField(src, s, e.f, headless)
	if err != nil {
		return resolvedField{}, err
	}
	rf := resolvedField{
		name:       e.f.Name,
		decided:    oc.decided,
		operative:  oc.operative,
		usedSource: oc.usedSource,
		hardError:  oc.hardError,
		value:      oc.value,
	}
	if rf.decided || rf.operative || rf.hardError {
		return rf, nil
	}

	// Interactive prompt for a required, still-unresolved field.
	if !headless && e.f.Prompt != nil {
		p := PrompterFrom(ctx)
		if p == nil {
			return rf, fmt.Errorf("wizard.Gather: field %q needs a prompt but no Prompter is bound to ctx", e.f.Name)
		}
		label := e.f.Prompt.Label
		if label == "" {
			label = e.f.Name
		}

		// Resolve the choice list: OptionsFunc (API/FS-derived) overrides the
		// static Prompt.Options. A Multi field keeps its pre-checked defaults.
		options := e.f.Prompt.Options
		if e.f.OptionsFunc != nil {
			derived, oerr := e.f.OptionsFunc(ctx, src, s)
			if oerr != nil {
				return rf, oerr
			}
			options = derived
		}

		// A field whose choice list is derived via OptionsFunc is never a
		// free-text field: an empty derived list is a dead-end (there is
		// nothing to choose), so it errors like a Multi field with no options
		// rather than silently falling through to a Text prompt. The Text
		// fallback below is reserved for fields with no OptionsFunc and no
		// static Options — those are genuinely free-text by design.
		if e.f.OptionsFunc != nil && len(options) == 0 {
			return rf, fmt.Errorf("wizard.Gather: select field %q has no options to choose from", e.f.Name)
		}

		if e.f.Prompt.Multi {
			// Multi-select: checked option labels -> ParseMulti -> T.
			if e.f.ParseMulti == nil {
				return rf, fmt.Errorf("wizard.Gather: multi-select field %q needs Field.ParseMulti", e.f.Name)
			}
			if len(options) == 0 {
				return rf, fmt.Errorf("wizard.Gather: multi-select field %q has no options to choose from", e.f.Name)
			}
			var pre []string
			if e.f.Prompt.CurrentSet != nil {
				pre = e.f.Prompt.CurrentSet(e.f.Operational(s))
			}
			chosen, merr := p.MultiSelect(label, options, pre)
			if merr != nil {
				return rf, merr
			}
			val, okP := e.f.ParseMulti(chosen)
			if !okP || !e.f.settle(&oc, s, val, srcPrompt, headless) {
				return rf, fmt.Errorf("wizard.Gather: invalid selection entered for field %q", e.f.Name)
			}
			rf.decided = oc.decided
			rf.operative = oc.operative
			rf.usedSource = oc.usedSource
			rf.value = oc.value
			return rf, nil
		}

		defStr := ""
		if e.f.Prompt.CurrentString != nil {
			defStr = e.f.Prompt.CurrentString(e.f.Operational(s))
		}
		var chosenStr string
		if len(options) > 0 {
			_, selVal, perr := p.Select(label, options, defStr)
			if perr != nil {
				return rf, perr
			}
			chosenStr = selVal
		} else {
			txt, perr := p.Text(label, e.f.Prompt.Mask, defStr)
			if perr != nil {
				return rf, perr
			}
			chosenStr = txt
		}
		chosen, okP := e.f.Parse(chosenStr)
		if !okP || !e.f.settle(&oc, s, chosen, srcPrompt, headless) {
			return rf, fmt.Errorf("wizard.Gather: invalid value entered for field %q", e.f.Name)
		}
		rf.decided = oc.decided
		rf.operative = oc.operative
		rf.usedSource = oc.usedSource
		rf.value = oc.value
	}
	return rf, nil
}

// FieldName returns the erased field's stable name (used by FormSchema).
func (e *erasedField[S, T]) FieldName() string {
	return e.f.Name
}

// Schema returns the erased field's JSON-schema entry (shared emitter).
func (e *erasedField[S, T]) Schema() *jsonschema.Schema {
	return e.f.Schema()
}

// Required reports whether the field is mandatory in a form.
func (e *erasedField[S, T]) Required() bool {
	return e.f.Required
}

// Declared unwraps the erased field to its concrete typed *Field[S, T].
func (e *erasedField[S, T]) Declared() any {
	return e.f
}

// erase wraps a typed Field as AnyField.
func erase[S any, T any](f *Field[S, T]) AnyField[S] {
	return &erasedField[S, T]{f: f}
}

// GatherAny is Gather's type-erased driver: it resolves a heterogeneous field
// set ([]AnyField[S], built from fieldSpec.Field() / Fields) in one pass, so a
// step mixing string, bool, and enum fields does not need one typed Gather call
// per type. Seeding and honest-source semantics match Gather exactly.
func GatherAny[S any](ctx context.Context, src ValueSource, s S, fields []AnyField[S]) ([]string, bool, error) {
	headless := NonInteractive
	seeded := make([]string, 0, len(fields))
	envUsed := false
	fullyDecided := true

	for _, anyf := range fields {
		// A gated field (When(s) == false) is skipped entirely: never
		// resolved, prompted, or hard-errored. The step's Execute handles the
		// alternative (a different branch) — this only suppresses a field that
		// does not apply under the current state.
		if !anyf.applies(s) {
			continue
		}

		rf, err := anyf.resolve(ctx, src, s, headless)
		if err != nil {
			return nil, false, err
		}
		if rf.hardError {
			return nil, false, &FieldError{Name: rf.name}
		}
		if rf.usedSource == "env file" {
			envUsed = true
		}
		if rf.usedSource != "" && !slices.Contains(seeded, rf.usedSource) {
			seeded = append(seeded, rf.usedSource)
		}
		if !(rf.decided || rf.operative) {
			fullyDecided = false
		}
	}

	if envUsed && !headless {
		fullyDecided = false
	}
	return seeded, fullyDecided, nil
}
