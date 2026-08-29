package vault

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

// searchReq builds a SearchRequest from an ANDed predicate list, so tests can
// express filters concisely.
func searchReq(preds ...Predicate) SearchRequest {
	return SearchRequest{Where: preds}
}

// TestSearchFilters verifies metadata-first search: name substring, tag AND
// membership, and directory prefix filtering, with AND semantics across
// filters.
func TestSearchFilters(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string, meta map[string]any) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, meta); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	// Three files with tags.
	mk("vault:/docs/report-q3.txt", map[string]any{"tags": []any{"finance", "draft"}})
	mk("vault:/docs/report-final.txt", map[string]any{"tags": []any{"finance", "final"}})
	mk("vault:/photos/beach.jpg", map[string]any{"tags": []any{"vacation"}})

	// Name substring (case-insensitive).
	res, err := svc.Search(ctx, SearchRequest{Query: "REPORT"})
	if err != nil {
		t.Fatalf("Search name: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("name-search report found %d, want 2", len(res))
	}

	// Tag AND: finance+final matches only report-final.txt (two separate
	// single-tag predicates that AND together).
	res, err = svc.Search(ctx, searchReq(Predicate{Tag: []string{"finance"}}, Predicate{Tag: []string{"final"}}))
	if err != nil {
		t.Fatalf("Search tags: %v", err)
	}
	if len(res) != 1 || res[0].Name != "report-final.txt" {
		t.Fatalf("tag-AND search = %+v, want only report-final.txt", res)
	}
	if len(res[0].Tags) == 0 {
		t.Fatal("search result should surface tags")
	}

	// Directory prefix: only /docs files.
	res, err = svc.Search(ctx, searchReq(Predicate{Dir: []string{"vault:/docs"}}))
	if err != nil {
		t.Fatalf("Search dir: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("dir /docs found %d, want 2", len(res))
	}

	// Cross-filter AND: tag finance+vacation -> none.
	res, err = svc.Search(ctx, searchReq(Predicate{Tag: []string{"finance"}}, Predicate{Tag: []string{"vacation"}}))
	if err != nil {
		t.Fatalf("Search combined: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("combined AND should match nothing, got %+v", res)
	}

	// Empty filter returns all live files.
	res, err = svc.Search(ctx, SearchRequest{})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("empty search found %d, want 3", len(res))
	}
}

// TestSearchTagOrWithinField verifies a single tag predicate with a LIST means
// "any of these tags" (OR/IN), distinct from two scalar predicates (AND).
func TestSearchTagOrWithinField(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string, meta map[string]any) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, meta); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	mk("vault:/docs/finance.txt", map[string]any{"tags": []any{"finance"}})
	mk("vault:/docs/tax.txt", map[string]any{"tags": []any{"tax"}})
	mk("vault:/docs/other.txt", map[string]any{"tags": []any{"q1"}})

	// One predicate with a list -> either finance OR tax.
	res, err := svc.Search(ctx, searchReq(Predicate{Tag: []string{"finance", "tax"}}))
	if err != nil {
		t.Fatalf("Search tag-or: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("tag-or [finance,tax] found %d, want 2 (finance.txt, tax.txt)", len(res))
	}

	// Combined with an AND tag: (finance OR tax) AND q1 -> none (other.txt has
	// q1 but not finance/tax).
	res, err = svc.Search(ctx, searchReq(Predicate{Tag: []string{"finance", "tax"}}, Predicate{Tag: []string{"q1"}}))
	if err != nil {
		t.Fatalf("Search tag-or-and: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("(finance|tax) AND q1 should match nothing, got %+v", res)
	}
}

// TestSearchNotStatus verifies negation of a column predicate.
func TestSearchNotStatus(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1, "vault:/docs/a.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("b")), 1, "vault:/docs/b.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Mark a.txt lost.
	rec := File{}
	if err := svc.db.Where("name = ?", "a.txt").First(&rec).Error; err != nil {
		t.Fatalf("load a.txt: %v", err)
	}
	if err := svc.db.Model(&File{}).Where("id = ?", rec.ID).Update("status", "lost").Error; err != nil {
		t.Fatalf("mark lost: %v", err)
	}

	res, err := svc.Search(ctx, searchReq(Predicate{Not: &Predicate{Status: []string{"lost"}}}))
	if err != nil {
		t.Fatalf("Search not-status: %v", err)
	}
	if len(res) != 1 || res[0].Name != "b.txt" {
		t.Fatalf("not-status=lost search = %+v, want only b.txt", res)
	}
}

// TestSearchWriteContext verifies the normalized write-context columns
// (source/host/agent) are populated from the stamped metadata and filterable.
func TestSearchWriteContext(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string, meta map[string]any) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, meta); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	mk("vault:/docs/mcp-doc.txt", StampedMetadata("mcp", "claude-desktop", "home", map[string]any{"agent": "orchestrator-a"}))
	mk("vault:/docs/cli-doc.txt", StampedMetadata("cli", "", "home", nil))
	mk("vault:/docs/other.txt", nil)

	// Projects the columns from stamped metadata on Put.
	byName := func(n string) *SearchItem {
		res, err := svc.Search(ctx, SearchRequest{Query: n})
		if err != nil {
			t.Fatalf("search %s: %v", n, err)
		}
		if len(res) != 1 {
			t.Fatalf("search %s found %d, want 1", n, len(res))
		}
		return &res[0]
	}
	mcp := byName("mcp-doc.txt")
	if mcp.Source != "mcp" || mcp.Host != "claude-desktop" || mcp.Agent != "orchestrator-a" {
		t.Fatalf("mcp-doc write-context = %+v", mcp)
	}
	cli := byName("cli-doc.txt")
	if cli.Source != "cli" || cli.Host != "" || cli.Agent != "" {
		t.Fatalf("cli-doc write-context = %+v", cli)
	}
	other := byName("other.txt")
	if other.Source != "" || other.Host != "" || other.Agent != "" {
		t.Fatalf("other write-context = %+v", other)
	}

	// Filter by source.
	res, err := svc.Search(ctx, searchReq(Predicate{Source: []string{"mcp"}}))
	if err != nil {
		t.Fatalf("Search source: %v", err)
	}
	if len(res) != 1 || res[0].Name != "mcp-doc.txt" {
		t.Fatalf("source=mcp found %+v, want only mcp-doc.txt", res)
	}

	// Filter by host.
	res, err = svc.Search(ctx, searchReq(Predicate{Host: []string{"claude-desktop"}}, Predicate{Source: []string{"mcp"}}))
	if err != nil {
		t.Fatalf("Search host: %v", err)
	}
	if len(res) != 1 || res[0].Agent != "orchestrator-a" {
		t.Fatalf("host+source found %+v, want only mcp-doc.txt", res)
	}

	// Filter by agent.
	res, err = svc.Search(ctx, searchReq(Predicate{Agent: []string{"orchestrator-a"}}))
	if err != nil {
		t.Fatalf("Search agent: %v", err)
	}
	if len(res) != 1 || res[0].Source != "mcp" {
		t.Fatalf("agent found %+v, want only mcp-doc.txt", res)
	}

	// Stat surfaces the columns too.
	st, err := svc.Stat(ctx, "vault:/docs/mcp-doc.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Source != "mcp" || st.Host != "claude-desktop" || st.Agent != "orchestrator-a" {
		t.Fatalf("Stat write-context = %+v", st)
	}
}

