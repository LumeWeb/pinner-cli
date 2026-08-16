package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTreeFS writes a small directory tree and returns an fs.FS rooted at it.
func buildTreeFS(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// TestCheckTreeSizeRejectsSingleOversize verifies a single file over the cap is
// rejected regardless of policy.
func TestCheckTreeSizeRejectsSingleOversize(t *testing.T) {
	dir := buildTreeFS(t, map[string]string{"big.bin": strings.Repeat("x", 64)})
	err := CheckTreeSize(os.DirFS(dir), 32, TreeSizeAggregate)
	if err == nil || !strings.Contains(err.Error(), "exceeds max_mcp_upload_size") {
		t.Fatalf("expected per-entry rejection, got %v", err)
	}
}

// TestCheckTreeSizeRejectsAggregate verifies the aggregate of several under-cap
// files is rejected once their sum exceeds the cap.
func TestCheckTreeSizeRejectsAggregate(t *testing.T) {
	dir := buildTreeFS(t, map[string]string{
		"a.txt": strings.Repeat("x", 24),
		"b.txt": strings.Repeat("x", 24), // sum = 48 > 40
	})
	err := CheckTreeSize(os.DirFS(dir), 40, TreeSizeAggregate)
	if err == nil || !strings.Contains(err.Error(), "exceeds max_mcp_upload_size") {
		t.Fatalf("expected aggregate rejection, got %v", err)
	}
}

// TestCheckTreeSizeAcceptsWithinCap verifies a tree within the cap passes.
func TestCheckTreeSizeAcceptsWithinCap(t *testing.T) {
	dir := buildTreeFS(t, map[string]string{
		"a.txt": strings.Repeat("x", 24),
		"b.txt": strings.Repeat("x", 10), // sum = 34 <= 40
	})
	if err := CheckTreeSize(os.DirFS(dir), 40, TreeSizeAggregate); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

// TestCheckTreeSizeNoCap verifies a non-positive cap is a no-op.
func TestCheckTreeSizeNoCap(t *testing.T) {
	dir := buildTreeFS(t, map[string]string{"big.bin": strings.Repeat("x", 64)})
	if err := CheckTreeSize(os.DirFS(dir), 0, TreeSizeAggregate); err != nil {
		t.Fatalf("expected no-op with zero cap, got %v", err)
	}
}

// TestDirToVaultCapsEntries verifies DirToVault rejects a tree whose aggregate
// exceeds the variadic maxBytes cap before transferring any entry.
func TestDirToVaultCapsEntries(t *testing.T) {
	dir := buildTreeFS(t, map[string]string{
		"a.txt": strings.Repeat("x", 24),
		"b.txt": strings.Repeat("x", 24), // aggregate 48 > 40
	})

	var got []string
	res, err := DirToVault(context.Background(), dir, "vault:/docs", recordingPut(t, &got), 40)
	if err == nil {
		t.Fatalf("expected DirToVault to reject aggregate oversize tree, got %d files", res.Total)
	}
	if !strings.Contains(err.Error(), "exceeds max_mcp_upload_size") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no entries written before rejection, got %d", len(got))
	}
}
