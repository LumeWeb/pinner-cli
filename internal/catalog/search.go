package catalog

// SearchArg reads the standard "search" argument from an operation input map.
// It is the single generic full-text search every server-side-searchable
// *_list operation exposes, separate from each op's structured filters.
// Returns "" when absent/empty.
func SearchArg(input map[string]any) string {
	return StrArg(input, "search", "")
}
