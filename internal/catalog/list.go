package catalog

// List is the normalized list cursor every *-list operation receives after the
// CLI/MCP surface resolves its page/page-size args. Start is the 0-based offset
// into the full result set and Limit is the page size; backends express this
// as the queryutil _start/_end pair (_end = start + limit).
type List struct {
	Start int
	Limit int
}

// defaultPageSize is the fallback page size when the caller supplies no
// --page-size. It matches the portal list endpoints' default server window so
// an un-paginated list degrades gracefully instead of hammering the backend.
const defaultPageSize = 10

// ListArgs returns the canonical paging arguments every *-list operation
// embeds so that all list surfaces (CLI flags and MCP tool args) expose the
// same human-friendly page/page-size protocol. Callers page with a 1-based
// page number and a page size; the surface wiring computes the underlying
// start/limit cursor (see ParseList) so no caller ever has to reason about raw
// offsets.
func ListArgs() []OperationArg {
	return []OperationArg{
		{
			Name:    "page",
			Type:    ArgTypeInt,
			Default: "1",
			Help:    "1-based page number to return",
		},
		{
			Name:    "page-size",
			Type:    ArgTypeInt,
			Default: "10",
			Help:    "Maximum number of results per page (default 10, max 100)",
		},
	}
}

// ParseList reads the shared page/page-size paging args from an operation input
// map and resolves them back to the server-side cursor. Page is clamped to be
// >= 1 and page-size is clamped to be >= 1, so the derived Limit is always
// positive: the backend's exclusive _end = start + limit is therefore always
// greater than _start, satisfying the queryutil pagination invariant
// (_end must be greater than _start).
func ParseList(input map[string]any) List {
	page := IntArg(input, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := IntArg(input, "page-size", defaultPageSize)
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	return List{Start: (page - 1) * pageSize, Limit: pageSize}
}
