package catalogops

// ListResult is the uniform contract every *-list operation result fulfills so
// that the CLI and MCP surfaces can render any list identically. Handlers build
// it with NewListResult, which carries the structured data for JSON output and
// the human table view together.
type ListResult interface {
	// ListCount is the number of items returned (the page length).
	ListCount() int
	// ListItems is the slice of typed items serialized under "items" in JSON.
	ListItems() any
	// ListNoun is the human noun used for the "No X found" / "Found N X" line.
	ListNoun() string
	// ListHeaders and ListRows describe the human-readable table view.
	ListHeaders() []string
	ListRows() [][]string
}

// ListResultMeta holds the presentation metadata a handler attaches to a
// paginated result: the human noun and the table headers/rows.
type ListResultMeta struct {
	Noun    string
	Headers []string
	Rows    [][]string
}

type listResult[T any] struct {
	count int
	items []T
	meta  ListResultMeta
}

// NewListResult wraps a slice of typed list items into a ListResult. items are
// the page returned by the service; meta supplies the human noun and the
// pre-rendered table rows.
func NewListResult[T any](items []T, meta ListResultMeta) *listResult[T] {
	return &listResult[T]{count: len(items), items: items, meta: meta}
}

func (r *listResult[T]) ListCount() int        { return r.count }
func (r *listResult[T]) ListItems() any        { return r.items }
func (r *listResult[T]) ListNoun() string      { return r.meta.Noun }
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