// TestSearchStatusFilter verifies filtering by the lost/pending status field.
func TestSearchStatusFilter(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1, "vault:/docs/a.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("b")), 1, "vault:/docs/b.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Mark one file lost directly in the DB.
	rec := File{}
	if err := svc.db.Where("name = ?", "a.txt").First(&rec).Error; err != nil {
		t.Fatalf("load a.txt: %v", err)
	}
	if err := svc.db.Model(&File{}).Where("id = ?", rec.ID).Update("status", "lost").Error; err != nil {
		t.Fatalf("mark lost: %v", err)
	}

	res, err := svc.Search(ctx, searchReq(Predicate{Status: []string{"lost"}}))
	if err != nil {
		t.Fatalf("Search status: %v", err)
	}
	if len(res) != 1 || res[0].Status != "lost" {
		t.Fatalf("status=lost search = %+v, want 1 lost file", res)
	}
	res, err = svc.Search(ctx, searchReq(Predicate{Status: []string{"ok"}}))
	if err != nil {
		t.Fatalf("Search status ok: %v", err)
	}
	if len(res) != 1 || res[0].Status != "ok" {
		t.Fatalf("status=ok search = %+v, want 1 ok file", res)
	}
}

// TestSearchQueryOpaque verifies `query` is a literal filename substring, never
// parsed as a query language (no field:, AND/OR/NOT, or negation semantics).
func TestSearchQueryOpaque(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string, meta map[string]any) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, meta); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	// A file whose literal name contains "tag:finance" and one that is merely
	// tagged finance.
	mk("vault:/docs/tag:finance.txt", nil)
	mk("vault:/docs/budget.txt", map[string]any{"tags": []any{"finance"}})

	// query="tag:finance" must match the literal filename, not apply a tag
	// filter (budget.txt has the finance tag but not the literal text).
	res, err := svc.Search(ctx, SearchRequest{Query: "tag:finance"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "tag:finance.txt" {
		t.Fatalf("query=tag:finance matched %+v, want only literal filename", res)
	}

	// query contains spaces: treated as one contiguous substring, not AND.
	mk("vault:/docs/q4 invoice.txt", nil)
	res, err = svc.Search(ctx, SearchRequest{Query: "q4 invoice"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "q4 invoice.txt" {
		t.Fatalf("query='q4 invoice' matched %+v, want one", res)
	}
}

// TestSearchQueryMinLength verifies 3-char (FTS-usable) and 2-char (LIKE-only)
// queries both still return substring matches.
func TestSearchQueryMinLength(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, nil); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	mk("vault:/docs/report.txt")
	mk("vault:/docs/abacus.txt")

	// 3 chars -> FTS or LIKE both acceptable; must match.
	res, err := svc.Search(ctx, SearchRequest{Query: "rep"})
	if err != nil {
		t.Fatalf("Search 3-char: %v", err)
	}
	if len(res) != 1 || res[0].Name != "report.txt" {
		t.Fatalf("3-char query matched %+v, want report.txt", res)
	}

	// 2 chars -> forced to LIKE path.
	res, err = svc.Search(ctx, SearchRequest{Query: "ab"})
	if err != nil {
		t.Fatalf("Search 2-char: %v", err)
	}
	if len(res) != 1 || res[0].Name != "abacus.txt" {
		t.Fatalf("2-char query matched %+v, want abacus.txt", res)
	}
}

// TestSearchNamePredicateUndefinedWhenEmpty ensures empty query + filters does
// not add a name predicate (metadata-only search).
func TestSearchNamePredicateUndefinedWhenEmpty(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	mk := func(path string, meta map[string]any) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, meta); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	mk("vault:/docs/a.txt", map[string]any{"tags": []any{"finance"}})
	mk("vault:/docs/b.txt", map[string]any{"tags": []any{"ops"}})

	// query="" only honors the tag filter.
	res, err := svc.Search(ctx, searchReq(Predicate{Tag: []string{"finance"}}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "a.txt" {
		t.Fatalf("empty-query tag search = %+v, want only a.txt", res)
	}
}

// TestSearchNameAndStructuredFilters verifies query ANDs with structured
// filters.
func TestSearchNameAndStructuredFilters(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1,
		"vault:/docs/report.txt", StampedMetadata("mcp", "claude-desktop", "home", map[string]any{"tags": []any{"finance"}})); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("b")), 1,
		"vault:/docs/other.txt", StampedMetadata("mcp", "claude-desktop", "home", nil)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	res, err := svc.Search(ctx, SearchRequest{
		Query: "report",
		Where: []Predicate{{Tag: []string{"finance"}}, {Host: []string{"claude-desktop"}}},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "report.txt" {
		t.Fatalf("name+tag+host search = %+v, want only report.txt", res)
	}
}

// TestSearchSinceBefore verifies the creation-time bounds compile.
func TestSearchSinceBefore(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1, "vault:/docs/a.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// All files exist "now"; a since in the past matches both, a since far in
	// the future matches none.
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour * 24).UTC().Format(time.RFC3339)

	res, err := svc.Search(ctx, searchReq(Predicate{Since: past}))
	if err != nil {
		t.Fatalf("Search since: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("since(past) found %d, want 1", len(res))
	}
	res, err = svc.Search(ctx, searchReq(Predicate{Since: future}))
	if err != nil {
		t.Fatalf("Search since-future: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("since(future) found %d, want 0", len(res))
	}
}

// TestParseWhere validates the parsing of the structured where JSON (the MCP
// surface and the CLI --where escape hatch), including the one-field-per-object
// rule and closed field names.
func TestParseWhere(t *testing.T) {
	// MCP-shaped input: a decoded []any of objects.
	got, err := ParseWhere([]any{
		map[string]any{"tag": []any{"finance", "tax"}},
		map[string]any{"host": "claude-desktop"},
		map[string]any{"not": map[string]any{"status": "lost"}},
	})
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ParseWhere len = %d, want 3", len(got))
	}
	if len(got[0].Tag) != 2 || got[0].Tag[0] != "finance" {
		t.Fatalf("tag list predicate = %+v", got[0])
	}
	if got[1].Host[0] != "claude-desktop" {
		t.Fatalf("host predicate = %+v", got[1])
	}
	if got[2].Not == nil || got[2].Not.Status[0] != "lost" {
		t.Fatalf("not predicate = %+v", got[2])
	}

	// CLI-shaped input: a JSON string.
	s := `[{"tag":["finance","tax"]},{"host":"claude-desktop"}]`
	got, err = ParseWhere(s)
	if err != nil {
		t.Fatalf("ParseWhere(string): %v", err)
	}
	if len(got) != 2 || len(got[0].Tag) != 2 {
		t.Fatalf("ParseWhere(string) = %+v", got)
	}

	// Scalar normalization: {tag: "a"} becomes a one-element predicate.
	got, err = ParseWhere([]any{map[string]any{"tag": "a"}})
	if err != nil {
		t.Fatalf("ParseWhere scalar: %v", err)
	}
	if len(got[0].Tag) != 1 || got[0].Tag[0] != "a" {
		t.Fatalf("scalar tag = %+v", got[0])
	}

	// Unknown field is an error.
	if _, err := ParseWhere([]any{map[string]any{"bogus": "x"}}); err == nil {
		t.Fatal("expected error for unknown field")
	}

	// Multiple field keys in one object is an error.
	if _, err := ParseWhere([]any{map[string]any{"tag": "a", "host": "b"}}); err == nil {
		t.Fatal("expected error for multi-field predicate")
	}

	// Empty list is an error.
	if _, err := ParseWhere([]any{map[string]any{"tag": []any{}}}); err == nil {
		t.Fatal("expected error for empty tag list")
	}

	// Empty where is allowed -> nil.
	got, err = ParseWhere(nil)
	if err != nil || got != nil {
		t.Fatalf("ParseWhere(nil) = %v, %v", got, err)
	}
}

// TestParseWhereNormalizesCase verifies status/source predicate values are
// lowercased at parse time (stored values are canonical lowercase), so a
// mixed-case where payload still matches stored rows.
func TestParseWhereNormalizesCase(t *testing.T) {
	got, err := ParseWhere([]any{
		map[string]any{"status": "LOST"},
		map[string]any{"source": "MCP"},
		map[string]any{"not": map[string]any{"source": "CLI"}},
		map[string]any{"tag": "FiNaNcE"},
		map[string]any{"host": "Codex"},
	})
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("ParseWhere len = %d, want 5", len(got))
	}
	if got[0].Status[0] != "lost" {
		t.Fatalf("status should be lowercased, got %v", got[0].Status)
	}
	if got[1].Source[0] != "mcp" {
		t.Fatalf("source should be lowercased, got %v", got[1].Source)
	}
	if got[2].Not == nil || got[2].Not.Source[0] != "cli" {
		t.Fatalf("not-wrapped source should be lowercased, got %+v", got[2])
	}
	// Tags, hosts, and agents are NOT normalized (free-form values).
	if got[3].Tag[0] != "FiNaNcE" {
		t.Fatalf("tag should not be lowercased, got %v", got[3].Tag)
	}
	if got[4].Host[0] != "Codex" {
		t.Fatalf("host should not be lowercased, got %v", got[4].Host)
	}
}

// TestSearchFTSSyncLifecycle verifies files_fts follows the files table: new
// rows are indexed, renames move the index entry, and soft-deleted rows are no
// longer searchable.
func TestSearchFTSSyncLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)
	if !svc.ftsAvailable() {
		t.Skip("FTS5 trigram not available; lifecycle index is not exercised")
	}

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1, "vault:/docs/alpha.txt", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	byName := func(n string) []string {
		t.Helper()
		res, err := svc.Search(ctx, SearchRequest{Query: n})
		if err != nil {
			t.Fatalf("Search %q: %v", n, err)
		}
		names := make([]string, 0, len(res))
		for _, r := range res {
			names = append(names, r.Name)
		}
		return names
	}
	if got := byName("alpha"); len(got) != 1 || got[0] != "alpha.txt" {
		t.Fatalf("indexed after insert: %v", got)
	}

	// Rename via DB: FTS entry should follow to the new name.
	rec := File{}
	if err := svc.db.Where("name = ?", "alpha.txt").First(&rec).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := svc.db.Model(&File{}).Where("id = ?", rec.ID).Update("name", "beta.txt").Error; err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := byName("beta"); len(got) != 1 || got[0] != "beta.txt" {
		t.Fatalf("renamed not searchable by new name: %v", got)
	}
	if got := byName("alpha"); len(got) != 0 {
		t.Fatalf("old name still searchable after rename: %v", got)
	}

	// Soft-delete: row must drop out of search.
	if err := svc.db.Model(&File{}).Where("id = ?", rec.ID).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	if got := byName("beta"); len(got) != 0 {
		t.Fatalf("soft-deleted still searchable: %v", got)
	}
}

