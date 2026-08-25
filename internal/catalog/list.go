package catalog

// List is the normalized server-side list cursor shared by every *-list
// operation. It follows the queryutil list protocol: Start is the 0-based
// offset into the full result set and Limit is the maximum number of rows to
// return (the page size). Backends express this as _start/_end.
type List struct {
	Start int
	Limit int
}

// ListArgs returns the canonical paging arguments every *-list operation
// embeds so that all list surfaces (CLI flags and MCP tool args) share the
// same start/limit protocol.
func ListArgs() []OperationArg {
	return []OperationArg{
		{
			Name:    "start",
			Type:    ArgTypeInt,
			Default: "0",
			Help:    "0-based offset to start the listing from",
		},
		{
			Name:    "limit",
			Type:    ArgTypeInt,
			Default: "0",
			Help:    "Maximum number of results to return (page size)",
		},
	}
}

// ParseList reads and normalizes the shared start/limit paging args from an
// operation input map. Negative values are clamped to zero so an empty cursor
// always means "no paging".
func ParseList(input map[string]any) List {
	start := IntArg(input, "start", 0)
	if start < 0 {
		start = 0
	}
	limit := IntArg(input, "limit", 0)
	if limit < 0 {
		limit = 0
	}
	return List{Start: start, Limit: limit}
}
