package mcp

import (
	"context"
	"strings"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// This file exposes the vault seed create/restore out-of-band hand-offs as
// first-class, resumable flows for agents, following the SSO pattern
// (auth_sso.go): a start tool returns a needs_human create_url / restore_url
// hand-off plus a resume handle, and two named *_resume tools,
// vault_create_resume and vault_restore_resume, poll the
// coordinator until the human has completed the browser action.
//
// Only the surface is per-domain (the two named *_resume tools); the dispatch
// machinery is the shared handoff.NewResumeTool template and the handoff.HandoffRegistry. The
// plaintext recovery seed never transits the MCP/LLM channel: it travels
// human-browser-to-host only, and the resume tools only report pending vs.
// done for the token the coordinator minted.
//
// The single-shot hand-off (create_url / restore_url returned directly to the
// agent in the invoke path) is preserved unchanged; this layer adds a resume
// handle + poll path on top of it. If the resume machinery is not wired (nil
// registry / handles), no handle is minted and the flow reduces to the
// original single-shot behavior.

const (
	// vaultCreateResumeToolName is the resume tool for a vault create
	// hand-off. Its name is per-domain so an agent can
	// pattern-match "this is the vault CREATE flow" and steer a restart to
	// the matching start tool vault_create
	vaultCreateResumeToolName = "vault_create_resume"
	// vaultRestoreResumeToolName is the resume tool for a vault restore
	// hand-off. Per-domain naming so an agent can tell create-resume from
	// restore-resume and steer a restart to vault_restore.
	vaultRestoreResumeToolName = "vault_restore_resume"

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

// tokenDone reports the coordinator token's current state by resolving it
// against the shared handoffEndpoint core, which distinguishes:
//
//	handoffUsed      -> the human consumed the one-time link (done)
//	handoffExpired   -> the TTL elapsed before use (dead -> restart)
//	live item        -> still pending, the human has not acted yet
//	absent           -> never existed, or its spent tombstone was evicted by
//	                    pruneSpentLocked at maxSpentTombstones, so it can never
//	                    transition on its own (dead -> restart)
//
// A token that no longer resolves must NOT be reported as either done or
// pending forever; the caller routes both of those to a terminal steer. This
// is a pure state check; it never touches the seed mnemonic.
func (s *SeedDrop) tokenDone(token string) (done, expired, pending bool) {
	if token == "" {
		return false, false, false
	}
	item, reason := s.core.Resolve(token)
	return reason == handoff.ReasonUsed, reason == handoff.ReasonExpired, item != nil
}

// tokenDone reports the restore coordinator token's current state. Unlike a
// generic spent check, it distinguishes a succeeded restore (done) from a
// failed one (failed) by consulting the per-token outcome recorded when the
// browser POST ran RunRestore. This prevents the resume continuation from
// reporting "the vault has been restored" when RunRestore actually failed (a
// wrong mnemonic or Sia approval/registration error).
//
//	succeeded outcome -> done
//	failed outcome     -> failed (steer, never done)
//	live item          -> pending (the human has not submitted the form yet)
//	handoffExpired     -> expired
//	no outcome + spent -> absent/evicted (dead -> steer)
func (o *OOBRestore) tokenDone(token string) (done, failed, expired, pending bool) {
	if token == "" {
		return false, false, false, false
	}
	item, reason := o.core.Resolve(token)
	if item != nil {
		return false, false, false, true // still live: nothing submitted
	}
	if reason == handoff.ReasonExpired {
		return false, false, true, false
	}
	o.mu.Lock()
	out, ok := o.outcomes[token]
	var succeeded bool
	var errText string
	if ok {
		succeeded, errText = out.succeeded, out.err
	}
	o.mu.Unlock()
	if ok && succeeded {
		return true, false, false, false
	}
	if ok && errText != "" {
		// Restore settled as failed: surface it, never report done.
		return false, true, false, false
	}
	if ok {
		// Claimed but the restore is still running (browser approval or sync
		// outstanding). Keep reporting pending so the continuation keeps
		// returning needs_human rather than treating a mid-approval restore as
		// a dead hand-off and steering the agent to restart.
		return false, false, false, true
	}
	// The spent tombstone was evicted before the outcome settled; it can never
	// transition to done on its own.
	o.pruneOutcomes()
	return false, false, false, false
}

// forgetOutcome drops the outcome record for a token once a continuation has
// consumed its terminal result (done or failed). This frees observed records
// immediately rather than leaving them to TTL-age, so the outcome map tracks
// only active or recently-settled hand-offs, not every restore ever attempted.
func (o *OOBRestore) forgetOutcome(token string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.outcomes, token)
}

// pruneOutcomes drops outcome records that have gone terminal and are older than
// the restore TTL, keeping the per-token outcome map bounded even when a
// continuation stops polling. Callers in the poll path use this locked wrapper;
// code already holding o.mu calls pruneOutcomesLocked.
func (o *OOBRestore) pruneOutcomes() {
	cutoff := time.Now().Add(-DefaultRestoreTTL)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneOutcomesLocked(cutoff)
}

// pruneOutcomesLocked prunes terminal outcomes older than cutoff. Caller holds
// o.mu. It is invoked from settle (the completion path) so a terminal record is
// never left resident waiting on a later poll to reap it.
func (o *OOBRestore) pruneOutcomesLocked(cutoff time.Time) {
	for token, out := range o.outcomes {
		if (out.succeeded || out.err != "") && out.started.Before(cutoff) {
			delete(o.outcomes, token)
		}
	}
}

// vaultCreateResumeContinuation returns the vault-create-specific poll logic:
// it reports pending (needs_human) until the human has approved the Sia device
// connection on the one-time create page AND retrieved the freshly generated
// recovery seed, then a terminal done result; if the one-time link expires or
// the create fails before the seed is retrieved it terminates the continuation
// and steers the agent to start a fresh vault create (a stale/failed hand-off is
// never reported as done). It is registered against the handle by
// vaultCreateSetupHandler so the shared vault_create_resume template
// dispatches to it. The continuation performs its own registry/handle cleanup
// on every terminal outcome.
func vaultCreateResumeContinuation(oob *OOBCreate, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry) handoff.ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (model.ToolResult, error) {
		token, _ := data[handleDataToken].(string)
		if oob == nil {
			return vaultExpiredResult(handles, reg, handle, vaultCreateResumeToolName, compiledVaultCreateToolName,
				"Vault create is not configured for this server; start a fresh vault create with vault_create")
		}
		done, failed, expired, pending := oob.tokenDone(token)
		switch {
		case done:
			// Vault created + activated and the recovery seed has been retrieved
			// by the human; hand-off over.
			oob.forgetOutcome(token)
			handles.Delete(handle)
			reg.End(handle)
			return model.ToolResult{
				Text:              "Vault create hand-off complete: the vault is active and the recovery seed has been retrieved.",
				StructuredContent: map[string]any{"status": model.StatusDone, "handle": handle},
			}, nil
		case failed:
			// RunCreate failed (approval/registration error). Do not report done;
			// terminate and steer to restart so the human can retry.
			oob.forgetOutcome(token)
			return vaultExpiredResult(handles, reg, handle, vaultCreateResumeToolName, compiledVaultCreateToolName,
				"The vault create failed on the one-time page (the Sia device approval/registration errored). Start a fresh vault create with vault_create so a new create_url is minted.")
		case expired:
			// One-time link expired before the vault was created and the seed
			// retrieved. Do not report completion; terminate and steer to a fresh
			// start.
			return vaultExpiredResult(handles, reg, handle, vaultCreateResumeToolName, compiledVaultCreateToolName,
				"The one-time create_url expired before the vault was created and the seed retrieved; start a fresh vault create with vault_create so a new create_url is minted.")
		case pending:
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonCredentialEntry,
				Handle:     handle,
				ResumeTool: vaultCreateResumeToolName,
				Detail:     "Ask the user to open the create_url in a browser, approve the Sia device connection, then retrieve the one-time recovery seed. Then call vault_create_resume with the handle.",
			}), nil
		default:
			// Token is absent (never existed, or its spent tombstone was evicted)
			// and cannot transition on its own. Do not report done and do not
			// leave the agent pending forever. Terminate and steer to a fresh
			// start.
			return vaultExpiredResult(handles, reg, handle, vaultCreateResumeToolName, compiledVaultCreateToolName,
				"The vault create hand-off is no longer resolvable; start a fresh vault create with vault_create so a new create_url is minted.")
		}
	}
}

