package mcp

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// ---------------------------------------------------------------------------
// UI elicitation abstraction
//
// The Go SDK v1.7.0 exposes the 2026-07-28 multi-round-trip (MRTR) mechanism at
// the wire level: a tool handler returns a CallToolResult whose InputRequests
// map carries *ElicitParams, the SDK marks it resultType "input_required", the
// client renders the requested form and retries the call with the submitted
// content in CallToolParams.InputResponses.
//
// That low-level plumbing is the analog of the TS SDK's
// inputRequired(...)/acceptedContent(...) builders. This file puts a
// Pinner-owned, SDK-neutral layer in front of it so our handlers describe "I
// need form input X" without touching SDK wire types, and read the client's
// answer without unwrapping raw responses.
// ---------------------------------------------------------------------------

// ElicitationSpec is the SDK-neutral interactive-input request type; it lives
// in core/model. The SDK seam functions below convert it to/from wire types.

// callToolResultFromElicitation builds an SDK CallToolResult that asks the
// client for the described input. The SDK's MRTR middleware sets
// resultType "input_required" because InputRequests is non-empty.
func callToolResultFromElicitation(spec model.ElicitationSpec) *mcp.CallToolResult {
	elicit := &mcp.ElicitParams{
		Mode:    "form",
		Message: spec.Message,
	}
	if spec.URL != "" || spec.ElicitationID != "" {
		elicit.Mode = "url"
		elicit.URL = spec.URL
		// NOTE: elicitationId was removed from URL-mode params in the
		// 2026-07-28 revision (SPR #2891); the client learns the outcome by
		// retrying the original request, so we do not emit it on the wire.
		// The field is kept on ElicitationSpec only for callers (none today)
		// that need to thread an id through an out-of-band flow internally.
	} else {
		elicit.RequestedSchema = spec.FormSchema
	}
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{spec.ID: elicit},
		RequestState:  spec.RequestState,
	}
}

// acceptedElicitation reads the accepted form content for the given request id
// from a retried call's InputResponses. It returns the submitted fields and
// true when the user accepted; it returns false for decline/cancel/absent.
func acceptedElicitation(req *mcp.CallToolRequest, id string) (map[string]any, bool) {
	content, ok := acceptedElicitationValue(req, id)
	return content, ok
}

// acceptedElicitations returns every accepted form submission, keyed by its
// elicitation id, for a retried call. Handlers use this to receive form input
// as ordinary arguments on the round-trip retry.
func acceptedElicitations(req *mcp.CallToolRequest) map[string]any {
	out := map[string]any{}
	if req == nil || req.Params == nil || req.Params.InputResponses == nil {
		return out
	}
	for id := range req.Params.InputResponses {
		if content, ok := acceptedElicitationValue(req, id); ok {
			out[id] = content
		}
	}
	return out
}

// acceptedElicitationValue reads the accepted form content for a request id.
// The SDK decodes an action-bearing inputResponse (JSON with an "action" field)
// into *mcp.ElicitResult on the wire (see InputResponseMap.UnmarshalJSON), so
// we read through that struct and never drop a submission regardless of action.
func acceptedElicitationValue(req *mcp.CallToolRequest, id string) (map[string]any, bool) {
	if req == nil || req.Params == nil || req.Params.InputResponses == nil {
		return nil, false
	}
	raw, ok := req.Params.InputResponses[id]
	if !ok {
		return nil, false
	}

	// The wire guarantees *mcp.ElicitResult for action-bearing values, but
	// decode defensively through the struct in case a value arrived as a raw
	// JSON object.
	res, ok := raw.(*mcp.ElicitResult)
	if !ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, false
		}
		var decoded mcp.ElicitResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, false
		}
		res = &decoded
	}
	if res.Action != "accept" || res.Content == nil {
		return nil, false
	}
	return res.Content, true
}
