package mcp

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// RelayURLUploadInput is the typed argument shape for upload_url.
type RelayURLUploadInput struct {
	URL  string `json:"url" jsonschema:"format=uri,description=Public HTTPS URL to fetch and upload."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
}

// RelayURLUploadDescriptor uploads a file by having the local MCP process
// fetch a caller-supplied HTTPS URL, then stream it through the existing
// authenticated TUS path. This is the generic relay fallback for HTTP-mode
// clients that are not co-located with Pinner and cannot pass a host path.
func RelayURLUploadDescriptor(handler RelayURLUploadHandler, allowedHosts []string, maxBytes int64) model.ToolDescriptor {
	maxBytes = effectiveRelayMaxBytes(maxBytes)
	return model.ToolDescriptor{
		Name:        "upload_url",
		Title:       "Upload a file from a URL",
		Description: "Fetch a public HTTPS URL locally and upload it to Pinner through the authenticated upload path. Do not put Pinner's credentials in the URL; Pinner fetches it with its own stored auth. Intended for remote HTTP clients that cannot reference a local path.",
		Category:    model.CategoryCore,
		InputSchema: toolSchemaFor[RelayURLUploadInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := decodeArgsFor[RelayURLUploadInput]("relay URL upload", handler != nil, request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.URL == "" {
				return model.ToolResult{}, fmt.Errorf("url is required")
			}
			body, size, err := OpenFileURL(ctx, in.URL, FileRelayOptions{
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
			// indefinitely. Budget scales with size; see syncUploadBudget.
			transferCtx, cancel := context.WithTimeout(ctx, syncUploadBudget(size))
			defer cancel()
			result, err := handler(transferCtx, body, size, in.Name, in.Wait)
			return wrapResult(result, err, "URL uploaded.")
		},
	}
}
