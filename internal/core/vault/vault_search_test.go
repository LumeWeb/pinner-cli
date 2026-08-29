package vault

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

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
	res, err := svc.Search(ctx, SearchFilter{Name: "REPORT"})
	if err != nil {
		t.Fatalf("Search name: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("name-search report found %d, want 2", len(res))
	}

	// Tag AND: finance+final matches only report-final.txt.
	res, err = svc.Search(ctx, SearchFilter{Tags: []string{"finance", "final"}})
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
	res, err = svc.Search(ctx, SearchFilter{Dir: "vault:/docs"})
	if err != nil {
		t.Fatalf("Search dir: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("dir /docs found %d, want 2", len(res))
	}

	// Cross-filter AND: tag finance+vacation -> none.
	res, err = svc.Search(ctx, SearchFilter{Tags: []string{"finance", "vacation"}})
	if err != nil {
		t.Fatalf("Search combined: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("combined AND should match nothing, got %+v", res)
	}

	// Empty filter returns all live files.
	res, err = svc.Search(ctx, SearchFilter{})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("empty search found %d, want 3", len(res))
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
		res, err := svc.Search(ctx, SearchFilter{Name: n})
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
	res, err := svc.Search(ctx, SearchFilter{Source: "mcp"})
	if err != nil {
		t.Fatalf("Search source: %v", err)
	}
	if len(res) != 1 || res[0].Name != "mcp-doc.txt" {
		t.Fatalf("source=mcp found %+v, want only mcp-doc.txt", res)
	}

	// Filter by host.
	res, err = svc.Search(ctx, SearchFilter{Host: "claude-desktop", Source: "mcp"})
	if err != nil {
		t.Fatalf("Search host: %v", err)
	}
	if len(res) != 1 || res[0].Agent != "orchestrator-a" {
		t.Fatalf("host+source found %+v, want only mcp-doc.txt", res)
	}

	// Filter by agent.
	res, err = svc.Search(ctx, SearchFilter{Agent: "orchestrator-a"})
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

	res, err := svc.Search(ctx, SearchFilter{Status: "lost"})
	if err != nil {
		t.Fatalf("Search status: %v", err)
	}
	if len(res) != 1 || res[0].Status != "lost" {
		t.Fatalf("status=lost search = %+v, want 1 lost file", res)
	}
	res, err = svc.Search(ctx, SearchFilter{Status: "ok"})
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
	res, err := svc.Search(ctx, SearchFilter{Name: "tag:finance"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "tag:finance.txt" {
		t.Fatalf("query=tag:finance matched %+v, want only literal filename", res)
	}

	// query contains spaces: treated as one contiguous substring, not AND.
	mk("vault:/docs/q4 invoice.txt", nil)
	res, err = svc.Search(ctx, SearchFilter{Name: "q4 invoice"})
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
	res, err := svc.Search(ctx, SearchFilter{Name: "rep"})
	if err != nil {
		t.Fatalf("Search 3-char: %v", err)
	}
	if len(res) != 1 || res[0].Name != "report.txt" {
		t.Fatalf("3-char query matched %+v, want report.txt", res)
	}

	// 2 chars -> forced to LIKE path.
	res, err = svc.Search(ctx, SearchFilter{Name: "ab"})
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
	res, err := svc.Search(ctx, SearchFilter{Tags: []string{"finance"}})
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

	res, err := svc.Search(ctx, SearchFilter{Name: "report", Tags: []string{"finance"}, Host: "claude-desktop"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "report.txt" {
		t.Fatalf("name+tag+host search = %+v, want only report.txt", res)
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
		res, err := svc.Search(ctx, SearchFilter{Name: n})
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

	wantSearch := func(f SearchFilter) []string {
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
	base := wantSearch(SearchFilter{Name: "report"})
	if len(base) != 2 {
		t.Fatalf("baseline FTS search matched %v, want 2", base)
	}

	// Force a MATCH error: mark availability as true (cached) but drop the
	// index so the MATCH query fails, which must fall back to LIKE.
	svc.ftsChecked, svc.ftsOK = true, true
	if err := svc.db.Exec("DROP TABLE files_fts").Error; err != nil {
		t.Fatalf("drop files_fts: %v", err)
	}
	after := wantSearch(SearchFilter{Name: "report"})
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

	res, err := svc.Search(ctx, SearchFilter{})
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
