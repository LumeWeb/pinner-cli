package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// IPFSDownloadHandler streams a single IPFS node (CID or CID/path) to dest.
// It is the authenticated IPFS download executor, homed in the CLI layer where
// the download/IPFS service lives (the same split as UploadHandler). The tool
// never decides the mechanism — it hands the resolver a sink and this handler
// provides the source bytes for the co-located or filedrop branch.
type IPFSDownloadHandler func(ctx context.Context, ipfsPath string, w io.Writer) error

// VaultGetHandler streams a single encrypted vault file (vault:/...) to dest.
// It is the authenticated vault-read executor, homed in the CLI layer where
// the vault service lives (mirror of VaultPutHandler).
type VaultGetHandler func(ctx context.Context, vaultPath string, w io.Writer) error

// DownloadSinkInput is the shared argument shape for how a download's bytes are
// delivered. A single `sink` union plus the local output path / filedrop TTL
// mirrors how upload_file carries its transport-scoped source.
type DownloadSinkInput struct {
	// Sink is where the retrieved bytes land: "local" writes to a host-side
	// output path (available on every transport); "drop" mints a one-time
	// HTTP GET filedrop the consumer pulls (only when a reachable HTTP mux
	// exists). See DownloadSink / downloadSinksFor.
	Sink DownloadSink `json:"sink"`
	// OutputPath is the host-side destination file path for sink=local. When
	// sink=local and output_path is omitted, the server picks a default in the
	// process working directory from the source name. Required before a local
	// write can proceed; unavailable sinks are rejected up front.
	OutputPath string `json:"output_path,omitempty" jsonschema:"description=Host-side destination file path for sink=local (e.g. /data/out/report.pdf). Required for local sink unless a default directory is configured."`
	// TTL is the filedrop GET lifetime for sink=drop (e.g. 5m; default 5
	// minutes). Only used with sink=drop.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Filedrop GET endpoint lifetime for sink=drop (e.g. 5m; default 5 minutes)."`
}

// downloadSinksAllowed reports whether the requested sink is one the running
// server honors, given whether a filedrop coordinator is wired and whether the
// transport is the OpenAI tunnel (no reachable mux). It is the per-invocation
// gate mirroring downloadSinksFor at registration.
func downloadSinksAllowed(sink DownloadSink, dropWired, tunnelOpenAI bool) error {
	if !sink.Valid() {
		return fmt.Errorf("unknown sink %q (valid: local, drop)", sink)
	}
	for _, s := range downloadSinksFor(dropWired, tunnelOpenAI) {
		if s == sink {
			return nil
		}
	}
	if sink == SinkDrop {
		return fmt.Errorf("sink %q is not available on this transport: no reachable filedrop GET endpoint", sink)
	}
	return fmt.Errorf("sink %q is not available on this transport", sink)
}

// sinkDefaultName derives a filesystem-safe base filename from an IPFS path or
// vault path for the local-sink default output. It returns the last path
// segment (striping any /ipfs/<cid>/ prefix and vault path) or "download".
func sinkDefaultName(source string) string {
	// Strip a vault path (vault:/... or vault:<profile>/...) authority stem.
	base := source
	if idx := strings.Index(base, ":/"); idx >= 0 {
		base = base[idx+2:]
	}
	// Strip a /ipfs/<cid>/ slash-path prefix: take everything after the last
	// slash that follows the CID, or just the last segment.
	if i := strings.LastIndex(base, "/"); i >= 0 && i+1 < len(base) {
		base = base[i+1:]
	}
	base = sanitizeFilename(base)
	if base == "download" {
		return base
	}
	return base
}

