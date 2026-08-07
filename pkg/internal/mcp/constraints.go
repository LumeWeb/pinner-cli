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
