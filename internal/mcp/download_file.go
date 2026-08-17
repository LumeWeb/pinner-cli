package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// DownloadFileInput is the typed argument shape for the unified download_file
// tool. The caller supplies an IPFS path (CID or CID/path) plus a sink telling
// where the retrieved bytes should land. The tool routes to the real delivery
// mechanism based on the server's configured sinks — the caller never picks a
// mechanism.
type DownloadFileInput struct {
	// IPFSPath is the content to download, a CID or CID/subpath.
	IPFSPath string `json:"ipfs_path" jsonschema:"description=IPFS content to download, a CID or CID/path (e.g. bafy.../subdir/file.txt). Required."`
	// Sink tells where the bytes land: "local" writes to a host-side path
	// (available on every transport); "drop" mints a one-time HTTP GET
	// filedrop (only when a reachable HTTP mux exists).
	Sink DownloadSink `json:"sink" jsonschema:"enum=local,drop,description=Where the downloaded bytes land: local writes to a host-side output_path on the MCP server's disk (available on every transport); drop mints a one-time HTTP GET filedrop link to pull from out of band."`
	// Name is an optional filename override for the downloaded file (used for
	// the local output name and the filedrop attachment name). Defaults to the
	// last path segment of ipfs_path.
	Name string `json:"name,omitempty" jsonschema:"description=Optional filename for the downloaded file (defaults to the last segment of ipfs_path)."`
	// OutputPath is the host-side destination for sink=local.
	OutputPath string `json:"output_path,omitempty" jsonschema:"description=Host-side destination file path for sink=local (e.g. /data/out/report.pdf). If omitted, the file is written to the current working directory using the source name."`
	// TTL is the filedrop GET lifetime for sink=drop (e.g. 5m; default 5m).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Filedrop GET endpoint lifetime for sink=drop (e.g. 5m; default 5 minutes)."`
}

// NewDownloadFileDescriptor builds the unified, sink-aware download_file tool.
// It downloads an IPFS node (CID or CID/path) and routes the retrieved bytes to
// the requested sink:
//
//   - sink=local (every transport): ipfsFn streams the CID bytes to a host-side
//     output path on the MCP server's own disk. Valid regardless of transport,
//     because the server's disk is always local to the server process.
//   - sink=drop (HTTP / real tunnel): hd mints a one-time HTTP GET filedrop the
//     consumer pulls with curl -o / a browser <a download> link.
//
// On the OpenAI tunnel, only sink=local is honored (no reachable HTTP mux for
// a drop). The handler validates the sink against downloadSinksAllowed before
// any byte is read or written.
func NewDownloadFileDescriptor(ipfsFn IPFSDownloadHandler, hd *httpDownload, tunnelOpenAI bool) ToolDescriptor {
	return ToolDescriptor{
		Name:        "download_file",
		Title:       "Download IPFS content to a file",
		Description: downloadFileDescription(hd != nil, tunnelOpenAI),
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[DownloadFileInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			in, err := decodeToolArgs[DownloadFileInput](request)
			if err != nil {
				return ToolResult{}, err
			}
			if in.IPFSPath == "" {
				return ToolResult{}, fmt.Errorf("ipfs_path is required")
			}
			// Validate the sink against what this transport actually offers
			// before reading anything.
			if err := downloadSinksAllowed(in.Sink, hd != nil, tunnelOpenAI); err != nil {
				return ToolResult{}, err
			}
			name := in.Name
			if name == "" {
				name = sinkDefaultName(in.IPFSPath)
			}
			if name == "" {
				name = defaultSourceName
			}

			switch in.Sink {
			case SinkLocal:
				if ipfsFn == nil {
					return ToolResult{}, errors.New("IPFS download handler is not configured")
				}
				res, err := executeLocalSink(ctx, in.IPFSPath, name, in.OutputPath, func(ctx context.Context, w io.Writer) error {
					return ipfsFn(ctx, in.IPFSPath, w)
				})
				return wrapResult(res, err, "Downloaded from IPFS.")
			case SinkDrop:
				res, err := executeDropSink(ctx, in.IPFSPath, name, hd, in.TTL, func(ctx context.Context, w io.Writer) error {
					return ipfsFn(ctx, in.IPFSPath, w)
				})
				return wrapResult(res, err, "Filedrop minted; pull the bytes from fetch_url.")
			default:
				return ToolResult{}, fmt.Errorf("unknown sink %q", in.Sink)
			}
		},
	}
}

// downloadFileDescription returns a sink-aware description so a model only sees
// sinks that can actually work on the running transport.
func downloadFileDescription(dropWired, tunnelOpenAI bool) string {
	if dropWired && !tunnelOpenAI {
		return "Download IPFS content (CID or CID/path) as a file. Set sink=local to write the bytes to a host-side output_path on the MCP server's own disk (available on every transport), or sink=drop to get a one-time HTTP GET filedrop link to pull from out of band (curl -o <url> or a browser link)."
	}
	return "Download IPFS content (CID or CID/path) as a file. Set sink=local to write the bytes to a host-side output_path on the MCP server's own disk. (The filedrop GET sink is unavailable on this tunnel transport.)"
}
