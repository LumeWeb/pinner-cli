package mcp

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// vaultFileResult describes one file written into the vault by DirToVault.
type vaultFileResult struct {
	RelPath string `json:"relpath"`             // path relative to the source dir root
	Vault   string `json:"vault_path"`          // destination vault path
	Object  any    `json:"object_id,omitempty"` // whatever the Put func returns
	Size    int64  `json:"size"`
}

// DirToVaultPutResult aggregates the per-file writes for one directory.
type DirToVaultPutResult struct {
	Base  string            `json:"base"` // vault destination root
	Files []vaultFileResult `json:"files"`
	Total int               `json:"total"`
	Bytes int64             `json:"bytes"`
}

// VaultPutFunc is the injectable per-file write used by DirToVault. It mirrors
// the vault.VaultService.Put shape so the real implementation wires straight
// in: put(ctx, reader, size, vaultPath) -> objectID.
//
// It is exported so the CLI layer (internal/cli) can drive DirToVault with a
// real vault service-backed implementation surfaced through the MCP adapter.
type VaultPutFunc func(ctx context.Context, reader io.Reader, size int64, vaultPath string) (any, error)

// DirToVault walks the local directory rooted at dir and writes one vault
// object per file, mapping each to dstBase/<relpath> (vault path grammar).
// Directories are skipped (vault has no empty-dir objects); only regular
// files are written. put is called once per file with an opened reader.
func DirToVault(ctx context.Context, dir string, dstBase string, put VaultPutFunc) (*DirToVaultPutResult, error) {
	res := &DirToVaultPutResult{Base: strings.TrimRight(dstBase, "/")}
	base := res.Base

	err := fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil // the walk root itself; nothing to write
		}
		if d.IsDir() {
			return nil // skip directories
		}
		// Open and stat the file via its full host path.
		full := filepath.Join(dir, p)
		fi, err := os.Stat(full)
		if err != nil {
			return err
		}
		f, err := os.Open(full)
		if err != nil {
			return err
		}
		defer f.Close()
		// fs.WalkDir paths already use "/" separators, which matches the vault
		// path grammar, so the vault path is base + "/" + relpath.
		vaultPath := base + "/" + p
		obj, perr := put(ctx, f, fi.Size(), vaultPath)
		if perr != nil {
			return perr
		}
		res.Files = append(res.Files, vaultFileResult{RelPath: p, Vault: vaultPath, Object: obj, Size: fi.Size()})
		res.Total++
		res.Bytes += fi.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
