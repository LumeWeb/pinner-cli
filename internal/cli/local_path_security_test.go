package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipWith creates an in-memory zip with the given entries (name -> contents).
func zipWith(t *testing.T, entries map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, contents := range entries {
		if strings.HasSuffix(name, "/") {
			if _, err := zw.Create(name); err != nil {
				t.Fatalf("create dir entry %q: %v", name, err)
			}
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("write entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return &buf
}

// TestMaterializeArchiveRejectsZipSlip verifies that archive entries whose
// paths escape the destination directory ("..", absolute) are rejected rather
// than written outside dstDir (a Zip Slip / path traversal guard).
func TestMaterializeArchiveRejectsZipSlip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "hostile.zip")
	if err := os.WriteFile(src, zipWith(t, map[string]string{
		"ok.txt":      "fine",
		"../evil.txt": "escaped",
	}).Bytes(), 0o644); err != nil {
		t.Fatalf("write hostile zip: %v", err)
	}

	dst := t.TempDir()
	err := materializeArchive(context.Background(), src, dst)
	if err == nil {
		t.Fatal("expected materializeArchive to reject a path-escaping archive entry")
	}
	if !strings.Contains(err.Error(), "escapes destination directory") {
		t.Fatalf("unexpected error: %v", err)
	}

	// No file may have been written outside dstDir.
	for _, parent := range []string{filepath.Dir(dst), filepath.Join(filepath.Dir(dst), filepath.Clean(".."))} {
		if _, err := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(err) {
			t.Fatalf("archive escaped dstDir (%s): %v", parent, err)
		}
	}
}

// TestCheckArchiveTreeSizeRejectsOverCap verifies that an archive whose
// extracted contents exceed max_mcp_upload_size is rejected by
// checkArchiveTreeSize BEFORE it is materialized (so a decompression bomb or
// oversized archive cannot be fully extracted into a temp dir / memory first),
// while a fitting archive passes.
func TestCheckArchiveTreeSizeRejectsOverCap(t *testing.T) {
	src := filepath.Join(t.TempDir(), "sized.zip")
	if err := os.WriteFile(src, zipWith(t, map[string]string{
		"a.txt": strings.Repeat("a", 512),
		"b.txt": strings.Repeat("b", 512),
	}).Bytes(), 0o644); err != nil {
		t.Fatalf("write sized zip: %v", err)
	}

	// Fits (sum of regular files == 1024 <= cap).
	if err := checkArchiveTreeSize(context.Background(), src, 1024); err != nil {
		t.Fatalf("expected in-cap archive to pass, got: %v", err)
	}

	// Over the aggregate cap (sum of regular files == 1024 > 512) -> rejected.
	err := checkArchiveTreeSize(context.Background(), src, 512)
	if err == nil {
		t.Fatal("expected checkArchiveTreeSize to reject an over-cap archive")
	}
	// And above the per-entry cap too (single 512-byte entry > 256).
	if err := checkArchiveTreeSize(context.Background(), src, 256); err == nil {
		t.Fatal("expected checkArchiveTreeSize to reject an over-cap archive")
	}
}
