package mcp

import (
	"context"
	"strings"
)

// This file exposes the vault seed create/restore out-of-band hand-offs as
// first-class, resumable flows for agents, mirroring the SSO pattern
// (auth_sso.go): a start tool returns a needs_human seed_url / restore_url
// hand-off PLUS a resume handle, and two named *_resume tools —
// pinner_vault_create_resume and pinner_vault_restore_resume — poll the
// coordinator until the human has completed the browser action.
//
// Exactly like SSO, only the SURFACE is per-domain (the two named *_resume
// tools); the dispatch machinery is the shared NewResumeTool template and the
// HandoffRegistry. The plaintext recovery seed still never transits the
// MCP/LLM channel: it travels human-browser-to-host only, and the resume
// tools only report pending vs. done for the token the coordinator minted.
//
// The single-shot hand-off (seed_url / restore_url returned directly to the
// agent in the invoke path) is preserved unchanged; this layer ADDS a
// resume handle + poll path on top of it. If the resume machinery is not
// wired (nil registry / handles), no handle is minted and the flow reduces to
// the original single-shot behavior.

const (
	// vaultCreateResumeToolName is the resume tool for a vault create
	// hand-off (seed drop). Its name is per-domain so an agent can
	// pattern-match "this is the vault CREATE flow" and steer a restart to
	// the matching start tool pinner_vault_create.
	vaultCreateResumeToolName = "pinner_vault_create_resume"
	// vaultRestoreResumeToolName is the resume tool for a vault restore
	// hand-off. Per-domain naming so an agent can tell create-resume from
	// restore-resume and steer a restart to pinner_vault_restore.
	vaultRestoreResumeToolName = "pinner_vault_restore_resume"

	// handleDataToken is the key under which the one-time coordinator token is
	// stored in the async handle data so a continuation can poll the
	// coordinator. Only the token (never the mnemonic) is stored here.
	handleDataToken = "token"
)

// vaultTokenFromURL extracts the one-time token from a minted hand-off URL
// (shaped <base>/<prefix>/<token> in both HTTP and loopback modes). It is a
// best-effort parse; an empty token simply means the coordinator did not mint
// a resumable URL and no continuation is registered.
func vaultTokenFromURL(url string) string {
	p := strings.LastIndex(url, "/")
	if p < 0 || p == len(url)-1 {
		return ""
	}
	return url[p+1:]
}

// tokenDone reports whether the coordinator token is no longer pending, i.e.
// the human has consumed it: the one-time seed was viewed (create) or the
// restore form was submitted (restore). The shared handoffEndpoint core
// returns a nil item for a spent or expired token, which is the terminal
// state of both hand-offs. This is a pure state check; it never touches the
// seed mnemonic.
func (s *SeedDrop) tokenDone(token string) bool {
	_, ok := s.core.get(token)
	return !ok
}

func (o *OOBRestore) tokenDone(token string) bool {
	_, ok := o.core.get(token)
	return !ok
}

// vaultCreateResumeContinuation returns the vault-create-specific poll logic:
// it reports pending (needs_human) until the human has picked up the seed from
// the one-time seed_url, then a terminal done result. It is registered against
// the handle by mintVaultHandoff so the shared pinner_vault_create_resume
// template dispatches to it. The continuation performs its own registry/
// handle cleanup on every terminal outcome.
func vaultCreateResumeContinuation(db *SeedDrop, handles *AsyncHandleStore, reg *HandoffRegistry) ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (ToolResult, error) {
		token, _ := data[handleDataToken].(string)
		if db == nil || token == "" || db.tokenDone(token) {
			// Seed picked up (or the drop is gone/absent) — hand-off over.
			handles.Delete(handle)
			reg.End(handle)
			return ToolResult{
				Text:              "Vault create hand-off complete: the recovery seed has been retrieved.",
				StructuredContent: map[string]any{"status": StatusDone, "handle": handle},
			}, nil
		}
		return NeedsHumanResult(NeedsHuman{
			Reason:     ReasonCredentialEntry,
			Handle:     handle,
			ResumeTool: vaultCreateResumeToolName,
			Detail:     "Ask the user to open the seed_url in a browser and retrieve the recovery seed. Then call pinner_vault_create_resume with the handle.",
		}), nil
	}
}

