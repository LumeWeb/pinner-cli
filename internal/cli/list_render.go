package cli

import "go.lumeweb.com/pinner-cli/internal/catalogops"

// renderListResult renders any *-list operation result (a catalogops.ListResult)
// through the CLI Output formatter. It is the single rendering home for list
// commands: JSON emits a uniform {count, items} shape (plus total/truncated
// when the backend reports a total); human mode prints a "No X found" line, or
// "Found N X" when the page is complete, or "Showing N of M X" when truncated.
func renderListResult(output Output, r catalogops.ListResult) error {
	if r == nil {
		return nil
	}
	if output.IsJSON() {
		out := map[string]any{
			"count": r.ListCount(),
			"items": r.ListItems(),
		}
		if r.ListTotal() > 0 {
			out["total"] = r.ListTotal()
			out["truncated"] = r.ListTruncated()
		}
		return output.PrintJSON(out)
	}
	if r.ListCount() == 0 {
		// An empty page of a truncated result set (e.g. Start past the last
		// row) still has items behind it — report the total rather than a stale
		// "no results". Only fall back to "No X found" when no total is known.
		if r.ListTotal() > 0 {
			output.Printfln("Showing 0 of %d %s", r.ListTotal(), r.ListNoun())
		} else {
			output.Printfln("No %s found", r.ListNoun())
		}
		return nil
	}
	if r.ListTruncated() {
		output.Printfln("Showing %d of %d %s", r.ListCount(), r.ListTotal(), r.ListNoun())
	} else {
		output.Printfln("Found %d %s", r.ListCount(), r.ListNoun())
	}
	output.PrintTable(r.ListHeaders(), r.ListRows())
	return nil
}