// TestSearchFTSUnavailableFallsBackToLike verifies that when the FTS5 index
// cannot be used (simulated as FTS "available" but the table dropped, forcing a
// MATCH error), search falls back to the original LIKE behavior and returns the
// same substring matches.
func TestSearchFTSUnavailableFallsBackToLike(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	if !svc.ftsAvailable() {
		t.Skip("FTS5 trigram not available; fallback path not exercised")
	}

	mk := func(path string) {
		t.Helper()
		if _, err := svc.Put(ctx, bytes.NewReader([]byte(path)), int64(len(path)), path, nil); err != nil {
			t.Fatalf("Put %s: %v", path, err)
		}
	}
	mk("vault:/docs/report-final.txt")
	mk("vault:/docs/report-draft.txt")
	mk("vault:/docs/budget.txt")

	wantSearch := func(f SearchRequest) []string {
		res, err := svc.Search(ctx, f)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		names := make([]string, 0, len(res))
		for _, r := range res {
			names = append(names, r.Name)
		}
		return names
	}

	// Baseline via the FTS path.
	base := wantSearch(SearchRequest{Query: "report"})
	if len(base) != 2 {
		t.Fatalf("baseline FTS search matched %v, want 2", base)
	}

	// Force a MATCH error: mark availability as true (cached) but drop the
	// index so the MATCH query fails, which must fall back to LIKE.
	svc.ftsChecked, svc.ftsOK = true, true
	if err := svc.db.Exec("DROP TABLE files_fts").Error; err != nil {
		t.Fatalf("drop files_fts: %v", err)
	}
	after := wantSearch(SearchRequest{Query: "report"})
	if len(after) != 2 {
		t.Fatalf("LIKE fallback matched %v, want 2", after)
	}
}

