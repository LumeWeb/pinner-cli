package catalogops

// ListResult is the uniform contract every *-list operation result fulfills so
// that the CLI and MCP surfaces can render any list identically. Handlers build
// it with NewListResult, which carries the structured data for JSON output and
// the human table view together.
type ListResult interface {
	// ListCount is the number of items returned on this page.
	ListCount() int
	// ListItems is the slice of typed items serialized under "items" in JSON.
	ListItems() any
	// ListNoun is the human noun used for the "No X found" / "Showing X of Y"
	// lines.
	ListNoun() string
	// ListTotal is the total number of items the (unpaginated) resource holds,
	// or 0 when the backend does not report a total. When ListTotal exceeds
	// ListCount the page is truncated.
	ListTotal() int
	// ListTruncated reports whether the page is a prefix of a larger result set
	// (i.e. ListCount < ListTotal). False when no total is known.
	ListTruncated() bool
	// ListHeaders and ListRows describe the human-readable table view.
	ListHeaders() []string
	ListRows() [][]string
}

// ListResultMeta holds the presentation metadata a handler attaches to a
// paginated result: the human noun, the table headers/rows, and (optionally)
// the total result count reported by the backend.
type ListResultMeta struct {
	Noun    string
	Headers []string
	Rows    [][]string
	// Total is the overall count the resource holds (not the page length).
	// Leave 0 when the backend does not report a total; the rendered result is
	// then treated as complete and not truncated.
	Total int
}

type listResult[T any] struct {
	count int
	items []T
	total int
	meta  ListResultMeta
}

// NewListResult wraps a slice of typed list items into a ListResult. items are
// the page returned by the service; meta supplies the human noun, the
// pre-rendered table rows, and optionally the total count.
func NewListResult[T any](items []T, meta ListResultMeta) *listResult[T] {
	total := meta.Total
	if total < 0 {
		total = 0
	}
	return &listResult[T]{count: len(items), items: items, total: total, meta: meta}
}

func (r *listResult[T]) ListCount() int        { return r.count }
func (r *listResult[T]) ListItems() any        { return r.items }
func (r *listResult[T]) ListNoun() string      { return r.meta.Noun }
func (r *listResult[T]) ListTotal() int        { return r.total }
func (r *listResult[T]) ListTruncated() bool   { return r.total > 0 && r.total > r.count }
func (r *listResult[T]) ListHeaders() []string { return r.meta.Headers }
func (r *listResult[T]) ListRows() [][]string  { return r.meta.Rows }

// slicePage applies a Start/Limit window to a slice when the backend has no
// server-side offset. A zero Limit keeps all rows from Start onward.
func slicePage[T any](items []T, start, limit int) []T {
	if start > 0 {
		if start >= len(items) {
			return []T{}
		}
		items = items[start:]
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
