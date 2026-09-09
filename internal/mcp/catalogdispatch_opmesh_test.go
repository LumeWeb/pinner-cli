package mcp

import (
	"context"
	"testing"

	"go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// Characterization tests for the opmesh dispatch seam (re-integration stage 4).
// DispatchCatalogOp now operates on opmesh.Catalog with opmesh.Actor; the
// AgentRequired pre-check is replicated locally (opmesh deliberately keeps
// AgentRequired out of its shared Invoke gate) while Required-no-default
// enforcement is left to the Invoke gate itself. These tests pin the seam
// contract: agent-required enforcement, camelCase alias acceptance, the
// destructive confirm hand-off, and the human-interactive hand-off.

type dispatchCaptureHandler struct {
	args map[string]any
}

func (h *dispatchCaptureHandler) Execute(_ context.Context, input map[string]any) (any, error) {
	h.args = input
	return map[string]any{"ok": true}, nil
}

// TestDispatchAgentRequiredRefused pins that an AgentRequired arg missing from
// the input is refused by the MCP dispatch pre-check (not by the shared
// Invoke gate) with the exact pre-seam message.
func TestDispatchAgentRequiredRefused(t *testing.T) {
	called := dispatchCaptureHandler{}
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "agent_required_probe", Safety: opmesh.SafetyRead,
		Args: []opmesh.OperationArg{
			{Name: "zone", Type: opmesh.ArgTypeString, AgentRequired: true},
		},
		Handler: &called,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := DispatchCatalogOp(context.Background(), cat, opmesh.ActorModel, "agent_required_probe", map[string]any{}, "agent_required_probe")
	if err != nil {
		t.Fatalf("DispatchCatalogOp: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	if res.Text != `missing required argument "zone"` {
		t.Errorf("Text = %q, want missing-required message", res.Text)
	}
	if called.args != nil {
		t.Errorf("handler must not run when an AgentRequired arg is missing")
	}
}

// TestDispatchAgentRequiredCamelCaseAlias pins that the AgentRequired pre-check
// accepts the camelCase spelling of a kebab arg, matching the registry's own
// alias acceptance contract.
func TestDispatchAgentRequiredCamelCaseAlias(t *testing.T) {
	called := dispatchCaptureHandler{}
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "agent_required_alias", Safety: opmesh.SafetyRead,
		Args: []opmesh.OperationArg{
			{Name: "zone-name", Type: opmesh.ArgTypeString, AgentRequired: true},
		},
		Handler: &called,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := DispatchCatalogOp(context.Background(), cat, opmesh.ActorModel, "agent_required_alias", map[string]any{"zoneName": "d"}, "agent_required_alias")
	if err != nil {
		t.Fatalf("DispatchCatalogOp: %v", err)
	}
	if res.IsError {
		t.Fatalf("camelCase alias must satisfy an AgentRequired arg, got %q", res.Text)
	}
	if called.args == nil {
		t.Errorf("handler must run when the AgentRequired arg is satisfied via alias")
	}
}

// TestDispatchAgentRequiredNotEnforcedOnNonAgent pinS the seam's central rule:
// the AgentRequired pre-check is MCP-dispatch-specific. A direct Invoke on the
// same catalog (the CLI path) runs the handler without the AgentRequired arg —
// AgentRequired never leaks into a non-MCP invocation.
func TestDispatchAgentRequiredNotEnforcedOnNonAgent(t *testing.T) {
	called := dispatchCaptureHandler{}
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "agent_required_probe", Safety: opmesh.SafetyRead, Visibility: opmesh.VisibilityBoth,
		Args: []opmesh.OperationArg{
			{Name: "zone", Type: opmesh.ArgTypeString, AgentRequired: true},
		},
		Handler: &called,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := cat.Invoke(context.Background(), "agent_required_probe", map[string]any{}, opmesh.ActorHuman); err != nil {
		t.Fatalf("non-MCP Invoke must ignore AgentRequired: %v", err)
	}
	if called.args == nil {
		t.Errorf("handler must run on the non-agent path without the AgentRequired arg")
	}
}

// TestDispatchDestructiveNeedsConfirm pins the destructive confirmation
// hand-off: a destructive op invoked by a model actor without a confirm flag
// is refused with a needs_human result (Reason=confirmation), never an error.
func TestDispatchDestructiveNeedsConfirm(t *testing.T) {
	called := dispatchCaptureHandler{}
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "destructive_probe", Safety: opmesh.SafetyDestructive,
		Args: []opmesh.OperationArg{
			{Name: "confirm", Type: opmesh.ArgTypeBool, AgentConfirm: true},
		},
		Handler: &called,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := DispatchCatalogOp(context.Background(), cat, opmesh.ActorModel, "destructive_probe", map[string]any{}, "destructive_resume")
	if err != nil {
		t.Fatalf("DispatchCatalogOp: %v", err)
	}
	if res.IsError {
		t.Fatalf("confirmation refusal must not be a hard error: %s", res.Text)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["status"] != model.StatusNeedsHuman {
		t.Fatalf("expected needs_human result, got %+v", res.StructuredContent)
	}
	if sc["reason"] != model.ReasonConfirmation {
		t.Errorf("reason = %v, want %q", sc["reason"], model.ReasonConfirmation)
	}
	if sc["resume_tool"] != "destructive_resume" {
		t.Errorf("resume_tool = %v, want destructive_resume", sc["resume_tool"])
	}
}

// TestDispatchHumanOnlyNeedsHuman pins the Interaction gate mapping: a
// HumanOnly op invoked by a model actor returns the interactive-only hand-off.
func TestDispatchHumanOnlyNeedsHuman(t *testing.T) {
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "human_probe", Interaction: opmesh.InteractionHumanOnly,
		Args:    []opmesh.OperationArg{{Name: "otp", Type: opmesh.ArgTypeString}},
		Handler: &dispatchCaptureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := DispatchCatalogOp(context.Background(), cat, opmesh.ActorModel, "human_probe", map[string]any{"otp": "123456"}, "human_resume")
	if err != nil {
		t.Fatalf("DispatchCatalogOp: %v", err)
	}
	if res.IsError {
		t.Fatalf("human-only refusal must not be an error result: %s", res.Text)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["status"] != model.StatusNeedsHuman {
		t.Fatalf("expected needs_human result, got %+v", res.StructuredContent)
	}
	if sc["reason"] != model.ReasonInteractiveOnly {
		t.Errorf("reason = %v, want %q", sc["reason"], model.ReasonInteractiveOnly)
	}
}
