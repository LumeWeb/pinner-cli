package transfer

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// DataURIUploadInput is the typed argument shape for upload_data.
// The File field has no omitempty tag, so the jsonschema reflector marks it
// required (matching the wizard step-input convention).
type DataURIUploadInput struct {
	File string `json:"file" jsonschema:"format=uri,description=RFC 2397 data: URI with a base64-encoded payload, e.g. data:;base64,<base64 payload> or data:text/plain;base64,<base64 payload>; optional ;name=<name>;size=<n> parameters are accepted but not required. The payload is base64. The bytes do not enter the model context; the host supplies this value from a user-attached file."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the data URI name, else 'upload')."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it)."`
	Wrap bool   `json:"wrap,omitempty" jsonschema:"description=Wrap the single file in a directory root so the resulting CID is a directory. Required when the upload is a website (a website resolves to a directory, not a bare file). When wrap=true and no name is given, HTML content is auto-named index.html so the site resolves at its root. An explicit name such as 'starter-site' is honored as-is and the page is then only reachable at /starter-site, not /. True only affects single-file uploads; directory uploads are already a directory root."`
}

// dataURIUploadDesc composes the upload_data tool description per profile.
// upload_data is only usable on a host that exposes the data: URI relay
// (FeatSourceData — the OpenAI tunnel). On a host without it (Grok, generic
// HTTP) the copy unconditionally forbids the tool so a model never
// base64-encodes a sandbox file when mint + PUT is the byte path. The old
// ChatGPT-oriented stop-rule ("do not call when host_file_input == true") is
// gone: it was a negation that flipped meaning after the honest host_file_input
// report, so the gate is now the presence of the data relay itself.
var dataURIUploadDesc = toolforge.Static(
	"Upload bytes from an RFC 2397 data: URI and pin the resulting CID. The returned CID is already pinned, so pins_add is not needed afterward; the wait flag waits for this upload's own pin operation.",
).
	When(hostenv.FeatSourceData,
		"Last resort — not for a host-provided or assistant-generated file.",
	).
	WhenAll([]hostenv.Feature{hostenv.FeatSourceMint, hostenv.FeatSourceURL, hostenv.FeatSourceData},
		"On this host prefer upload_file (mint + PUT) for an agent-local file and upload_url for a public HTTPS URL.",
	).
	Unless(hostenv.FeatSourceData,
		"This transport has no data: URI relay. Upload bytes with upload_file(source.mode=mint) by PUTting the agent-local file to the returned url, then poll upload_status; a file is not base64-encoded as a data URI.",
	)

// DataURIUploadTargets is the per-profile description target for upload_data,
// resolved against the calling host profile (via describe_tool/search_tools).
// It is exported so the server can re-resolve a dedicated per-host description
// on tools/list.
var DataURIUploadTargets = toolforge.MCPTargets(model.ToolTarget{
	Visible:  true,
	DescFunc: dataURIUploadDesc.Resolve,
})

// DataURIUploadDescriptor uploads a file passed as a SEP-2356 data: URI. This
// is the additive, optional draft-MCP file-input mode declared via the
// x-mcp-file schema annotation; hosts that don't speak the draft simply omit
// it. Bytes are decoded by Pinner from the data URI, never re-emitted into the
// model context. The base64 payload is streamed to the upload handler, not
// materialized in memory.
func DataURIUploadDescriptor(handler DataURIUploadHandler, maxBytes int64) model.ToolDescriptor {
	maxBytes = ieo.EffectiveRelayMaxBytes(maxBytes)
	// dataURIUploadDescription is the startup/tools-list bake. upload_data only
	// works on a host that exposes the data: URI relay (FeatSourceData, the
	// OpenAI tunnel). The per-request surface resolves the same builder against
	// the calling profile (see dataURIUploadTargets) so a host without the
	// relay sees an unconditional forbid instead of this neutral copy.
	dataURIUploadDescription := dataURIUploadDesc.Resolve(hostenv.ProfileForTransport(TransportOpenAI))
	return model.ToolDescriptor{
		Name:          "upload_data",
		Title:         "Upload a file from a data URI",
		Description:   dataURIUploadDescription,
		Category:      model.CategoryCore,
		OpenWorldHint: true, // submits content to the Pinner/IPFS network
		MCPTargets:    DataURIUploadTargets,
		InputSchema:   toolargs.ToolSchemaFor[DataURIUploadInput](),
		// x-mcp-file marks the "file" property as a file-valued input per the
		// draft spec; the SDK's Meta map carries it without a typed field.
		Meta: map[string]any{"x-mcp-file": map[string]any{"file": map[string]any{"transferModes": []string{"inline"}}}},
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeArgsFor[DataURIUploadInput]("data URI upload", handler != nil, request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.File == "" {
				return model.ToolResult{}, fmt.Errorf("file (data URI) is required")
			}
			reader, opt, err := ieo.ParseFileDataURI(in.File, maxBytes)
			if err != nil {
				return model.ToolResult{}, err
			}
			name := in.Name
			if name == "" {
				name = opt.Name
			}
			if name == "" {
				name = DefaultUploadName
			}
			// Bound the upload phase; see SyncUploadBudget.
			transferCtx, cancel := context.WithTimeout(ctx, SyncUploadBudget(opt.Size))
			defer cancel()
			// data: URI input exposes no archive_mode field, so the upload must
			// always stay single-file. Pass an explicit "preserve" so
			// ParseArchiveMode cannot default "" to convert and silently extract
			// a base64 ZIP into a directory DAG the caller cannot opt out of.
			result, err := handler(transferCtx, reader, opt.Size, name, in.Wait, "preserve", in.Wrap)
			return toolargs.WrapResult(result, err, "Data URI uploaded.")
		},
	}
}
