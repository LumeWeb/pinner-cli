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
// start/limit cursor (see ParseList/ParseListPage) so no caller ever has to
// reason about raw offsets.
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
			Help:    "Maximum number of results per page (max 100)",
		},
	}
}

// ParseList reads the shared page/page-size paging args from an operation input
// map and resolves them back to a cursor. Page is clamped to >= 1. A zero or
// absent page-size yields Limit 0, which keeps all rows: this is the form used
// by lists that fetch the full result set and slice client-side (slicePage), so
// they preserve their historical "show everything" default and never silently
// truncate.
func ParseList(input map[string]any) List {
	return parseList(input, 0)
}

// ParseListPage is like ParseList but coerces a zero or absent page-size to the
// given positive default, so the derived Limit is always > 0. Serve-side paged
// lists (websites, pins, operations) use this form: a positive Limit keeps the
// backend's exclusive _end = start + limit strictly greater than _start,
// satisfying the queryutil invariant "_end must be greater than _start", and a
// bare `--page N` advances by defaultPageSize rows.
func ParseListPage(input map[string]any, defaultPageSize int) List {
	return parseList(input, defaultPageSize)
}

// parseList is the shared page/page-size → start/limit resolution. A positive
// defaultPageSize substitutes for any zero/absent page-size; otherwise a
// zero/absent page-size means "no paging" (Limit 0).
func parseList(input map[string]any, defaultPageSize int) List {
	page := IntArg(input, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := IntArg(input, "page-size", 0)
	if pageSize < 0 {
		pageSize = 0
	}
	if defaultPageSize > 0 && pageSize < 1 {
		pageSize = defaultPageSize
	}
	return List{Start: (page - 1) * pageSize, Limit: pageSize}
}
