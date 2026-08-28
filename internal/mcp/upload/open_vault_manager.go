package upload

import (
	"context"
	"fmt"
	"time"

	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// OpenVaultManagerURI is the ui:// resource URI served by the Upload to Vault
// app. The launcher's tool _meta.ui references it.
const OpenVaultManagerURI = VaultUploadAppURI

// OpenVaultManagerToolName is the model-facing open_* launcher for the Upload
// to Vault app. It is the ONLY tool carrying ui.resourceUri for this view; the
// headless vault_put_file primitive never advertises a card.
const OpenVaultManagerToolName = "open_vault_manager"

// openVaultManagerDescription is shared between the static Description and the
// Fallback MCPTarget so the launcher descriptor carries a target list.
const openVaultManagerDescription = "Open the interactive Upload to Vault file picker. This is a UI launcher: it renders an HTML iframe so the user can pick a file. It is not a headless primitive. " +
	"It returns a presigned PUT URL plus the vault_path; the iframe's Uppy uploader POSTs file bytes to that URL directly, and the vault write is synchronous (the PUT response is the result). " +
	"Prefer vault_put_file (headless) for autonomous workflows; call this only when a human file picker is actually desired."

// OpenVaultManagerInput is the typed argument shape for the model-facing
// Vault upload launcher. Vault uploads are synchronous: bytes land in the
// vault when the iframe's Uppy XHR PUT completes (no async poll loop).
type OpenVaultManagerInput struct {
	VaultPath string `json:"vault_path" jsonschema:"description=Vault destination path, e.g. vault:/uploads/report.pdf. Required."`
	TTL       string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5 minutes)."`
}

// NewOpenVaultManagerDescriptor builds the model-facing Upload to Vault
// launcher tool. It is the ONLY tool that carries _meta.ui.resourceUri for
// this particular Vault App — vault_put_file itself is headless.
//
// This is a UI launcher: it renders an HTML iframe so the user can pick a
// file. It returns the presigned PUT URL the iframe's Uppy uploader writes
// to, plus the vault_path. The vault write is synchronous, so the iframe
// PUT response carries the result directly.
func NewOpenVaultManagerDescriptor(vu *transfer.VaultHTTPUpload) model.ToolDescriptor {
	appMeta, _ := sdk.MarshalToolMeta(model.AppToolMeta{
		ResourceURI: OpenVaultManagerURI,
		Visibility:  []model.ToolVisibility{model.ToolVisibilityModel, model.ToolVisibilityApp},
	})
	return model.ToolDescriptor{
		Name:        "open_vault_manager",
		Title:       "Open Upload to Vault App",
		Description: openVaultManagerDescription,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(openVaultManagerDescription)),
		InputSchema: toolargs.ToolSchemaFor[OpenVaultManagerInput](),
		Meta:        appMeta,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[OpenVaultManagerInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
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
			var hostType string
			if request.Caps != nil && request.Caps.Profile != nil {
				hostType = string(request.Caps.Profile.HostType)
			}
			url, err := vu.Mint(in.VaultPath, ttl, corevault.StampedMetadata("mcp", hostType, "", nil))
			if err != nil {
				return model.ToolResult{}, err
			}
			sc := map[string]any{
				"presigned_url": url,
				"vault_path":    in.VaultPath,
				"ttl":           ttl.String(),
			}
			return model.ToolResult{
				StructuredContent: sc,
				Text:              toolargs.ResultJSONText(sc) + " The Upload to Vault UI is open; pick a file to PUT. The vault write is synchronous on the PUT.",
			}, nil
		},
	}
}
