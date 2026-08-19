package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// This file wires the "transfer.Upload to Vault" MCP App onto the shared AppView lib
// layer. It pairs the model-facing vault_put_file tool with a ui:// view so a
// UI-capable host renders a file-picker panel. There is no draft MCP
// file-upload yet, so the app does NOT push file bytes through the MCP/LLM
// channel. Instead it mirrors the transfer.Upload to IPFS app: a helper mints a
// one-time presigned PUT endpoint bound to the destination vault path, and the
// iframe's Uppy XHR uploader PUTs the raw file body straight to that endpoint
// (formData off, HTTP PUT). The vault write is synchronous, so the PUT
// response itself carries the vault result - no async handle or poll round
// trip is needed.

// VaultUploadAppURI is the ui:// resource serving the "transfer.Upload to Vault" app.
const VaultUploadAppURI = "ui://uploads/vault.html"

// VaultUploadSubmitInput is the typed argument shape for the app-only
// vault_upload_submit helper. It carries only the destination path and TTL;
// the file bytes themselves never cross the tool channel.
type VaultUploadSubmitInput struct {
	VaultPath string `json:"vault_path" jsonschema:"description=Vault destination path, e.g. vault:/uploads/report.pdf. Required."`
	TTL       string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5m)."`
}

// renderVaultUploadAppHTML renders the complete "transfer.Upload to Vault" app document
// (ui://uploads/vault.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + upload logic) come from
// mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ.
func renderVaultUploadAppHTML() string {
	return mcpapp.RenderMcpAppDoc("transfer.Upload to Vault", mcpapp.VaultUploadAppForm(), mcpapp.AppModule("vault-upload"))
}

// vaultUploadSubmitDescriptor builds the app-only mint helper for the transfer.Upload
// to Vault view. It is visible to the app only (never the model). It mints a
// one-time presigned PUT endpoint (via the transfer.VaultHTTPUpload coordinator) bound
// to the requested vault path and returns the URL for the iframe's Uppy XHR
// upload to PUT the raw bytes into.
func vaultUploadSubmitDescriptor(vu *transfer.VaultHTTPUpload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "vault_upload_submit",
		Title:       "Prepare a vault upload endpoint",
		Description: "Mint a one-time presigned PUT endpoint that writes the uploaded file body into the encrypted vault at the given path. App-only helper for the transfer.Upload to Vault view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"vault_path":{"type":"string"},"ttl":{"type":"string"}},"required":["vault_path"]}`),
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[VaultUploadSubmitInput](req)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
			}
			var ttl time.Duration
			if in.TTL != "" {
				ttl, err = time.ParseDuration(in.TTL)
				if err != nil {
					return model.ToolResult{}, fmt.Errorf("invalid ttl: %w", err)
				}
			}
			// mint validates the destination (file path, inside the uploads
			// scope, no traversal) before minting, refusing to mint a PUT
			// endpoint that could write anywhere else in the vault.
			url, err := vu.Mint(in.VaultPath, ttl)
			if err != nil {
				return model.ToolResult{}, err
			}
			return model.ToolResult{StructuredContent: map[string]any{"url": url, "vault_path": in.VaultPath}, Text: "transfer.Upload endpoint prepared."}, nil
		},
	}
}

// RegisterVaultUploadApp wires the complete "transfer.Upload to Vault" MCP App: attaches
// the ui:// view to the vault_put_file tool, registers the ui://uploads/vault.html
// HTML resource, and registers the app-only vault_upload_submit mint helper. The
// vault write is provided by the transfer.VaultHTTPUpload coordinator (which carries the
// authenticated VaultPutHandler for the actual write).
func RegisterVaultUploadApp(srv *sdk.Server, catalog *ToolCatalog, vu *transfer.VaultHTTPUpload) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if vu == nil {
		return fmt.Errorf("nil vault upload coordinator")
	}
	return RegisterAppView(srv, catalog, AppView{
		URI:           VaultUploadAppURI,
		Name:          "vault-upload",
		Title:         "transfer.Upload to Vault",
		Description:   "Pick a file and store it in your encrypted Pinner vault.",
		HTML:          renderVaultUploadAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"vault_put_file"},
		Helpers:       []model.ToolDescriptor{vaultUploadSubmitDescriptor(vu)},
	})
}
