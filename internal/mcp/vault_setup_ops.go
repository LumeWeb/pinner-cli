package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// This file turns pinner_vault_create and pinner_vault_restore into clean,
// CLI-free out-of-band hand-offs by routing them through the catalog
// operations (catalogops.vault.create / vault.restore) that drive the core
// vault.Provisioner, and then having the MCP layer mint the one-time OOB URL
// and the resume handle.
//
// Instead of running the CLI command in-process and parsing its JSON stdout to
// find a seed path or profile, the handler invokes the catalog operation,
// receives typed handoff data, and builds the OOB hand-off directly.
//
// The plaintext recovery mnemonic is never placed on the MCP ToolResult Text
// or StructuredContent. On create, the vault is created + activated out-of-band
// through the OOBCreate coordinator (fresh seed + Sia approval + registration)
// and the freshly generated seed is delivered through the shared SeedDrop's
// one-time seed_url. The restore seed never crosses the agent channel at all:
// it is entered by the human on the one-time /restore/<token> page and consumed
// by the OOBRestore coordinator's RestoreRunner.

// vaultSetupOps returns the create/restore catalog operations wired to the
// default core Provisioner. The getter preserves the lazy-deps pattern: a
// fresh Provisioner is built per invocation so any test override of the
// underlying core stays live.
func vaultSetupOps() (create, restore catalog.Operation) {
	deps := catalogops.VaultDeps{
		Provisioner: func() *vault.Provisioner { return vault.NewProvisioner() },
	}
	for _, op := range catalogops.VaultSetupOperations(deps) {
		switch op.Name() {
		case "vault.create":
			create = op
		case "vault.restore":
			restore = op
		}
	}
	return create, restore
}

// vaultHandoffResult builds a needs_human ToolResult that preserves the vault
// OOB contract keys (create_url / restore_url), distinct from the generic
// action_url used by SSO. The urlKey is "create_url" for create and
// "restore_url" for restore. The plaintext mnemonic never appears in either
// the Text or StructuredContent; only the one-time URL, handle and resume tool.
func vaultHandoffResult(resumeTool, urlKey, url, handle, detail string) ToolResult {
	sc := map[string]any{
		"status": StatusNeedsHuman,
		"reason": ReasonCredentialEntry,
	}
	if url != "" {
		sc[urlKey] = url
	}
	if handle != "" {
		sc["handle"] = handle
	}
	if resumeTool != "" {
		sc["resume_tool"] = resumeTool
	}
	sc["detail"] = detail
	return ToolResult{
		Text:              "needs_human: " + string(ReasonCredentialEntry) + " - " + detail,
		StructuredContent: sc,
	}
}