// TestSearchCap500 verifies results are capped at 500.
func TestSearchCap500(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	// Bulk-insert 501 distinct live files directly (distinct names satisfy the
	// live unique (name, dir) partial index).
	const total = 501
	rows := make([]File, 0, total)
	now := time.Now()
	for i := 0; i < total; i++ {
		rows = append(rows, File{
			UUID:          fmt.Sprintf("uuid-%05d", i),
			VersionID:     fmt.Sprintf("v-%05d", i),
			Seq:           uint(i),
			Name:          fmt.Sprintf("file-%05d.txt", i),
			ObjectKey:     fmt.Sprintf("obj-%05d", i),
			ContentDigest: fmt.Sprintf("digest-%05d", i),
			IsCurrent:     true,
			Status:        "ok",
			CreatedAt:     now.Add(time.Duration(i) * time.Second),
			UpdatedAt:     now.Add(time.Duration(i) * time.Second),
		})
	}
	if err := svc.db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("bulk insert: %v", err)
	}

	res, err := svc.Search(ctx, SearchRequest{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 500 {
		t.Fatalf("cap search returned %d, want 500", len(res))
	}
	// Newest-first (the inserted file-00500 has the latest created_at).
	if res[0].Name != "file-00500.txt" {
		t.Fatalf("first result = %s, want newest file-00500.txt", res[0].Name)
	}
}
