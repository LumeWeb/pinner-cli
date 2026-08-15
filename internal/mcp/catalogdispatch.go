package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// This file provides the generic operation-catalog dispatch seam: a typed MCP
// ToolRequest is routed through the catalog's Catalog.Invoke gate (not around
// it) so Interaction, Visibility, Safety, and required-arg enforcement all run,
// and then the typed result (any) is converted into an SDK-neutral ToolResult.
//
// It mirrors the pattern in vault_setup_ops.go (NormalizeOperationInput +
// op.Handler().Execute) but stays on the Invoke side of the seam, so a caller
// that only has (name, args, actor) never reaches into an Operation.Handler
// directly and never bypasses the dispatch gates.

// DispatchCatalogOp routes a typed tool request through the operation
// catalog's Invoke gate (the single enforcement point for Interaction,
// Visibility, Safety, and required-arg validation) and converts the result
// into an SDK-neutral ToolResult.
//
// MCP errors are carried inside the returned ToolResult (IsError=true), not as
// Go errors, so the function returns nil as its error on every dispatch path
// the caller can act on. Only a genuinely unexpected failure (a result that
// cannot be handled at all) surfaces as a non-nil error.
//
// Two refusal classes are NOT treated as hard errors, matching the
// needs_human contract in human_handoff.go:
//
//   - A destructive operation invoked by a model actor returns an error that
//     wraps the catalog.ErrConfirmRequired sentinel; that is mapped to a
//     NeedsHumanResult with Reason=ReasonConfirmation so the caller presents a
//     confirm flow rather than a failure.
//
//   - An InteractionHumanOnly / InteractionNeedsHandoff operation invoked by a
//     non-human actor is refused by the gate with an error wrapping the
//     catalog.ErrHumanRequired sentinel. That is mapped to a NeedsHumanResult
//     with Reason=ReasonInteractiveOnly.
//
// On any other error the result is a plain ToolResult{IsError:true} with a
// cleaned message.
func DispatchCatalogOp(ctx context.Context, cat catalog.Catalog, actor catalog.Actor, name string, args map[string]any, resumeTool string) (ToolResult, error) {
	// AgentRequired args are enforced here, in the MCP dispatch layer, not in
	// Catalog.Invoke / NormalizeOperationInput — those are shared normalization
	// seams that the CLI and other non-MCP callers use, and AgentRequired must
	// never leak into a non-MCP invocation. So the AgentRequired check belongs
	// at the MCP surface, just before the op runs.
	if op, ok := cat.Get(name); ok {
		if err := catalog.ValidateMCPRequired(op, args); err != nil {
			return ToolResult{IsError: true, Text: cleanMessage(err)}, nil
		}
	}
	result, err := cat.Invoke(ctx, name, args, actor)
	if err != nil {
		// A destructive op invoked by a model needs explicit human
		// confirmation. Surface it as a confirm hand-off, not an error.
		if errors.Is(err, catalog.ErrConfirmRequired) {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonConfirmation,
				ResumeTool: resumeTool,
				Detail:     name + " is destructive and requires explicit human confirmation",
			}), nil
		}
		// An InteractiveOnly/NeedsHandoff op refused for a non-human actor is
		// a hand-off to the human, not a failure.
		if errors.Is(err, catalog.ErrHumanRequired) {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonInteractiveOnly,
				ResumeTool: resumeTool,
				Detail:     name + " requires a human to complete; resume with " + resumeTool,
			}), nil
		}
		return ToolResult{IsError: true, Text: cleanMessage(err)}, nil
	}

	return resultToToolResult(result), nil
}

// resultToToolResult converts the typed (any) result returned by the catalog
// gate into an SDK-neutral ToolResult. Every successful result is wrapped in
// the single canonical envelope {"status":"ok","value":<result>}, regardless
// of whether the result is an object, array, or scalar, e.g.
//
//	auth_status -> {"status":"ok","value":{"authenticated":true,...}}
//	pins_list   -> {"status":"ok","value":[...] }
//	auth_login  -> {"status":"ok","value":{"status":"logged_in","message":...}}
//
// Keeping the result under "value" gives every tool the same envelope shape,
// and lets a result carry its own "status" field (e.g. auth_login ->
// {"status":"logged_in"}) without colliding with the transport "status":"ok".
// The Text channel holds the same JSON as StructuredContent so both agree.
func resultToToolResult(result any) ToolResult {
	switch v := result.(type) {
	case ToolResult:
		return v
	case *ToolResult:
		if v != nil {
			return *v
		}
		return ToolResult{Text: `{"status":"ok"}`, StructuredContent: map[string]any{"status": StatusOk}}
	}
	b, err := json.Marshal(result)
	if err != nil {
		return ToolResult{IsError: true, Text: "unable to serialize catalog result"}
	}
	// A marshaled "null" is a genuine empty result: emit the bare envelope.
	if string(b) == "null" {
		sc := map[string]any{"status": StatusOk}
		jb, _ := json.Marshal(sc)
		return ToolResult{Text: string(jb), StructuredContent: sc}
	}
	// Every non-empty result lives under "value" inside the same {status, value}
	// envelope, uniformly for objects, arrays, and scalars.
	var value any
	if json.Valid(b) {
		value = json.RawMessage(b)
	}
	sc := map[string]any{"status": StatusOk, "value": value}
	jb, _ := json.Marshal(sc)
	return ToolResult{Text: string(jb), StructuredContent: sc}
}

// cleanMessage returns a single-line, non-empty error message for surfacing as
// ToolResult.Text, guarding against an empty string.
func cleanMessage(err error) string {
	if err == nil {
		return "operation failed"
	}
	return strings.TrimSpace(translateAgentGuidance(err.Error()))
}

// translateAgentGuidance rewrites CLI-only shell commands embedded in error
// messages into the corresponding agent-facing MCP tool names. The vault core
// package intentionally words errors for the `pinner` CLI (e.g. "Run 'pinner
// vault setup'", "Run 'pinner vault create --profile X' or 'pinner vault
// restore --profile X'"), but an agent has no such command — pointing it at a
// shell invocation is a dead end. Map the known CLI-isms to the real tools so
// the error tells the agent what to call next.
func translateAgentGuidance(msg string) string {
	replacer := strings.NewReplacer(
		"Run 'pinner vault setup' to create one",
		"no vault profile exists; create one with vault_create (or restore one with vault_restore)",
	)
	msg = replacer.Replace(msg)

	// The create/restore producers (internal/core/vault/registry.go and
	// vault_service.go) interpolate the real profile name and combine both
	// commands in one sentence, e.g.
	//   ... Run 'pinner vault create --profile alice' or 'pinner vault restore --profile alice'
	//   ... Provision it with 'pinner vault create --profile alice' or 'pinner vault restore --profile alice'
	// The profile is already named earlier in the message, so rewrite the whole
	// quoted pair to name the two tools without repeating the CLI flags.
	createRestoreRe := regexp.MustCompile(`'pinner vault create --profile [^']+' or 'pinner vault restore --profile [^']+'`)
	msg = createRestoreRe.ReplaceAllString(msg, "vault_create or vault_restore")
	return msg
}
