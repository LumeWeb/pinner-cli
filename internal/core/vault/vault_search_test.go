package vault

import (
	"bytes"
	"context"
	"testing"
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
