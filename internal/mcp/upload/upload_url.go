package upload

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// RelayURLUploadInput is the typed argument shape for upload_url.
type RelayURLUploadInput struct {
	URL  string `json:"url" jsonschema:"format=uri,description=Public HTTPS URL to fetch and upload."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it)."`
	Wrap bool   `json:"wrap,omitempty" jsonschema:"description=Wrap the single fetched file in a directory root so the CID is a directory (required when the upload is a website)."`
}

// relayURLUploadDesc composes the upload_url tool description per profile.
// upload_url is a server-fetch URL relay — only a host with FeatSourceURL can
// have bytes fetched for it. On a host without it (Grok, generic HTTP) the copy
// unconditionally forbids the tool so a model never routes the byte path
// through a URL fetch when mint + PUT is what works.
var relayURLUploadDesc = toolforge.Static(
	"Fetch a public HTTPS URL and upload it to Pinner, pinning the resulting CID. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. Do not put Pinner's credentials in the URL; Pinner fetches with its own stored auth.",
).
	When(hostenv.FeatSourceURL,
		"For hosts that expose a server-fetchable URL relay.",
	).
	Unless(hostenv.FeatSourceURL,
		"Do NOT call this tool on this host: it has no URL-fetch relay. Upload bytes with upload_file(source.mode=mint) by PUTting the agent-local file to the returned url, then poll upload_status.",
	)

// RelayURLUploadTargets is the per-profile description target for upload_url,
// exported so the server can re-resolve a dedicated per-host description.
var RelayURLUploadTargets = toolforge.MCPTargets(model.ToolTarget{
	Visible:  true,
	DescFunc: relayURLUploadDesc.Resolve,
})

// RelayURLUploadDescriptor uploads a file by having the local MCP process
// fetch a caller-supplied HTTPS URL, then stream it through the existing
// authenticated TUS path. This is the generic relay fallback for HTTP-mode
// clients that are not co-located with Pinner and cannot pass a host path.
func RelayURLUploadDescriptor(handler transfer.RelayURLUploadHandler, allowedHosts []string, maxBytes int64) model.ToolDescriptor {
	maxBytes = ieo.EffectiveRelayMaxBytes(maxBytes)
	return model.ToolDescriptor{
		Name:        "upload_url",
		Title:       "Upload a file from a URL",
		Description: relayURLUploadDesc.Resolve(hostenv.ProfileForTransport(transfer.TransportHTTP)),
		Category:    model.CategoryCore,
		// Profile-aware target so the description resolves through the catalog
		// seam (describe_tool/search_tools) per calling host like every other
		// custom tool.
		MCPTargets:  RelayURLUploadTargets,
		InputSchema: toolargs.ToolSchemaFor[RelayURLUploadInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeArgsFor[RelayURLUploadInput]("relay URL upload", handler != nil, request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.URL == "" {
				return model.ToolResult{}, fmt.Errorf("url is required")
			}
			body, size, err := ieo.OpenFileURL(ctx, in.URL, ieo.FileRelayOptions{
				AllowedHosts:   allowedHosts,
				MaxBytes:       maxBytes,
				RequestTimeout: 2 * time.Minute,
			})
			if err != nil {
				return model.ToolResult{}, err
			}
			defer body.Close()
			// Bound the upload itself: the MCP request ctx may carry no
			// deadline, so a hung TUS/network operation must not run
			// indefinitely. Budget scales with size; see SyncUploadBudget.
			transferCtx, cancel := context.WithTimeout(ctx, transfer.SyncUploadBudget(size))
			defer cancel()
			// Relay URL input exposes no archive_mode field, so the upload must
			// always stay single-file. Pass an explicit "preserve" so
			// ParseArchiveMode cannot default "" to convert and silently extract
			// a fetched ZIP into a directory DAG the caller cannot opt out of.
			result, err := handler(transferCtx, body, size, in.Name, in.Wait, "preserve", in.Wrap)
			return toolargs.WrapResult(result, err, "URL uploaded.")
		},
	}
}
