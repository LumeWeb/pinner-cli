package upload

import (
	"context"
	"fmt"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	corevault "go.lumeweb.com/pinner/core/vault"
)

// OpenVaultManagerURI is the ui:// resource URI served by the Upload to Vault
// app. The launcher's tool _meta.ui references it.
const OpenVaultManagerURI = VaultUploadAppURI

// OpenVaultManagerToolName is the model-facing open_* launcher for the Upload
// to Vault app. It is the ONLY tool carrying ui.resourceUri for this view; the
// headless vault_put_file primitive never advertises a card.
const OpenVaultManagerToolName = "open_vault_manager"

// openVaultManagerDescription is shared between the static Description and the
// Fallback MCPTarget so the launcher descriptor carries a target list. It is
// composed from the shared launcher skeleton
// (apps.OpenLauncherDescriptionBody) so the scaffold wording cannot drift from
// the other open_* launchers; its mint-durability body composes the shared
// dependency-neutral canon (internal/mcp/mintcontract) so it cannot drift
// from the tool descriptions.
var openVaultManagerDescription = apps.OpenLauncherDescriptionBody("Upload to Vault file picker", "pick a file",
	"It returns a presigned PUT URL plus the vault_path; the iframe's Uppy uploader POSTs file bytes to that URL directly, and "+mintcontract.StagedWrite+" — "+mintcontract.DurabilitySource+".",
	"vault_put_file for autonomous uploads without a rendered file picker")

// OpenVaultManagerInput is the typed argument shape for the model-facing
// Vault upload launcher. Durability/backgrounding semantics follow the
// canonical mint contract (see mintcontract.DurabilitySource/FlushJobShape);
// authoritative there.
type OpenVaultManagerInput struct {
	VaultPath string `json:"vault_path" jsonschema:"description=Vault destination path, e.g. vault:/uploads/report.pdf. Required."`
	// TTL's tag composes schematext.TTLOptional and Profile's tag composes
	// schematext.ProfileWriteExtended (struct tags cannot embed constants;
	// TestSchemaTextFragmentsPinned pins these literals to them).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5m)."`
	// Profile is the vault profile the picked file is written into. On a
	// multi-profile server it is REQUIRED (omitting it returns a structured
	// profile_required error listing the unlocked profiles, mirroring
	// vault_put_file); on a single-profile server it defaults to the active
	// profile.
	Profile string `json:"profile,omitempty" jsonschema:"description=Vault profile name to write into. Required when more than one profile is unlocked (omitting it returns profile_required and mints nothing); on a single-profile server it defaults to the active profile. Specify a different profile to store in another vault without changing the default."`
}

// Multi-profile guard shared by every vault-mint surface in this package:
// the launcher and the app-only mint helper use the registry-backed
// corevault.ProfileRequired exactly like vault_put_file, so all surfaces
// agree that a missing profile on a multi-profile server is a structured
// profile_required error, never a silent default.
// vaultProfileRequired is the registry-backed guard (corevault.ProfileRequired)
// at production; tests may swap it for a deterministic stub.
var vaultProfileRequired = corevault.ProfileRequired

// SetVaultProfileRequiredForTest swaps the vault profile guard (exercised by
// cross-package hermetic tests) and returns the previous guard so the caller
// can restore it. Production retains the registry-backed corevault default.
func SetVaultProfileRequiredForTest(fn func(string) *corevault.ProfileRequiredError) func(string) *corevault.ProfileRequiredError {
	prev := vaultProfileRequired
	vaultProfileRequired = fn
	return prev
}

func vaultProfileGuard(profile string) (model.ToolResult, bool) {
	if pr := vaultProfileRequired(profile); pr != nil {
		return model.ErrorResult(pr.Code, pr.Message, map[string]any{
			"profiles": pr.Profiles,
		}), true
	}
	return model.ToolResult{}, false
}

// NewOpenVaultManagerDescriptor builds the model-facing Upload to Vault
// launcher tool. It is the ONLY tool that carries _meta.ui.resourceUri for
// this particular Vault App — vault_put_file itself is headless.
//
// This is a UI launcher: it renders an iframe for a human to pick a file. It
// returns the presigned PUT URL the iframe's Uppy uploader writes to, plus
// the vault_path. Durability/backgrounding semantics follow the canonical
// mint contract (see mintcontract.DurabilitySource/FlushJobShape);
// authoritative there.
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
			// Multi-profile guard: a mint without an explicit profile on a
			// server with more than one unlocked profile fails HERE with the
			// structured profile_required error, not later at the PUT.
			if gr, failed := vaultProfileGuard(in.Profile); failed {
				return gr, nil
			}
			// ONE shared presign-TTL parser (core/transfer.ParsePresignTTL) —
			// the launcher must accept exactly the TTL format the upload_file
			// mint path and the app helpers accept.
			ttl, terr := transfer.ParsePresignTTL(in.TTL)
			if terr != nil {
				return model.ToolResult{}, terr
			}
			// Pin the resolved profile identity into the minted metadata via
			// the ONE canonical assembly (transfer.StampedMCPMetadata):
			// host-type lifting from the request caps, ResolveMintProfile
			// pinning (an explicit profile passes through; an EMPTY one
			// resolves to the single unlocked profile HERE, so the sealed
			// metadata names the destination and a second profile unlocking
			// before the browser PUT can never re-resolve the write against
			// an ambiguous registry), and the corevault.StampedMetadata
			// stamp. Identical on every mint surface.
			url, err := vu.Mint(ctx, in.VaultPath, ttl, transfer.StampedMCPMetadata(request.Caps, in.Profile, nil))
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
				Text:              toolargs.ResultJSONText(sc) + " The Upload to Vault UI is open; pick a file to PUT. " + mintcontract.FirstUpper(mintcontract.StagedWrite) + " — " + mintcontract.DurabilitySource + ".",
			}, nil
		},
	}
}
