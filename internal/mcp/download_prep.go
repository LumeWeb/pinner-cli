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

// resolveLocalOutputPath expands a caller-supplied (or absent) output path for
// sink=local, mirroring vault cp's directory-expansion: a trailing slash or an
// existing directory joins the source-derived filename; otherwise the path is
// used verbatim. It never writes — it only decides the destination.
func resolveLocalOutputPath(outputPath, sourceName string) string {
	name := sinkDefaultName(sourceName)
	if outputPath == "" {
		outputPath = name
	} else if outputPath == "." || strings.HasSuffix(outputPath, string(filepath.Separator)) || strings.HasSuffix(outputPath, "/") {
		outputPath = filepath.Join(outputPath, name)
	} else if fi, err := os.Stat(outputPath); err == nil && fi.IsDir() {
		outputPath = filepath.Join(outputPath, name)
	}
	return outputPath
}

// writeLocalDownload streams the source bytes to a host-side output path
// atomically: it writes to a temp file in the destination directory, then
// renames onto the final path only after the stream succeeds, so a failed or
// interrupted download never leaves a truncated file as if it were complete.
// The destination directory is created if missing. An existing destination is
// overwritten by the rename (the caller is expected to gate on --force-style
// semantics at the tool boundary if desired).
func writeLocalDownload(ctx context.Context, outputPath string, resolve func(ctx context.Context, w io.Writer) error) (int64, error) {
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
	if err := resolve(ctx, tmp); err != nil {
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

// executeLocalSink resolves bytes to a host path and returns a local result.
func executeLocalSink(ctx context.Context, source, sourceName, outputPath string, resolve func(ctx context.Context, w io.Writer) error) (downloadResult, error) {
	final := resolveLocalOutputPath(outputPath, sourceName)
	size, err := writeLocalDownload(ctx, final, resolve)
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
// ttl string is parsed (default applied inside mint when <= 0 / unparsable).
func executeDropSink(ctx context.Context, source, sourceName string, hd *httpDownload, ttl string, resolve func(ctx context.Context, w io.Writer) error) (downloadResult, error) {
	if hd == nil {
		return downloadResult{}, errors.New("filedrop GET coordinator is not configured for sink=drop")
	}
	var d time.Duration
	if ttl != "" {
		if parsed, err := time.ParseDuration(ttl); err == nil && parsed > 0 {
			d = parsed
		}
	}
	// The serve closure mirrors the same resolve the local sink uses; the size
	// is unknown up front (we do not pre-buffer), so report 0 unless the tool
	// supplies it later.
	fetchURL, err := hd.mint(sourceName, 0, resolve, d)
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

// defaultSourceName is a shared fallback for an empty derived name.
const defaultSourceName = "download"
