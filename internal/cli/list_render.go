package cli

import "go.lumeweb.com/pinner-cli/internal/catalogops"

// renderListResult renders any *-list operation result (a catalogops.ListResult)
// through the CLI Output formatter. It is the single rendering home for list
// commands: JSON emits a uniform {count, items} shape; human mode prints a
// "No X found" / "Found N X" line followed by the table.
func renderListResult(output Output, r catalogops.ListResult) error {
	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"count": r.ListCount(),
			"items": r.ListItems(),
		})
	}
	if r.ListCount() == 0 {
		output.Printfln("No %s found", r.ListNoun())
		return nil
	}
	output.Printfln("Found %d %s", r.ListCount(), r.ListNoun())
	output.PrintTable(r.ListHeaders(), r.ListRows())
	return nil
}