// vaultRestoreResumeContinuation returns the vault-restore-specific poll logic:
// it reports pending (needs_human) until the human has submitted the recovery
// seed on the one-time restore page, then a terminal done result; if the
// one-time link expires before use it terminates the continuation and steers
// the agent to restart (an expired restore is not reported as done). It
// is registered against the handle by mintVaultHandoff so the shared
// vault_restore_resume template dispatches to it. It is a pure
// coordinator-state poll (the token going spent); the RestoreRunner only runs
// in the browser POST handler, never on this channel, so the seed never
// reaches the agent.
func vaultRestoreResumeContinuation(oob *OOBRestore, handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry) handoff.ResumeContinuation {
	return func(ctx context.Context, handle string, data map[string]any) (model.ToolResult, error) {
		token, _ := data[handleDataToken].(string)
		if oob == nil {
			return vaultExpiredResult(handles, reg, handle, vaultRestoreResumeToolName, compiledVaultRestoreToolName,
				"Vault restore is not configured for this server; start a fresh vault restore with vault_restore")
		}
		done, failed, expired, pending := oob.tokenDone(token)
		switch {
		case done:
			// Restore succeeded; hand-off over. Free the consumed outcome record.
			oob.forgetOutcome(token)
			handles.Delete(handle)
			reg.End(handle)
			return model.ToolResult{
				Text:              "Vault restore hand-off complete: the vault has been restored.",
				StructuredContent: map[string]any{"status": model.StatusDone, "handle": handle},
			}, nil
		case failed:
			// RunRestore failed (wrong mnemonic, approval/registration error). Do
			// not report done; terminate and steer the agent to restart so the
			// human can correct the seed. vaultExpiredResult clears the handle and
			// the consumed outcome record is freed.
			oob.forgetOutcome(token)
			return vaultExpiredResult(handles, reg, handle, vaultRestoreResumeToolName, compiledVaultRestoreToolName,
				"The restore failed on the one-time page (the recovery phrase was rejected or the device approval/registration errored). Review the seed and start a fresh vault restore with vault_restore so a new restore_url is minted.")
		case expired:
			// One-time link expired before the human completed the restore.
			return vaultExpiredResult(handles, reg, handle, vaultRestoreResumeToolName, compiledVaultRestoreToolName,
				"The one-time restore_url expired before the restore was completed; start a fresh vault restore with vault_restore so a new restore_url is minted.")
		case pending:
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonCredentialEntry,
				Handle:     handle,
				ResumeTool: vaultRestoreResumeToolName,
				Detail:     "Ask the user to open the restore_url in a browser and enter the recovery seed to complete the restore. Then call vault_restore_resume with the handle.",
			}), nil
		default:
			// Token is absent (never existed, or its spent tombstone was evicted)
			// and cannot transition on its own. Do not report done and do not
			// leave the agent pending forever. Terminate and steer to a fresh
			// start.
			return vaultExpiredResult(handles, reg, handle, vaultRestoreResumeToolName, compiledVaultRestoreToolName,
				"The vault restore hand-off is no longer resolvable; start a fresh vault restore with vault_restore so a new restore_url is minted.")
		}
	}
}

