package upload

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// RelayURLUploadInput is the typed argument shape for upload_url.
type RelayURLUploadInput struct {
	URL  string `json:"url" jsonschema:"format=uri,description=Public HTTPS URL to fetch and upload."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it)."`
	Wrap bool   `json:"wrap,omitempty" jsonschema:"description=Wrap the single fetched file in a directory root so the CID is a directory (required when the upload is a website)."`
}

// RelayURLUploadDescriptor uploads a file by having the local MCP process
// fetch a caller-supplied HTTPS URL, then stream it through the existing
// authenticated TUS path. This is the generic relay fallback for HTTP-mode
// clients that are not co-located with Pinner and cannot pass a host path.
func RelayURLUploadDescriptor(handler transfer.RelayURLUploadHandler, allowedHosts []string, maxBytes int64) model.ToolDescriptor {
	maxBytes = ieo.EffectiveRelayMaxBytes(maxBytes)
	return model.ToolDescriptor{
		Name:        "upload_url",
		Title:       "Upload a file from a URL",
		Description: "Fetch a public HTTPS URL and upload it to Pinner, pinning the resulting CID. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. Do not put Pinner's credentials in the URL; Pinner fetches with its own stored auth. For remote HTTP clients that cannot reference a local path.",
		Category:    model.CategoryCore,
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
