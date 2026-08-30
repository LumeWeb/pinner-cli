package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/invopop/jsonschema"

	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.uber.org/zap"
)

// VaultPutHandler is the authenticated vault write executor. It writes bytes
// from a reader into the encrypted vault at the given destination path via the
// existing authenticated vault service. metadata carries the caller-supplied KV
// plus the auto-stamped write-context keys (src, host, profile) built by the
// tool handler; the CLI closure stamps profile before invoking VaultService.Put.
type VaultPutHandler func(context.Context, io.Reader, int64, string, map[string]any) (any, error)

// LocalPathVaultPutHandler writes a host-side file, directory, or archive into
// the encrypted vault. It homes the file-vs-dir-vs-archive decision in the CLI
// layer where the vault service lives (the same split as upload_file's
// path mode / LocalPathUploadHandler). metadata is threaded to every
// per-file vault write, mirroring VaultPutHandler.
type LocalPathVaultPutHandler func(ctx context.Context, path, vaultPath, archiveMode string, metadata map[string]any) (any, error)

// VaultPutFileInput is the typed argument shape for the unified vault_put_file
// tool. The caller supplies a single transport-scoped `source` plus a vault
// destination path; the tool routes to the real file-input mechanism based on
// the server's transport — the caller never picks a mechanism.
type VaultPutFileInput struct {
	// Source is the file to store. Mode must be valid for the running
	// transport: path=co-located stdio; mint=HTTP/tunnel (returns a presigned
	// curl PUT URL); url/data=OpenAI tunnel (relayed through MCP). Mutually
	// exclusive with File.
	Source *transfer.UploadSource `json:"source,omitempty"`
	// File is an OpenAI/host-provided generated-file reference (temporary
	// download_url + file_id). It lets a ChatGPT user hand a file it created
	// in its own environment directly to the vault, without a human file-picker
	// or manual transport. Mutually exclusive with Source.
	File *transfer.ChatGPTFileInput `json:"file,omitempty"`
	// VaultPath is the destination file path inside the encrypted vault. It
	// must be a file path (not a directory) free of parent-relative traversal.
	// Any vault file path is allowed (e.g. vault:/docs/f.pdf); there is no
	// single-folder restriction — see ValidateVaultFilePath.
	VaultPath string `json:"vault_path" jsonschema:"description=Vault destination file path (e.g. vault:/docs/f.pdf or vault:/uploads/report.pdf). Required. Must be a file path, not a directory; traversal (.. or .) segments are rejected. Any vault file path is allowed."`
	// Profile is the vault profile the file is written into. Defaults to the
	// active profile on a single-profile server; on a multi-profile server it
	// is REQUIRED — omitting it fails with profile_required instead of
	// silently targeting the active vault. Specify it to write into another
	// vault without switching the default (avoids a swarm flipping the active
	// profile via vault_profile_use).
	Profile string `json:"profile,omitempty" jsonschema:"description=Vault profile name to write into. Required when more than one profile is unlocked (omitting it returns profile_required and mints nothing); on a single-profile server it defaults to the active profile. Specify a different profile to store in another vault without changing the default."`
	// ArchiveMode controls how an archive path is handled: 'convert' (default)
	// extracts and stores the contents; 'preserve' keeps the archive intact.
	// Only used for source mode path.
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,enum=preserve,description=How to treat an archive path ('convert' extracts, 'preserve' keeps intact). Only used for source mode path."`
	// TTL is the presigned endpoint lifetime for source mode mint (e.g. 5m).
	// Only used in HTTP/tunnel mode.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint."`
	// Agent is an identifier for the creating agent (e.g. an orchestrator
	// name). It is stored as metadata alongside the auto-stamped write-context
	// keys — never as a tag.
	Agent string `json:"agent,omitempty" jsonschema:"description=Identifier for the creating agent (e.g. an orchestrator name). Stored as vault metadata (not a tag)."`
	// Metadata is caller-supplied key-value pairs (e.g. kind, project, role).
	// Reserved write-context keys (src, host, profile) are auto-stamped by the
	// environment and cannot be overridden here.
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"description=Caller-supplied metadata key-value pairs (e.g. kind, project, role). Reserved keys (src, host, profile) are auto-stamped and cannot be overridden."`
	// Tags are applied atomically at write time, durable on the sealed object
	// metadata and the local tag index. Tags are normalized (lowercased,
	// deduped). This eliminates the need for a separate vault_tag_add call
	// after upload.
	Tags []string `json:"tags,omitempty" jsonschema:"description=Tags to apply to the file at write time. Durable: sealed into the object metadata and the local tag index so they sync to every device. Normalized (lowercased, deduped). Eliminates the need for a separate vault_tag_add call."`
}

// NewVaultPutFileDescriptor builds the unified, transport-aware vault_put_file
// tool. The transport is selected at registration time from the wiring flags,
// and the handler routes the caller's source mode to the real mechanism:
//
//   - stdio (coLocated): source mode path → pathFn writes the host path.
//   - HTTP/tunnel (remoteSupported): source mode mint → vu mints a presigned
//     PUT bound to the destination vault path.
//   - OpenAI tunnel (neither): source mode url/data → relayed through MCP via
//     SourceResolver.OpenBytes into the authenticated vault write.
//
// The vault destination path is validated (ValidateVaultFilePath) BEFORE any
// byte is read or written on every source branch, so a directory or traversal
// destination can never be written regardless of transport. Any vault file
// path is an allowed destination; there is no single-folder restriction.
func NewVaultPutFileDescriptor(features hostenv.FeatureSet, coLocated, tunnelOpenAI bool, pathFn LocalPathVaultPutHandler, vu *transfer.VaultHTTPUpload, relayFn VaultPutHandler, relayHosts []string, maxRelayBytes int64) model.ToolDescriptor {
	return newVaultPutFileDescriptor(features, coLocated, tunnelOpenAI, pathFn, vu, relayFn, relayHosts, maxRelayBytes, corevault.ProfileRequired, nil)
}

// NewVaultPutFileDescriptorWithProfileCheck builds a vault_put_file descriptor
// with an injectable multi-profile guard (profileCheck reports a
// *corevault.ProfileRequiredError when an operation must target an explicit
// profile; production uses the registry-backed corevault.ProfileRequired). It
// exists so tests can simulate a multi-profile server without touching the real
// vault registry. httpClient, when non-nil, overrides the client used to fetch
// an OpenAI `file` download_url (a deliberate trust decision by embedding Go
// code; production passes nil and uses Pinner's SSRF-guarded client).
func NewVaultPutFileDescriptorWithProfileCheck(features hostenv.FeatureSet, coLocated, tunnelOpenAI bool, pathFn LocalPathVaultPutHandler, vu *transfer.VaultHTTPUpload, relayFn VaultPutHandler, relayHosts []string, maxRelayBytes int64, profileCheck func(string) *corevault.ProfileRequiredError, httpClient *http.Client) model.ToolDescriptor {
	return newVaultPutFileDescriptor(features, coLocated, tunnelOpenAI, pathFn, vu, relayFn, relayHosts, maxRelayBytes, profileCheck, httpClient)
}

// newVaultPutFileDescriptor is the implementation behind the two exported
// constructors. profileCheck applies the multi-profile profile-required rule
// before any byte is written or URL minted. httpClient, when non-nil, overrides
// the client used to fetch an OpenAI `file` download_url. It is a deliberate
// trust decision by embedding Go code (tests); production passes nil and uses
// Pinner's SSRF-guarded client.
func newVaultPutFileDescriptor(features hostenv.FeatureSet, coLocated, tunnelOpenAI bool, pathFn LocalPathVaultPutHandler, vu *transfer.VaultHTTPUpload, relayFn VaultPutHandler, relayHosts []string, maxRelayBytes int64, profileCheck func(string) *corevault.ProfileRequiredError, httpClient *http.Client) model.ToolDescriptor {
	transport := transfer.UploadFileTransport(coLocated, tunnelOpenAI)
	hostFile := features.Has(hostenv.FeatFileHostInput)
	var meta map[string]any
	if hostFile {
		// Advertise the OpenAI file-parameter handoff so a ChatGPT/OpenAI host
		// knows the top-level `file` argument carries a generated-file
		// reference (temporary download_url + file_id) it can populate from a
		// file it owns, without a human file-picker. This metadata is additive
		// to any other Pinner metadata. Hosts without FeatFileHostInput (e.g.
		// Grok) must not advertise it.
		meta = transfer.ChatGPTFileMeta()
	}
	return model.ToolDescriptor{
		Name:        "vault_put_file",
		Title:       "Store a file in the Pinner vault",
		Description: vaultPutFileDescription(transport),
		Category:    model.CategoryCore,
		// The input schema is compiled from the profile's feature set (see
		// vaultPutFileSchema), so file-handoff presence, the source.mode enum,
		// and the mode prose all follow the connected host.
		InputSchema: vaultPutFileSchema(features),
		Meta:        meta,
		MCPTargets:  toolforge.VaultPutFileTargets,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[VaultPutFileInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
			}
			// SECURITY INVARIANT: validate the destination path before any byte
			// is read or written — a well-formed FILE path, not a directory,
			// and free of parent-relative traversal / profile authority. The
			// destination is intentionally NOT confined to a single vault
			// folder: a caller may write to any vault file path (e.g. the
			// historical vault_put_path target vault:/docs/f.pdf). The mint
			// flow additionally re-validates defensively inside its own mint().
			if err := transfer.ValidateVaultFilePath(in.VaultPath); err != nil {
				return model.ToolResult{}, err
			}

			// Deterministic source selection: exactly one byte source must be
			// provided. Do not let a silent precedence rule decide between
			// `file` and `source` for the caller.
			hasSource := in.Source != nil
			hasFile := in.File != nil
			switch {
			case !hasSource && !hasFile:
				return model.ToolResult{}, errors.New("an upload source is required")
			case hasSource && hasFile:
				return model.ToolResult{}, errors.New("provide exactly one upload source")
			}

			// Multi-profile guard: on a server with more than one unlocked
			// profile, a write/mint without an explicit profile must fail here
			// (profile_required) rather than silently target the active vault
			// and later fail on the write with an unusable one-shot upload URL.
			// Mirrors the guard on every catalog vault op so both surfaces
			// agree that a missing profile is an error, never a default.
			if pr := profileCheck(in.Profile); pr != nil {
				return model.ErrorResult(pr.Code, pr.Message, map[string]any{
					"profiles": pr.Profiles,
				}), nil
			}

			// Build the metadata the vault write seals into the object. The
			// environment stamps src=mcp and, when the request carries a
			// detected host profile, host=<host type>. An explicit profile
			// argument stamps the destination profile so the CLI write closure
			// (which owns service construction) routes to that vault; an empty
			// profile omits the key and the closure resolves the active one.
			// Caller-supplied KV (agent, kind, project, ...) is merged on top;
			// the reserved write-context keys are never overridable.
			callerKV := in.Metadata
			if in.Agent != "" {
				if callerKV == nil {
					callerKV = map[string]any{}
				}
				callerKV["agent"] = in.Agent
			}
			if len(in.Tags) > 0 {
				if callerKV == nil {
					callerKV = map[string]any{}
				}
				callerKV["tags"] = in.Tags
			}
			var hostType string
			if request.Caps != nil && request.Caps.Profile != nil {
				hostType = string(request.Caps.Profile.HostType)
			}
			metadata := corevault.StampedMetadata("mcp", hostType, in.Profile, callerKV)

			// OpenAI/host-provided generated-file handoff. The host passes a
			// temporary download_url + file_id; Pinner fetches/streams the bytes
			// through the same authenticated vault write executor the relay
			// url/data sources use — there is no separate vault path.
			if hasFile {
				if relayFn == nil {
					return model.ToolResult{}, errors.New("vault relay write is not configured")
				}
				_, body, size, oerr := transfer.OpenChatGPTFileInput(ctx, *in.File, transfer.ChatGPTOpenTimeout, maxRelayBytes, relayHosts, httpClient)
				if oerr != nil {
					return model.ToolResult{}, oerr
				}
				defer body.Close()
				writeCtx, cancel := context.WithTimeout(ctx, transfer.SyncUploadBudget(size))
				defer cancel()
				result, err := relayFn(writeCtx, body, size, in.VaultPath, metadata)
				return toolargs.WrapResult(result, err, "Stored in the vault.")
			}

			src := *in.Source
			if err := src.Validate(transport); err != nil {
				return model.ToolResult{}, err
			}

			switch transport {
			case transfer.TransportStdio:
				if src.Mode != transfer.SourcePath {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", src.Mode, transport)
				}
				if pathFn == nil {
					return model.ToolResult{}, errors.New("local path vault handler is not configured")
				}
				result, err := pathFn(ctx, src.Path, in.VaultPath, in.ArchiveMode, metadata)
				return toolargs.WrapResult(result, err, "Stored in the vault.")
			case transfer.TransportHTTP:
				if src.Mode != transfer.SourceMint {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", src.Mode, transport)
				}
				if vu == nil {
					return model.ToolResult{}, errors.New("presigned vault-upload endpoint is not configured for remote mode")
				}
				ttl := transfer.DefaultHTTPUploadTTL
				if in.TTL != "" {
					d, derr := time.ParseDuration(in.TTL)
					if derr != nil {
						return model.ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
					}
					if d > 0 {
						ttl = d
					}
				}
				url, merr := vu.Mint(in.VaultPath, ttl, metadata)
				if merr != nil {
					return model.ToolResult{}, merr
				}
				curlCmd := fmt.Sprintf("curl -sS -T <your-file> %q", url)
				sc := map[string]any{
					"url":          url,
					"vault_path":   in.VaultPath,
					"curl_command": curlCmd,
					"ttl":          ttl.String(),
					"max_bytes":    vu.MaxBytes(),
				}
				return model.ToolResult{
					StructuredContent: sc,
					// Text carries the same JSON so a text-only client sees the
					// actual presigned URL and curl command, not just prose.
					Text: toolargs.ResultJSONText(sc) + " Run the curl command with your file; the PUT stages the bytes locally (status: staged) — durability on Sia happens in the background or via the vault_flush tool.",
				}, nil
			default: // TransportOpenAI
				if src.Mode != transfer.SourceURL && src.Mode != transfer.SourceData {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the OpenAI tunnel transport", src.Mode)
				}
				if relayFn == nil {
					return model.ToolResult{}, errors.New("vault relay write is not configured")
				}
				res := &transfer.SourceResolver{Transport: transfer.TransportOpenAI, RelayAllowedHosts: relayHosts, RelayMaxBytes: ieo.EffectiveRelayMaxBytes(maxRelayBytes)}
				body, size, _, oerr := res.OpenBytes(ctx, src)
				if oerr != nil {
					return model.ToolResult{}, oerr
				}
				defer body.Close()
				writeCtx, cancel := context.WithTimeout(ctx, transfer.SyncUploadBudget(size))
				defer cancel()
				result, err := relayFn(writeCtx, body, size, in.VaultPath, metadata)
				return toolargs.WrapResult(result, err, "Stored in the vault.")
			}
		},
	}
}

// vaultPutFileSchema compiles the vault_put_file input schema from the tool's
// feature set (see upload_file's uploadFileSchema for the shared model: the
// source.mode enum and prose follow the host features, and `file` is present
// only when FeatFileHostInput).
func vaultPutFileSchema(features hostenv.FeatureSet) json.RawMessage {
	return toolforge.Schema().
		Property("source", toolargs.SchemaFor[transfer.UploadSource](), toolforge.Transform(transfer.VaultSourceSchemaTransform)).
		Property("file", toolargs.SchemaFor[transfer.ChatGPTFileInput](), toolforge.When(hostenv.FeatFileHostInput)).
		StringProperty("vault_path", "Vault destination file path (e.g. vault:/docs/f.pdf or vault:/uploads/report.pdf). Required. Must be a file path, not a directory; traversal (.. or .) segments are rejected. Any vault file path is allowed.").
		StringProperty("profile", "Vault profile name to write into. Required when more than one profile is unlocked (omitting it returns profile_required and mints nothing); on a single-profile server it defaults to the active profile. Specify a different profile to store in another vault without changing the default.").
		// archive_mode is only meaningful for source.mode=path (co-located
		// stdio): the mint and url/data branches stream raw bytes with no
		// in-band archive contract. Declaring it solely for FeatSourcePath
		// keeps its "source mode path" prose off a mint-only host (e.g. Grok),
		// where the dead transport name would reactivate the wrong source.
		StringProperty("archive_mode", "How to treat an archive path ('convert' extracts the archive contents, 'preserve' keeps the archive intact as a single file).", toolforge.Enum("convert", "preserve"), toolforge.When(hostenv.FeatSourcePath)).
		StringProperty("ttl", "Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint.").
		StringProperty("agent", "Identifier for the creating agent (e.g. an orchestrator name). Stored as vault metadata — never as a tag.").
		Property("metadata", &jsonschema.Schema{
			Type:        "object",
			Description: "Caller-supplied metadata key-value pairs (e.g. kind, project, role). Reserved keys (src, host, profile) are auto-stamped and cannot be overridden.",
			AdditionalProperties: &jsonschema.Schema{
				Description: "Arbitrary caller-supplied metadata value.",
			},
		}).
		Property("tags", &jsonschema.Schema{
			Type:        "array",
			Description: "Tags to apply to the file at write time. Durable: sealed into the object metadata and the local tag index so they sync to every device. Normalized (lowercased, deduped). Eliminates the need for a separate vault_tag_add call.",
			Items: &jsonschema.Schema{
				Type: "string",
			},
		}).
		Required("vault_path").
		RawJSON(features)
}

// vaultPutFileDescription resolves the tool description from the forge's
// feature-keyed targets. The transport determines which features the
// platform has, and the forge picks the most specific matching target.
func vaultPutFileDescription(t transfer.TransportKind) string {
	profile := hostenv.ProfileForTransport(t).CloneFeatures()
	desc, ok := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, profile)
	if !ok {
		zap.L().Fatal("vaultPutFileDescription: no matching target for transport", zap.String("transport", string(t)))
	}
	return desc
}