// resolveLocalOutputPath confines a caller-supplied (or absent) output path for
// sink=local to a configured download root, resolving it to a concrete host
// path and rejecting any attempt to escape the root.
//
// Security invariant: local-sink writes are confined to downloadRoot. The
// caller's output_path is a RELATIVE path within the root (subdirectories are
// allowed and created); an absolute path, a drive root, or any path whose
// cleaned lexical form escapes the root (via ".." or a Windows drive/cross-drive
// input) is rejected. This prevents a compromised MCP agent from overwriting
// arbitrary server files or redirecting decrypted vault/IPFS content elsewhere
// on the host — the mirror of upload's local-path gating.
//
// Implementation: `filepath.Join(root, rel)` replaces the root when rel is
// absolute (or a Windows volume path), and lexically resolves `..`; the returned
// path is confined only if `filepath.Rel(root, joined)` stays inside root (does
// not start with ".."). That single containment check rejects every escape —
// absolute inputs, drive inputs, and `..` traversal alike.
//
// It never writes — it only decides the destination and validates containment;
// the returned error rejects the request before any byte is read or written.
func resolveLocalOutputPath(downloadRoot, outputPath, sourceName string) (string, error) {
	name := sinkDefaultName(sourceName)
	if name == "" {
		name = defaultSourceName
	}
	root := filepath.Clean(downloadRoot)
	if root == "" || root == "." {
		return "", fmt.Errorf("download root is not configured")
	}
	rel := outputPath
	if rel == "" || rel == "." || rel == string(filepath.Separator) {
		// Absent path, or an explicit "this directory": the destination is the
		// root directory itself, so the source-derived filename is appended
		// (mirroring vault cp's directory-expansion).
		rel = name
	}
	// Reject any caller-supplied ABSOLUTE path outright. filepath.Join does not
	// reset on a leading separator (Join("a", "/b") = "a/b"), so an absolute
	// input would otherwise silently collapse INTO the root; a Windows
	// volume/drive path (C:\... or a bare "C:") is likewise never a valid
	// relative target. Because filepath.IsAbs on Windows only treats paths with
	// a drive letter as absolute (a POSIX-style "/etc/..." or a bare "\\x"
	// root-relative path is reported relative on the current drive), we also
	// reject any rel that begins with a path separator — such a path is never a
	// valid relative destination and must not be merged.
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" ||
		strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("download output path %q must be relative to the configured download root %q", outputPath, root)
	}
	// Clean both the requested path and the root, then join.
	clean := filepath.Clean(filepath.Join(root, rel))
	// The candidate is confined if and only if it is lexically inside root.
	relFromRoot, err := filepath.Rel(root, clean)
	if err != nil || relFromRoot == ".." || strings.HasPrefix(relFromRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relFromRoot) {
		return "", fmt.Errorf("download output path %q escapes the configured download root %q", outputPath, root)
	}
	return clean, nil
}

// writeLocalDownload streams the source bytes to a host-side output path
// atomically: it writes to a temp file in the destination directory, then
// renames onto the final path only after the stream succeeds, so a failed or
// interrupted download never leaves a truncated file as if it were complete.
// The destination directory is created if missing. An existing destination is
// overwritten by the rename (the caller is expected to gate on --force-style
// semantics at the tool boundary if desired).
func writeLocalDownload(ctx context.Context, outputPath string, maxBytes int64, resolve func(ctx context.Context, w io.Writer) error) (int64, error) {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create destination directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".pinner-dl-*")
	if err != nil {
		return 0, fmt.Errorf("create temp download file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after successful rename
	// Enforce the download size cap at the stream: over-limit bytes fail
	// loudly (and the temp file is removed by the defer) rather than landing a
	// truncated file as if it were complete.
	if err := resolve(ctx, &sizeLimitedWriter{w: tmp, maxBytes: maxBytes}); err != nil {
		tmp.Close()
		return 0, err
	}
	info, err := tmp.Stat()
	if err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return 0, fmt.Errorf("finalize download: %w", err)
	}
	return info.Size(), nil
}

// downloadResult is the canonical envelope returned by both download tools.
type downloadResult struct {
	Status   string `json:"status"`
	Source   string `json:"source"`
	Sink     string `json:"sink"`
	Output   string `json:"output_path,omitempty"`   // sink=local
	FetchURL string `json:"fetch_url,omitempty"`     // sink=drop
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	TTL      string `json:"ttl,omitempty"` // sink=drop
	Error    string `json:"error,omitempty"`
}

