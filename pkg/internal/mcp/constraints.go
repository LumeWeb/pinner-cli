package mcp

import (
	"github.com/urfave/cli/v3"
)

// Constraint describes JSON-schema constraints to emit for a flag so MCP
// agents see the valid value domain up front instead of guessing. It is the
// single source of truth for those constraints: they are declared next to the
// flag definition and read by the adapter, so they cannot drift from the CLI
// validation logic.
type Constraint struct {
	// Enum lists the only valid values for the flag.
	Enum []string
}

// ConstraintProvider is implemented by flags that declare a value domain the
// MCP adapter should surface. Flags that do not implement it are exposed
// without enum or bound constraints.
type ConstraintProvider interface {
	Constraint() Constraint
}

// enumStringFlag is a cli.StringFlag that carries an explicit set of valid
// values. It implements cli.Flag via the embedded *cli.StringFlag and
// ConstraintProvider for the adapter.
type enumStringFlag struct {
	*cli.StringFlag
	enum []string
}

func (f *enumStringFlag) Constraint() Constraint {
	return Constraint{Enum: f.enum}
}

// EnumStringFlag returns a string flag that only accepts the given enum
// values. The enum list is declared in the same expression as the flag, so it
// is the single source of truth and cannot drift from the CLI's validation.
func EnumStringFlag(name, usage string, required bool, value string, enum ...string) cli.Flag {
	return &enumStringFlag{
		StringFlag: &cli.StringFlag{
			Name:     name,
			Usage:    usage,
			Required: required,
			Value:    value,
		},
		enum: enum,
	}
}

// SensitiveProvider is implemented by flags whose value is credential material
// (password, token, API key, passphrase) and must never be logged verbatim.
// Like ConstraintProvider, it is declared next to the flag definition and read
// by the MCP adapter, so the redaction vocabulary cannot drift from the CLI's
// flag declarations.
type SensitiveProvider interface {
	// Sensitive reports whether this flag's value must be redacted.
	Sensitive() bool
}

// sensitiveStringFlag is a cli.StringFlag that carries a sensitive marker. It
// implements cli.Flag via the embedded *cli.StringFlag and SensitiveProvider
// for the adapter's arg-trace redaction.
type sensitiveStringFlag struct {
	*cli.StringFlag
	sensitive bool
}

func (f *sensitiveStringFlag) Sensitive() bool {
	return f.sensitive
}

// SensitiveStringFlag marks an existing string flag's value as credential
// material: the MCP adapter redacts it from the in-process arg-trace log. It
// wraps the fully-specified flag (preserving Aliases, Sources, and any other
// fields) and adds the sensitivity marker, so the marker lives in the same
// expression as the flag definition and cannot drift from the CLI.
func SensitiveStringFlag(flag *cli.StringFlag) cli.Flag {
	return &sensitiveStringFlag{
		StringFlag: flag,
		sensitive:  true,
	}
}

// sensitiveFlagNames returns the long flag names that declare sensitivity via
// SensitiveProvider. It lets the adapter build the redaction set from the
// command's actual flag declarations rather than a separately-maintained
// hardcoded name list.
func sensitiveFlagNames(flags []cli.Flag) []string {
	var out []string
	for _, flag := range flags {
		sp, ok := flag.(SensitiveProvider)
		if !ok || !sp.Sensitive() {
			continue
		}
		switch f := flag.(type) {
		case *sensitiveStringFlag:
			if f.StringFlag != nil && f.Name != "" {
				out = append(out, f.Name)
			}
		}
	}
	return out
}

// unionSensitiveFlags merges two sensitive flag-name lists, de-duplicating so
// a name shared by a command and the root appears once. Order is preserved
// with the second list appended after the first.
func unionSensitiveFlags(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, lists := range [][]string{a, b} {
		for _, name := range lists {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
