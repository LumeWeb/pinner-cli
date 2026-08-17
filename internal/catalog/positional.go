package catalog

import (
	"fmt"
	"regexp"
	"strings"
)

// rePositionalName matches each <name> token in an op's Positional usage
// string (e.g. "[<website>] <domain>" -> ["website", "domain"]). This is the
// single source of truth for how a Positional declaration maps to named args;
// every frontend (CLI positional mapping, MCP _args) relies on it instead of
// re-interpreting the string ad hoc.
var rePositionalName = regexp.MustCompile(`<([^>]+)>`)

// positionalNames returns the ordered argument names declared in a Positional
// usage string, in declaration order.
func positionalNames(pos string) []string {
	var names []string
	for _, m := range rePositionalName.FindAllStringSubmatch(pos, -1) {
		names = append(names, m[1])
	}
	return names
}

// MapPositionalArgs maps a frontend-supplied positional argument list onto the
// named string arguments declared by a Positional usage string, and validates
// the count. It is the single canonical positional-mapping rule shared by every
// frontend (CLI + MCP), keeping the arg-position contract in the framework
// rather than hand-parsed per frontend.
//
// Mapping is right-aligned so an optional leading slot ([<website>]) can be
// omitted while remaining args still land in the required trailing slots:
//   - `domains add example.com`        (1 arg, "<domain>" slot)  -> domain
//   - `domains add my-site example.com`(2 args, "<website> <domain>") -> both
//
// A positional slot is bound to the OperationArg of the same name; when the
// Positional placeholder names no declared arg (some ops use a user-facing
// label like "<domain>" for a positional that drives a "website" arg), the slot
// falls back to the operation's first string arg. Values already present in
// input (e.g. populated from a flag) are never overwritten. It returns an error
// when more positionals are supplied than the declaration allows, so surplus
// arguments are rejected instead of silently dropped — mirroring legacy
// per-command validation (a destructive op like `domains rm good.example bogus`
// must not operate on good.example while ignoring bogus).
func MapPositionalArgs(args []OperationArg, pos string, supplied []string, input map[string]any) error {
	names := positionalNames(pos)
	n := len(supplied)
	if len(names) == 0 || n == 0 {
		return nil
	}
	if n > len(names) {
		return fmt.Errorf("unexpected extra argument %q (expected at most %d positional argument(s): %s)",
			supplied[len(names)], len(names), strings.Join(names, " "))
	}
	start := len(names) - n
	for i := start; i < len(names); i++ {
		slot := names[i]
		argName := resolvePositionalArgName(args, slot)
		// Supplying the same arg both as a flag and positionally is ambiguous —
		// reject it instead of silently preferring one (the legacy commands
		// errored, e.g. "website provided both as --website and positionally").
		if existing := StrArg(input, argName, ""); existing != "" {
			return fmt.Errorf("%s provided both as a flag and as a positional argument; use one form", argName)
		}
		input[argName] = supplied[i-start]
	}
	return nil
}

// resolvePositionalArgName binds a Positional placeholder slot to the OperationArg
// it drives. It prefers an arg whose name matches the placeholder; when the
// placeholder names no declared arg (a user-facing label like "<domain>" that
// actually drives a "website" arg), it falls back to the first string arg so the
// positional still lands on the intended named input.
func resolvePositionalArgName(args []OperationArg, slot string) string {
	for _, a := range args {
		if a.Name == slot {
			return a.Name
		}
	}
	for _, a := range args {
		if a.Type == ArgTypeString {
			return a.Name
		}
	}
	return slot
}