// vaultRestoreResumeContinuation returns the vault-restore-specific poll logic:
// it reports pending (needs_human) until the human has submitted the recovery
// seed on the one-time restore page, then a terminal done result. It is
// registered against the handle by mintVaultHandoff so the shared
// pinner_vault_restore_resume template dispatches to it. It is a pure
// coordinator-state poll (the token going spent) — the RestoreRunner only runs
// in the browser POST handler, never on this channel — so the seed never
// reaches the agent.
func vaultRestoreResumeContinuation(oob *OOBRestore, handles *AsyncHandleStore, reg *HandoffRegistry) ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (ToolResult, error) {
		token, _ := data[handleDataToken].(string)
		if oob == nil || token == "" || oob.tokenDone(token) {
			// Restore form submitted (or token gone/absent) — hand-off over.
			handles.Delete(handle)
			reg.End(handle)
			return ToolResult{
				Text:              "Vault restore hand-off complete: the vault has been restored.",
				StructuredContent: map[string]any{"status": StatusDone, "handle": handle},
			}, nil
		}
		return NeedsHumanResult(NeedsHuman{
			Reason:     ReasonCredentialEntry,
			Handle:     handle,
			ResumeTool: vaultRestoreResumeToolName,
			Detail:     "Ask the user to open the restore_url in a browser and enter the recovery seed to complete the restore. Then call pinner_vault_restore_resume with the handle.",
		}), nil
	}
}

// NewVaultCreateResumeDescriptor returns the pinner_vault_create_resume tool,
// built from the shared resume template. Name/description and restart steering
// are create-flavored: a dead handle steers back to pinner_vault_create.
func NewVaultCreateResumeDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                vaultCreateResumeToolName,
		Description:         "Poll a pending vault create (seed drop) hand-off to check whether the human has retrieved the recovery seed from the one-time seed_url. Returns pending (needs_human) until the seed has been retrieved, then reports done. Pass the handle returned by pinner_vault_create.",
		RestartTool:         vaultCreateToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault create with pinner_vault_create",
		ExpiredHandleDetail: "the vault create hand-off expired before the seed was retrieved; start a fresh vault create with pinner_vault_create so a new seed_url is minted",
		DeadHandleReason:    ReasonCredentialEntry,
	}, reg, handles)
}

// NewVaultRestoreResumeDescriptor returns the pinner_vault_restore_resume tool,
// built from the shared resume template. Name/description and restart steering
// are restore-flavored: a dead handle steers back to pinner_vault_restore.
func NewVaultRestoreResumeDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                vaultRestoreResumeToolName,
		Description:         "Poll a pending vault restore hand-off to check whether the human has completed the out-of-band restore on the one-time restore_url. Returns pending (needs_human) until the restore is done, then reports done. Pass the handle returned by pinner_vault_restore.",
		RestartTool:         vaultRestoreToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault restore with pinner_vault_restore",
		ExpiredHandleDetail: "the vault restore hand-off expired before the human completed it; start a fresh vault restore with pinner_vault_restore so a new restore_url is minted",
		DeadHandleReason:    ReasonCredentialEntry,
	}, reg, handles)
}

// mintVaultHandoff is the invoke-path bridge that turns a raw seed_url /
// restore_url hand-off into a resumable one. Given the tool entry that just
// produced a hand-off, the minted seed_url (from attachSeedDrop) and
// restore_url (from attachRestoreURL), plus the resume machinery, it mints a
// fresh async handle, registers the per-domain continuation against it, and
// returns the handle + resume tool name to embed in the structured content.
//
// Only the coordinator's one-time TOKEN is stored on the handle and re-polled
// by the continuation — never the mnemonic. If there is nothing to resume (no
// hand-off minted, coordinators absent, or resume machinery not wired) it
// returns empty strings so the invoke path degrades to the original
// single-shot hand-off unchanged.
func mintVaultHandoff(entry *ToolEntry, seedURL, restoreURL string, seedDrop *SeedDrop, oobRestore *OOBRestore, handoffReg *HandoffRegistry, authHandles *AsyncHandleStore) (handle, resumeTool string) {
	if handoffReg == nil || authHandles == nil || entry == nil {
		return "", ""
	}
	// Restore hand-off: attachRestoreURL mints restore_url directly.
	if entry.Behavior.RestoreURL != nil && restoreURL != "" {
		token := vaultTokenFromURL(restoreURL)
		handle := authHandles.Create("pending", map[string]any{handleDataToken: token})
		handoffReg.Begin(handle, vaultRestoreResumeContinuation(oobRestore, authHandles, handoffReg))
		return handle, vaultRestoreResumeToolName
	}
	// Seed-drop hand-off (vault create): attachSeedDrop mints seed_url into extra.
	if entry.Behavior.SeedDrop != nil && seedURL != "" {
		token := vaultTokenFromURL(seedURL)
		handle := authHandles.Create("pending", map[string]any{handleDataToken: token})
		handoffReg.Begin(handle, vaultCreateResumeContinuation(seedDrop, authHandles, handoffReg))
		return handle, vaultCreateResumeToolName
	}
	return "", ""
}
