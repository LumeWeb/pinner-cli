package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"
	"go.lumeweb.com/pinner-cli/internal/catalog"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// This file provides the generic operation-catalog dispatch seam: a typed MCP
// ToolRequest is routed through the catalog's Catalog.Invoke gate (not around
// it) so Interaction, Visibility, Safety, and required-arg enforcement all run,

// catalogEnvelopeSchema is the typed shape of a catalog tool's *success*
// StructuredContent: an object whose `status` is always "ok" and whose optional
// `value` holds the op result. Only the success path emits this {status,value}
// envelope (resultToToolResult); error results carry no StructuredContent
// (they are signaled by the wire IsError flag) and needs_human results use a
// different shape entirely (NeedsHumanResult), so neither belongs here. The
// type is reflected into catalogOutputSchema with the project's
// invopop/jsonschema reflector, keeping schema generation consistent with every
// other tool schema and free of ad-hoc JSON strings.
type catalogEnvelopeSchema struct {
	Status string `json:"status" jsonschema:"required,description=Always ok on success"`
	Value  any    `json:"value,omitempty" jsonschema:"description=Operation result"`
}

// catalogEnvelopeReflector derives the envelope schema. AllowAdditionalProperties
// keeps the schema open so the dynamic per-operation `value` and any transport
// fields validate — unlike closed input schemas, the output envelope must
// accept the concrete op result the handler returns.
var catalogEnvelopeReflector = &jsonschema.Reflector{
	DoNotReference:            true,
	Anonymous:                 true,
	AllowAdditionalProperties: true,
}

// catalogOutputSchema is the JSON Schema describing the StructuredContent that
// a catalog-dispatched tool emits on success: the canonical {status:"ok", value}
// envelope from resultToToolResult. Error and needs_human responses are not
// described here — errors carry no structured content (IsError signals them),
// and needs_human uses catalogNeedsHumanOutputSchema — so the declared schema
// matches the shape the handler actually returns on the success path.
var catalogOutputSchema = func() json.RawMessage {
	b, err := json.Marshal(catalogEnvelopeReflector.Reflect(catalogEnvelopeSchema{}))
	if err != nil {
		// Only possible on an un-marshalable struct; the envelope is fully
		// serializable, so this cannot happen in practice.
		panic(err)
	}
	return b
}()

// catalogNeedsHumanSchema is the typed shape of a NeedsHumanResult's
// StructuredContent: a {status:"needs_human", reason, ...} object (see
// NeedsHumanResult). It is the declared output schema for tools whose handler
// returns the needs_human hand-off shape (the vault_create / vault_restore
// setup swaps) rather than the {status,value} success envelope. The URL key is
// tool-specific: SSO/account tools emit action_url, while the vault setup
// hand-offs emit create_url / restore_url (vaultHandoffResult) — so the schema
// declares both sets, keeping every emitting tool's shape covered.
type catalogNeedsHumanSchema struct {
	Status     string `json:"status" jsonschema:"required,description=Always needs_human"`
	Reason     string `json:"reason" jsonschema:"required,description=Why human action is required"`
	ActionURL  string `json:"action_url,omitempty" jsonschema:"description=Short-lived URL the human opens (SSO and account hand-offs)"`
	CreateURL  string `json:"create_url,omitempty" jsonschema:"description=Out-of-band vault create URL (vault_create hand-off)"`
	RestoreURL string `json:"restore_url,omitempty" jsonschema:"description=Out-of-band vault restore URL (vault_restore hand-off)"`
	Handle     string `json:"handle,omitempty" jsonschema:"description=Async handle for a matching resume/status tool"`
	ResumeTool string `json:"resume_tool,omitempty" jsonschema:"description=Tool name to poll or resume with"`
	Detail     string `json:"detail,omitempty" jsonschema:"description=Optional human-readable context"`
}

// catalogNeedsHumanReflector derives the needs_human schema with the same
// open-additional-properties policy as the success envelope.
var catalogNeedsHumanReflector = &jsonschema.Reflector{
	DoNotReference:            true,
	Anonymous:                 true,
	AllowAdditionalProperties: true,
}

// catalogNeedsHumanOutputSchema is the JSON Schema describing a NeedsHumanResult
// StructuredContent. It is emitted as the outputSchema for the tools whose
// handlers are swapped post-compile to return the needs_human hand-off (see
// adapter.go), so their declared schema matches what they actually return.
var catalogNeedsHumanOutputSchema = func() json.RawMessage {
	b, err := json.Marshal(catalogNeedsHumanReflector.Reflect(catalogNeedsHumanSchema{}))
	if err != nil {
		panic(err)
	}
	return b
}()

