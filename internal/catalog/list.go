package catalog

import "context"

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
			Default: "0",
			Help:    "Maximum number of results per page; when omitted, lists that page client-side return all rows and server-side lists use their default page size",
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

// MatchPredicate reports whether item satisfies the scan. Returning true stops
// the scan at item; returning an error aborts the scan.
type MatchPredicate[E any] func(item E) (bool, error)

// ListOptions is a paging cursor plus optional service-specific filters. The
// Start/Limit cursor is shared by every listing; F is the per-service filter
// struct (e.g. websites' domain/status/target-type filters) and is never
// touched by paging. This is the single shared options type: services alias it
// with their own filter struct instead of defining a bespoke options type.
type ListOptions[F any] struct {
	Start  int
	Limit  int
	Filter F
}

// WithPage returns a copy of o with the paging cursor set to the given start
// offset and page size, leaving the filter untouched.
func (o ListOptions[F]) WithPage(start, limit int) ListOptions[F] {
	o.Start = start
	o.Limit = limit
	return o
}

// PageLister is implemented by listing sources whose items can be filtered and
// paged by ListOptions[F]. List applies the options server-side and returns
// the resulting items.
type PageLister[E any, F any] interface {
	List(ctx context.Context, opts ListOptions[F]) ([]E, error)
}

// ScanPages iterates src's pages, calling pred on each item until it returns
// true. Each page is fetched with cur.WithPage(start, pageSize), where cur is
// base (or the zero options when base is nil) so that nil means "default". It
// stops on the first match or when a page returns fewer than the effective page
// size (end of data). pageSize <= 0 falls back to defaultPageSize. Returns the
// first matching item, whether one was found, and any error from List or pred.
func ScanPagesWithOptions[E any, F any](ctx context.Context, src PageLister[E, F], pred MatchPredicate[E], base *ListOptions[F], pageSize int) (E, bool, error) {
	var zero E
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	var cur ListOptions[F]
	if base != nil {
		cur = *base
	}
	start := 0
	for {
		items, err := src.List(ctx, cur.WithPage(start, pageSize))
		if err != nil {
			return zero, false, err
		}
		for _, item := range items {
			hit, err := pred(item)
			if err != nil {
				return zero, false, err
			}
			if hit {
				return item, true, nil
			}
		}
		if len(items) < pageSize {
			return zero, false, nil
		}
		start += len(items)
	}
}

// ScanPages is a convenience wrapper around ScanPagesWithOptions that scans
// with default paging: the zero options (nil base) and defaultPageSize.
func ScanPages[E any, F any](ctx context.Context, src PageLister[E, F], pred MatchPredicate[E]) (E, bool, error) {
	return ScanPagesWithOptions(ctx, src, pred, nil, 0)
}