// vaultCreateSetupHandler builds the PinnerToolHandler for pinner_vault_create.
// It runs the vault.create catalog operation (provisioning a fresh vault that
// SSO-activates like restore), then mints a one-time create_url (OOBCreate.Register)
// and a resume handle whose continuation polls that OOB create, returning a
// needs_human hand-off with create_url + handle + resume_tool. The freshly
// generated seed is only ever delivered to the human through the OOB seeddrop
// after the create activates; it never enters the result.
//
// When the OOB create coordinator is absent, the handler returns a structured
// not-configured hand-off rather than hanging.
func vaultCreateSetupHandler(oobCreate *OOBCreate, reg *HandoffRegistry, handles *AsyncHandleStore) PinnerToolHandler {
	return func(ctx context.Context, req ToolRequest) (ToolResult, error) {
		if reg == nil || handles == nil || oobCreate == nil {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonInteractiveOnly,
				ResumeTool: vaultCreateResumeToolName,
				Detail:     "Vault create is not configured for this server; the out-of-band create hand-off is unavailable.",
			}), nil
		}
		op, _ := vaultSetupOps()
		if op == nil {
			return ToolResult{IsError: true, Text: "vault create: catalog operation unavailable"}, nil
		}
		// Route through the catalog's normalization (camelCase aliasing,
		// coercion, defaults) before the op handler runs, mirroring the
		// Catalog.Invoke path so a model sending e.g. camelCase "deviceName"
		// for kebab "device-name" is not silently dropped.
		normalized, err := catalog.NormalizeOperationInput(op, req.Arguments)
		if err != nil {
			return ToolResult{IsError: true, Text: "vault create: " + err.Error()}, nil
		}
		result, err := op.Handler().Execute(ctx, normalized)
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		handoff, ok := result.(*catalogops.VaultCreateHandoff)
		if !ok {
			return ToolResult{IsError: true, Text: "vault create: unexpected result"}, nil
		}
		// Mint the one-time OOB create URL for the requested profile. The create
		// page runs the SSO approval + activation in the browser and, on success,
		// delivers the freshly generated seed via a one-time seeddrop link.
		createURL := oobCreate.Register(handoff.Profile)
		token := vaultTokenFromURL(createURL)
		handle := handles.Create("pending", map[string]any{handleDataToken: token})
		reg.Begin(handle, vaultCreateResumeContinuation(oobCreate, handles, reg))
		return vaultHandoffResult(vaultCreateResumeToolName, "create_url", createURL, handle,
			"Ask the user to open create_url in a browser, approve the Sia device connection, then retrieve the one-time recovery seed. Then call pinner_vault_create_resume with the handle. The seed never crosses the MCP channel."), nil
	}
}

// vaultRestoreSetupHandler builds the PinnerToolHandler for pinner_vault_restore.
// It runs the vault.restore catalog operation (resolving the target profile),
// then mints a one-time restore_url (OOBRestore.Register) and a resume handle
// whose continuation polls that OOB restore, returning a needs_human hand-off
// with restore_url + handle + resume_tool. The seed is only ever entered by the
// human on the browser page; it never transits the agent channel.
//
// When the OOB restore coordinator is absent, the handler returns a structured
// not-configured hand-off rather than hanging.
func vaultRestoreSetupHandler(oobRestore *OOBRestore, reg *HandoffRegistry, handles *AsyncHandleStore) PinnerToolHandler {
	return func(ctx context.Context, req ToolRequest) (ToolResult, error) {
		if reg == nil || handles == nil || oobRestore == nil {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonInteractiveOnly,
				ResumeTool: vaultRestoreResumeToolName,
				Detail:     "Vault restore is not configured for this server; the out-of-band restore hand-off is unavailable.",
			}), nil
		}
		_, op := vaultSetupOps()
		if op == nil {
			return ToolResult{IsError: true, Text: "vault restore: catalog operation unavailable"}, nil
		}
		// Route through the catalog's normalization (aliasing, coercion,
		// defaults) before the op handler runs, mirroring Catalog.Invoke.
		normalized, err := catalog.NormalizeOperationInput(op, req.Arguments)
		if err != nil {
			return ToolResult{IsError: true, Text: "vault restore: " + err.Error()}, nil
		}
		result, err := op.Handler().Execute(ctx, normalized)
		if err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
		handoff, ok := result.(*catalogops.VaultRestoreHandoff)
		if !ok {
			return ToolResult{IsError: true, Text: "vault restore: unexpected result"}, nil
		}
		// Mint the one-time OOB restore URL for the resolved profile.
		restoreURL := oobRestore.Register(handoff.Profile)
		token := vaultTokenFromURL(restoreURL)
		handle := handles.Create("pending", map[string]any{handleDataToken: token})
		reg.Begin(handle, vaultRestoreResumeContinuation(oobRestore, handles, reg))
		return vaultHandoffResult(vaultRestoreResumeToolName, "restore_url", restoreURL, handle,
			"Ask the user to open restore_url in a browser and enter the recovery seed to complete the restore. Then call pinner_vault_restore_resume with the handle. The seed never crosses the MCP channel."), nil
	}
}
