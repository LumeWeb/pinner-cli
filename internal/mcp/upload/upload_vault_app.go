package upload

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
	"go.lumeweb.com/pinner/canvas"
	pinnertransfer "go.lumeweb.com/pinner/transfer"
)

// This file wires the "Upload to Vault" MCP App onto the shared AppView lib
// layer. It pairs the model-facing vault_put_file tool with a ui:// view so a
// UI-capable host renders a file-picker panel. There is no draft MCP
// file-upload yet, so the app does NOT push file bytes through the MCP/LLM
// channel. Instead it mirrors the Upload to IPFS app: a helper mints a
// one-time presigned PUT endpoint bound to the destination vault path, and the
// iframe's Uppy XHR uploader PUTs the raw file body straight to that endpoint
// (formData off, HTTP PUT). Durability/backgrounding semantics follow the
// canonical internal/mcp/mintcontract contract (see
// mintcontract.DurabilitySource/FlushJobShape); authoritative there. The
// normative prose (staging status, durability source, vault_flush job shape,
// polling, no upload_status) is composed from mintcontract fragments; the
// launcher and tool copy never restate them by hand.

// VaultUploadAppURI is the ui:// resource serving the "Upload to Vault" app.
const VaultUploadAppURI = "ui://uploads/vault.html"

// VaultUploadSubmitInput is the typed argument shape for the app-only
// vault_upload_submit helper. It carries the destination path, TTL, and
// destination vault profile; the file bytes themselves never cross the tool
// channel.
type VaultUploadSubmitInput struct {
	// VaultPath carries no jsonschema description tag: this helper's schema
	// is the LIVE, hand-authored InputSchema baked into
	// vaultUploadSubmitDescriptor (single description copy, tag/wire parity
	// by construction).
	VaultPath string `json:"vault_path"`
	// TTL's tag composes schematext.TTLOptional and Profile's tag composes
	// schematext.ProfileWriteExtended (struct tags cannot embed constants;
	// TestSchemaTextFragmentsPinned pins these literals to them).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5m)."`
	// Profile is the vault profile to write into, carried through when the
	// app mints a fresh endpoint (e.g. after a refresh). On a multi-profile
	// server it is required and validated with the same profile_required
	// guard as the launcher and vault_put_file.
	Profile string `json:"profile,omitempty" jsonschema:"description=Vault profile name to write into. Required when more than one profile is unlocked (omitting it returns profile_required and mints nothing); on a single-profile server it defaults to the active profile. Specify a different profile to store in another vault without changing the default."`
}

// renderVaultUploadAppHTML renders the complete "Upload to Vault" app document
// (ui://uploads/vault.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + upload logic) come from
// mcpapp.RenderAppDoc (go.lumeweb.com/pinner/canvas); the body form is authored in templ.
func renderVaultUploadAppHTML() string {
	return mcpapp.RenderAppDoc(canvas.ViewVaultUpload, "Upload to Vault")
}

// vaultUploadSubmitDescriptor builds the app-only mint helper for the Upload
// to Vault view. It is visible to the app only (never the model). It mints a
// one-time presigned PUT endpoint (via the VaultHTTPUpload coordinator) bound
// to the requested vault path and returns the URL for the iframe's Uppy XHR
// upload to PUT the raw bytes into.
func vaultUploadSubmitDescriptor(vu *transfer.VaultHTTPUpload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "vault_upload_submit",
		Title:       "Prepare a vault upload endpoint",
		Description: "Mint a one-time presigned PUT endpoint that writes the uploaded file body into the encrypted vault at the given path. App-only helper for the Upload to Vault view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"vault_path":{"type":"string","description":"Vault destination file path, e.g. vault:/uploads/report.pdf. Required."},"ttl":{"type":"string","description":"` + schematext.TTLOptional + `"},"profile":{"type":"string","description":"` + schematext.ProfileWriteExtended + `"}},"required":["vault_path"]}`),
		// OpenAI tool invocation labels shown by UI-capable hosts while the
		// tool runs and after it finishes. Required alongside the openai
		// outputTemplate this app helper carries.
		Meta: map[string]any{
			"openai/toolInvocation": map[string]any{
				"invoking": "Preparing vault upload endpoint…",
				"invoked":  "Vault upload endpoint ready",
			},
		},
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[VaultUploadSubmitInput](req)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
			}
			// Multi-profile guard, mirroring the launcher and vault_put_file:
			// never silently default to the active profile on a multi-profile
			// server — fail with the structured profile_required error here.
			if gr, failed := vaultProfileGuard(in.Profile); failed {
				return gr, nil
			}
			// ONE canonical presign-TTL parser
			// (go.lumeweb.com/pinner/transfer.ParsePresignTTL): empty/non-
			// positive → the default lifetime, unparseable → the stable
			// `invalid ttl "..."` error — the same parser the
			// open_vault_manager launcher and vault_put_file mint path use.
			ttl, terr := pinnertransfer.ParsePresignTTL(in.TTL)
			if terr != nil {
				return model.ToolResult{}, terr
			}
			// mint validates the destination (file path, inside the uploads
			// scope, no traversal) before minting, refusing to mint a PUT
			// endpoint that could write anywhere else in the vault.
			// Pin the resolved profile identity into the sealed metadata via
			// the ONE canonical assembly (transfer.StampedMCPMetadata) —
			// explicit profile passes through; an empty one resolves to the
			// single unlocked profile at mint time, so the later PUT can
			// never re-resolve against an ambiguous registry. Identical on
			// every mint surface.
			url, err := vu.Mint(ctx, in.VaultPath, ttl, transfer.StampedMCPMetadata(req.Caps, in.Profile, nil))
			if err != nil {
				return model.ToolResult{}, err
			}
			return model.ToolResult{StructuredContent: map[string]any{"url": url, "vault_path": in.VaultPath}, Text: "Upload endpoint prepared."}, nil
		},
	}
}

// RegisterVaultUploadApp wires the complete "Upload to Vault" MCP App: attaches
// the ui:// view to the vault_put_file tool, registers the ui://uploads/vault.html
// HTML resource, and registers the app-only vault_upload_submit mint helper. The
// vault write is provided by the VaultHTTPUpload coordinator (which carries the
// authenticated VaultPutHandler for the actual write).
func RegisterVaultUploadApp(srv *sdk.Server, catalog apps.AppCatalog, vu *transfer.VaultHTTPUpload) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if vu == nil {
		return fmt.Errorf("nil vault upload coordinator")
	}
	return apps.RegisterAppView(srv, catalog, apps.AppView{
		URI:           VaultUploadAppURI,
		Name:          "vault-upload",
		Title:         "Upload to Vault",
		Description:   "Pick a file and store it in your encrypted Pinner vault.",
		HTML:          renderVaultUploadAppHTML(),
		PrefersBorder: true,
		// Advertise the presigned vault-upload origin in the app's CSP
		// connectDomains so an MCP host permits the sandbox iframe to
		// PUT file bytes to it. Resolved dynamically because the origin (the
		// tunnel/base URL or loopback address) is only known after the server
		// and transport are up — after app registration.
		ConnectDomainsFunc: vu.ConnectOrigins,
		// Attach the UI view to the open_vault_manager LAUNCHER — not the
		// headless vault_put_file primitive.
		AttachTo: []string{OpenVaultManagerToolName},
		Helpers:  []model.ToolDescriptor{vaultUploadSubmitDescriptor(vu)},
	})
}
