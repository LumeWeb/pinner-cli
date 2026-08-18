package ieo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// TreeSizePolicy controls how CheckTreeSize enforces the size cap.
type TreeSizePolicy int

const (
	// TreeSizePerEntry only rejects a single regular file exceeding maxBytes.
	TreeSizePerEntry TreeSizePolicy = iota
	// TreeSizeAggregate rejects when the sum of all regular-file sizes exceeds
	// maxBytes, matching the total-transfer cap applied to the relay, data-URI,
	// and curl upload surfaces (which cap the whole body, not individual parts).
	TreeSizeAggregate
)

// CheckTreeSize walks fsys once and reports whether its content is within the
// operator-set size cap. It is a deterministic pre-flight check: the local-path
// upload handlers call it BEFORE transferring anything, so an oversized tree —
// whether a single file over the cap or the aggregate sum of all files over the
// cap — is rejected up front rather than after partial data has been uploaded.
//
// This mirrors the relay/DataURI/curl surfaces, which cap total transfer size.
// fsys is walked with fs.WalkDir; only regular files count toward the total.
func CheckTreeSize(fsys fs.FS, maxBytes int64, policy TreeSizePolicy) error {
	if maxBytes <= 0 {
		return nil
	}
	var total int64
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if fi.Size() > maxBytes {
			return fmt.Errorf("file %s (%d bytes) exceeds max_mcp_upload_size (%d)", p, fi.Size(), maxBytes)
		}
		if policy == TreeSizeAggregate {
			total += fi.Size()
			if total > maxBytes {
				return fmt.Errorf("total size %d bytes exceeds max_mcp_upload_size (%d)", total, maxBytes)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// CheckDirectorySize enforces the same size cap as CheckTreeSize but for a
// host-side directory, walking it with filepath.WalkDir and os.Stat so symlinks
// are FOLLOWED. This makes the pre-flight size match the bytes the transfer
// will actually read: os.DirFS-based uploads follow symlinks when opening
// files, whereas an fs.WalkDir over os.DirFS lstats entries (fs.DirEntry.Info)
// and would skip symlink targets entirely — letting a symlink pointing at a
// file larger than maxBytes bypass the cap while its target bytes still get
// uploaded. Only regular files (after following symlinks) count toward the
// total, mirroring CheckTreeSize's semantics.
func CheckDirectorySize(dir string, maxBytes int64, policy TreeSizePolicy) error {
	if maxBytes <= 0 {
		return nil
	}
	var total int64
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// os.Stat follows symlinks: the size we check here is the size the
		// upload will actually transfer through the DirFS.
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if fi.Size() > maxBytes {
			return fmt.Errorf("file %s (%d bytes) exceeds max_mcp_upload_size (%d)", p, fi.Size(), maxBytes)
		}
		if policy == TreeSizeAggregate {
			total += fi.Size()
			if total > maxBytes {
				return fmt.Errorf("total size %d bytes exceeds max_mcp_upload_size (%d)", total, maxBytes)
			}
		}
		return nil
	})
}
