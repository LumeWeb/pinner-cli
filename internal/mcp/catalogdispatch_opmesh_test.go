package mcp

import (
	"context"
	"errors"
	"testing"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner/catalogmeta"
	"go.lumeweb.com/pinner/catalogops"
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

// TestDispatchRequiredArgMissingPlainMessage preserves the pre-seam behavior:
// a missing Required-no-default arg is reported by the dispatch pre-check with
// the plain `missing required argument "x"` text (not opmesh Invoke's
// `operation "x": ...` wrapper), so model-facing error text is unchanged.
func TestDispatchRequiredArgMissingPlainMessage(t *testing.T) {
	called := dispatchCaptureHandler{}
	cat := opmesh.NewCatalog()
	if err := cat.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "required_probe", Safety: opmesh.SafetyRead,
		Args: []opmesh.OperationArg{
			{Name: "cid", Type: opmesh.ArgTypeString, Required: true},
		},
		Handler: &called,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := DispatchCatalogOp(context.Background(), cat, opmesh.ActorModel, "required_probe", map[string]any{"cid": ""}, "required_resume")
	if err != nil {
		t.Fatalf("DispatchCatalogOp: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	if res.Text != `missing required argument "cid"` {
		t.Errorf("Text = %q, want plain missing-required message", res.Text)
	}
	if called.args != nil {
		t.Errorf("handler must not run for a missing Required arg")
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

// TestCatalogAgentOnlyArgsOptional pins the AgentOnly invariant at the CLI
// consumer boundary: opmesh.Add deliberately does not validate frontend
// metadata, so an AgentOnly arg that is Required with no declared Default
// would render mandatory on the MCP surface while being impossible to supply
// from the CLI surface (CLI callers can never provide an AgentOnly arg),
// making such operations uninvokable there. Every AgentOnly arg in the
// assembled catalog must therefore be optional (not Required, or satisfied by
// a declared Default). This passes today; it guards future drift.
func TestCatalogAgentOnlyArgsOptional(t *testing.T) {
	bundle := &CatalogDepsBundle{
		Auth:       catalogops.AuthDeps{},
		Account:    catalogops.AccountDeps{},
		Vault:      catalogops.VaultDeps{},
		VaultSetup: catalogops.VaultDeps{},
		Pins:       catalogops.PinsDeps{},
		Websites:   catalogops.WebsitesDeps{},
		DNS:        catalogops.DNSDeps{},
		IPNS:       catalogops.IPNSDeps{},
		ENS:        catalogops.ENSDeps{},
		APIKeys:    catalogops.APIKeysDeps{},
		Operations: catalogops.OperationsDeps{},
		Admin:      catalogops.AdminDeps{},
	}
	cat, err := AssembleCatalogOps(bundle, FullSurface, false)
	if err != nil {
		t.Fatalf("AssembleCatalogOps: %v", err)
	}
	ops := cat.Search("", "", opmesh.VisibilityBoth)
	if len(ops) == 0 {
		t.Fatal("assembled catalog is empty — nothing to check")
	}
	checked := 0
	for _, op := range ops {
		for _, a := range op.Args() {
			fe := catalogmeta.ArgFrontendForArg(op.Name(), a.Name)
			if fe == nil || !fe.AgentOnly {
				continue
			}
			checked++
			if a.Required && a.Default == "" {
				t.Errorf("operation %q: AgentOnly arg %q is Required with no Default — uninvokable on the CLI surface", op.Name(), a.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no AgentOnly args found in the catalog — catalogmeta metadata and catalogops ops have drifted entirely; the invariant is not being exercised")
	}
}

// TestNormalizeOperationInputMissingRequiredContract pins the plain-form
// missing-required message opmesh.NormalizeOperationInput produces. The MCP
// dispatch seam (firstMissingDispatchRequiredArg and the keep-out comment in
// catalogdispatch.go) relies on this exact text so Invoke's refusal is
// indistinguishable from the seam's own pre-check messages. If opmesh changes
// the wording, this fails deliberately and the seam must be re-reviewed.
func TestNormalizeOperationInputMissingRequiredContract(t *testing.T) {
	op := opmesh.NewOperation(opmesh.OperationSpec{
		Name: "contract_probe", Safety: opmesh.SafetyRead,
		Args: []opmesh.OperationArg{
			{Name: "zone-id", Type: opmesh.ArgTypeString, Required: true},
		},
		Handler: &dispatchCaptureHandler{},
	})
	_, err := opmesh.NormalizeOperationInput(op, map[string]any{})
	if err == nil {
		t.Fatal("expected missing-required error, got nil")
	}
	if got := err.Error(); got != `missing required argument "zone-id"` {
		t.Errorf("missing-required text drifted: got %q, want %q", got, `missing required argument "zone-id"`)
	}
}

// TestInvokeUnknownOperationContract pins the unknown-operation path dispatch
// flows through: the error wraps opmesh.ErrUnknownOperation and carries the
// exact `opmesh: unknown operation "<name>"` text, so drift in either the
// sentinel wrapping or the message wording surfaces here on purpose.
func TestInvokeUnknownOperationContract(t *testing.T) {
	cat := opmesh.NewCatalog()
	_, err := cat.Invoke(context.Background(), "no_such_op", map[string]any{}, opmesh.ActorModel)
	if err == nil {
		t.Fatal("expected unknown-operation error, got nil")
	}
	if !errors.Is(err, opmesh.ErrUnknownOperation) {
		t.Errorf("error must wrap opmesh.ErrUnknownOperation, got: %v", err)
	}
	if got := err.Error(); got != `opmesh: unknown operation "no_such_op"` {
		t.Errorf("unknown-operation text drifted: got %q, want %q", got, `opmesh: unknown operation "no_such_op"`)
	}
}