// catalogOutputUnionSchema is the JSON Schema a destructive or interactive-only
// compiled operation emits: an anyOf of the success envelope
// ({status:"ok", value}) and the needs_human hand-off shape. A destructive
// operation invoked by a model is first refused with ErrConfirmRequired (mapped
// to the needs_human hand-off by DispatchCatalogOp), and only after human
// confirmation resumes does it run and return the {status:ok,value} success
// envelope — so its declared output schema must admit both shapes. The two
// members reuse the typed envelope and needs_human schemas above, keeping schema
// generation consistent and free of ad-hoc JSON. See outputSchemaForCompiled for
// per-classification selection.
//
// The root is explicitly object-typed: an outputSchema describes the
// StructuredContent of a tool result, which is always a JSON object, and the
// MCP tool schema contract requires an object-rooted output schema. Without the
// top-level type:object the root serializes to a bare anyOf object, which is
// neither a valid object schema nor accepted by 2025-era (15.x) model connectors
// that import tools/list. Every anyOf branch already requires type:object, so
// adding the root type does not change which values validate.
var catalogOutputUnionSchema = func() json.RawMessage {
	s := &jsonschema.Schema{
		Type: "object",
		AnyOf: []*jsonschema.Schema{
			catalogEnvelopeReflector.Reflect(catalogEnvelopeSchema{}),
			catalogNeedsHumanReflector.Reflect(catalogNeedsHumanSchema{}),
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}()

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
func DispatchCatalogOp(ctx context.Context, cat catalog.Catalog, actor catalog.Actor, name string, args map[string]any, resumeTool string) (model.ToolResult, error) {
	// AgentRequired args are enforced here, in the MCP dispatch layer, not in
	// Catalog.Invoke / NormalizeOperationInput — those are shared normalization
	// seams that the CLI and other non-MCP callers use, and AgentRequired must
	// never leak into a non-MCP invocation. So the AgentRequired check belongs
	// at the MCP surface, just before the op runs.
	if op, ok := cat.Get(name); ok {
		if err := catalog.ValidateMCPRequired(op, args); err != nil {
			return model.ToolResult{IsError: true, Text: cleanMessage(err)}, nil
		}
	}
	result, err := cat.Invoke(ctx, name, args, actor)
	if err != nil {
		// A destructive op invoked by a model needs explicit human
		// confirmation. Surface it as a confirm hand-off, not an error.
		if errors.Is(err, catalog.ErrConfirmRequired) {
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonConfirmation,
				ResumeTool: resumeTool,
				Detail:     name + " is destructive and requires explicit human confirmation",
			}), nil
		}
		// An InteractiveOnly/NeedsHandoff op refused for a non-human actor is
		// a hand-off to the human, not a failure.
		if errors.Is(err, catalog.ErrHumanRequired) {
			return model.NeedsHumanResult(model.NeedsHuman{
				Reason:     model.ReasonInteractiveOnly,
				ResumeTool: resumeTool,
				Detail:     name + " requires a human to complete; resume with " + resumeTool,
			}), nil
		}
		return model.ToolResult{IsError: true, Text: cleanMessage(err)}, nil
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
func resultToToolResult(result any) model.ToolResult {
	switch v := result.(type) {
	case model.ToolResult:
		return v
	case *model.ToolResult:
		if v != nil {
			return *v
		}
		return model.ToolResult{Text: `{"status":"ok"}`, StructuredContent: map[string]any{"status": model.StatusOk}}
	}
	b, err := json.Marshal(result)
	if err != nil {
		return model.ToolResult{IsError: true, Text: "unable to serialize catalog result"}
	}
	// A marshaled "null" is a genuine empty result: emit the bare envelope.
	if string(b) == "null" {
		sc := map[string]any{"status": model.StatusOk}
		jb, _ := json.Marshal(sc)
		return model.ToolResult{Text: string(jb), StructuredContent: sc}
	}
	// Every non-empty result lives under "value" inside the same {status, value}
	// envelope, uniformly for objects, arrays, and scalars.
	var value any
	if json.Valid(b) {
		value = json.RawMessage(b)
	}
	sc := map[string]any{"status": model.StatusOk, "value": value}
	jb, _ := json.Marshal(sc)
	return model.ToolResult{Text: string(jb), StructuredContent: sc}
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