// executeLocalSink resolves bytes to a root-confined host path and returns a
// local result. It rejects any output path that escapes downloadRoot.
func executeLocalSink(ctx context.Context, source, sourceName, outputPath, downloadRoot string, maxBytes int64, resolve func(ctx context.Context, w io.Writer) error) (downloadResult, error) {
	final, err := resolveLocalOutputPath(downloadRoot, outputPath, sourceName)
	if err != nil {
		return downloadResult{}, err
	}
	size, err := writeLocalDownload(ctx, final, maxBytes, resolve)
	if err != nil {
		return downloadResult{}, err
	}
	return downloadResult{
		Status: "ok",
		Source: source,
		Sink:   string(SinkLocal),
		Output: final,
		Name:   filepath.Base(final),
		Size:   size,
	}, nil
}

// executeDropSink mints a one-time filedrop GET and returns a drop result. The
// ttl string is parsed; when omitted/invalid/<=0 the default is applied so the
// reported TTL matches what mint actually enforces (mint does the same default
// internally, but reporting "0s" would mislead a consumer into treating a live
// endpoint as already expired). The serve closure enforces maxBytes (<=0 =
// unlimited) so an over-limit GET fails loudly instead of streaming a partial
// file.
func executeDropSink(ctx context.Context, source, sourceName string, hd *httpDownload, ttl string, maxBytes int64, resolve func(ctx context.Context, w io.Writer) error) (downloadResult, error) {
	if hd == nil {
		return downloadResult{}, errors.New("filedrop GET coordinator is not configured for sink=drop")
	}
	var d time.Duration
	if ttl != "" {
		if parsed, err := time.ParseDuration(ttl); err == nil && parsed > 0 {
			d = parsed
		}
	}
	if d <= 0 {
		d = defaultHTTPDownloadTTL
	}
	// The serve closure mirrors the same resolve the local sink uses, wrapped
	// with the size cap; the size is unknown up front (we do not pre-buffer),
	// so report 0 unless the tool supplies it later.
	capped := func(ctx context.Context, w io.Writer) error {
		return resolve(ctx, &sizeLimitedWriter{w: w, maxBytes: maxBytes})
	}
	fetchURL, err := hd.mint(sourceName, 0, capped, d)
	if err != nil {
		return downloadResult{}, err
	}
	return downloadResult{
		Status:   "ok",
		Source:   source,
		Sink:     string(SinkDrop),
		FetchURL: fetchURL,
		Name:     sourceName,
		TTL:      d.String(),
	}, nil
}

// sizeLimitedWriter wraps an io.Writer with a hard byte cap. Unlike
// io.LimitWriter (which silently discards bytes past the cap — corrupting a
// download), it returns an error the moment a write would exceed maxBytes, so
// an over-limit stream fails loudly instead of landing a truncated file. A
// maxBytes <= 0 means "no limit".
type sizeLimitedWriter struct {
	w        io.Writer
	maxBytes int64
	written  int64
}

func (lw *sizeLimitedWriter) Write(p []byte) (int, error) {
	if lw.maxBytes > 0 && lw.written+int64(len(p)) > lw.maxBytes {
		return 0, fmt.Errorf("download exceeds max_mcp_upload_size (%d bytes)", lw.maxBytes)
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}

// defaultSourceName is a shared fallback for an empty derived name.
const defaultSourceName = "download"

// resolveDownloadRoot returns the host directory confining download_file /
// vault_get_file local-sink writes. It prefers the operator-supplied supplier
// (from WithDownloadRoot), falling back to the config default
// (<config-dir>/downloads). The value is returned verbatim — Clean/containment
// is applied by resolveLocalOutputPath at invocation time.
func resolveDownloadRoot(supplier func() string) string {
	if supplier != nil {
		if r := supplier(); r != "" {
			return r
		}
	}
	return config.DefaultDownloadRoot()
}