// vaultExpiredResult terminates an expired (or unconfigured) vault hand-off:
// it clears the continuation and backing handle so the agent is not left
// polling a dead flow, and returns a needs_human steer to the matching start
// tool. It is NOT a success result; an expired one-time link must read as a
// restart, never as a completed vault create/restore.
func vaultExpiredResult(handles *session.AsyncHandleStore, reg *handoff.HandoffRegistry, handle, resumeTool, restartTool, detail string) (model.ToolResult, error) {
	// Clearing the continuation + backing handle means the next poll of the
	// *_resume tool hits the template's dead-handle branch, which steers to
	// restart via the tool spec. The agent gets one clean restart instruction
	// instead of a forever-pending or falsely-done hand-off.
	handles.Delete(handle)
	reg.End(handle)
	_ = resumeTool
	return model.NeedsHumanResult(model.NeedsHuman{
		Reason:     model.ReasonCredentialEntry,
		ResumeTool: restartTool,
		Detail:     detail,
	}), nil
}

// NewVaultCreateResumeDescriptor returns the vault_create_resume tool,
// built from the shared resume template. Name/description and restart steering
// are create-flavored: a dead handle steers back to vault_create.
func NewVaultCreateResumeDescriptor(reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return handoff.NewResumeTool(handoff.ResumeToolSpec{
		Name:                vaultCreateResumeToolName,
		Title:               "Vault Create Resume",
		Description:         "Poll a pending vault create hand-off to check whether the human has approved the Sia device connection on the one-time create_url and retrieved the recovery seed. Returns pending (needs_human) until the vault is active and the seed has been retrieved, then reports done. Pass the handle returned by vault_create.",
		RestartTool:         compiledVaultCreateToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault create with vault_create",
		ExpiredHandleDetail: "the vault create hand-off expired before the vault was created and the seed retrieved; start a fresh vault create with vault_create so a new create_url is minted",
		DeadHandleReason:    model.ReasonCredentialEntry,
		Category:            model.CategoryVault,
	}, reg, handles)
}

// NewVaultRestoreResumeDescriptor returns the vault_restore_resume tool,
// built from the shared resume template. Name/description and restart steering
// are restore-flavored: a dead handle steers back to vault_restore.
func NewVaultRestoreResumeDescriptor(reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return handoff.NewResumeTool(handoff.ResumeToolSpec{
		Name:                vaultRestoreResumeToolName,
		Title:               "Vault Restore Resume",
		Description:         "Poll a pending vault restore hand-off to check whether the human has completed the out-of-band restore on the one-time restore_url. Returns pending (needs_human) until the restore is done, then reports done. Pass the handle returned by vault_restore.",
		RestartTool:         compiledVaultRestoreToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault restore with vault_restore",
		ExpiredHandleDetail: "the vault restore hand-off expired before the human completed it; start a fresh vault restore with vault_restore so a new restore_url is minted",
		DeadHandleReason:    model.ReasonCredentialEntry,
		Category:            model.CategoryVault,
	}, reg, handles)
}
