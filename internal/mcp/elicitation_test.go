package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

func TestCallToolResultFromElicitationForm(t *testing.T) {
	res := sdk.CallToolResultFromElicitation(model.FormElicitation("input", "Needs domain", map[string]any{
		"type": "object", "properties": map[string]any{"domain": map[string]any{"type": "string"}},
	}))
	require.NotNil(t, res)
	require.NotEmpty(t, res.InputRequests)
	el, ok := res.InputRequests["input"].(*mcp.ElicitParams)
	require.True(t, ok)
	require.Equal(t, "form", el.Mode)
	require.Equal(t, "Needs domain", el.Message)
	require.NotNil(t, el.RequestedSchema)
}

func TestCallToolResultFromElicitationURL(t *testing.T) {
	res := sdk.CallToolResultFromElicitation(model.ElicitationSpec{
		ID: "auth", Message: "Complete login", URL: "https://example.com/oauth", ElicitationID: "flow-1",
	})
	el := res.InputRequests["auth"].(*mcp.ElicitParams)
	require.Equal(t, "url", el.Mode)
	require.Equal(t, "https://example.com/oauth", el.URL)
	require.Equal(t, "", el.ElicitationID, "elicitationId was removed from URL-mode params in 2026-07-28")
}

// TestOfficialToolHandlerElicitationRoundTrip drives a handler through the full
// MRTR shape: first the handler asks for input (returns an elicitation), the
// SDK seam converts it to an input_required result, then a retry delivers the
// accepted form content back as the "input" argument.
func TestOfficialToolHandlerElicitationRoundTrip(t *testing.T) {
	closed := make(chan model.ToolResult, 1)

	// localInput is the typed step input decoded from the merged arguments.
	type localInput struct {
		Input json.RawMessage `json:"input"`
	}
	stepHandler := model.PinnerToolHandler(func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[localInput](req)
		if err != nil {
			return model.ToolResult{}, err
		}
		var domain struct {
			Domain string `json:"domain"`
		}
		if len(in.Input) > 0 {
			if err := json.Unmarshal(in.Input, &domain); err != nil {
				return model.ToolResult{}, err
			}
			closed <- model.ToolResult{Text: "got " + domain.Domain}
			return model.ToolResult{Text: "accepted"}, nil
		}
		return model.ToolResult{Elicitation: &model.ElicitationSpec{
			ID:         "input",
			Message:    "Enter domain",
			FormSchema: map[string]any{"type": "object"},
		}}, nil
	})

	handler := officialToolHandler(stepHandler)

	// Round 1: no input yet -> expect an input_required (InputRequests) result.
	r1, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step", Arguments: json.RawMessage(`{}`),
	}})
	require.NoError(t, err)
	require.NotEmpty(t, r1.InputRequests, "expected input_required elicitation on first round")

	// Round 2 (retry): client fulfilled the form and echoes InputResponses.
	r2, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step",
		InputResponses: mcp.InputResponseMap{
			"input": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"domain": "example.com"}},
		},
	}})
	require.NoError(t, err)
	require.Empty(t, r2.InputRequests)
	got := <-closed
	require.Equal(t, "got example.com", got.Text)
}

// TestElicitationRequestStateCarriesSession proves a handler can recover its
// session id from the requestState echoed back on the elicitation retry, even
// when the client does not echo the original session_id argument.
func TestElicitationRequestStateCarriesSession(t *testing.T) {
	type stepInput struct {
		SessionID    string          `json:"session_id"`
		Input        json.RawMessage `json:"input"`
		RequestState string          `json:"request_state"`
	}
	stepHandler := model.PinnerToolHandler(func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[stepInput](req)
		if err != nil {
			return model.ToolResult{}, err
		}
		sess := in.SessionID
		if sess == "" {
			sess = in.RequestState
		}
		var domain struct {
			Domain string `json:"domain"`
		}
		if len(in.Input) > 0 {
			if err := json.Unmarshal(in.Input, &domain); err != nil {
				return model.ToolResult{}, err
			}
			return model.ToolResult{Text: "session " + sess + " got " + domain.Domain}, nil
		}
		return model.ToolResult{Elicitation: &model.ElicitationSpec{
			ID: "input", Message: "Enter domain", RequestState: "sess-42",
		}}, nil
	})
	handler := officialToolHandler(stepHandler)

	r1, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step", Arguments: json.RawMessage(`{"session_id":"sess-42"}`),
	}})
	require.NoError(t, err)
	require.Equal(t, "sess-42", r1.RequestState, "elicitation must carry session_id as requestState")

	// Retry WITHOUT session_id in args, only inputResponses + requestState.
	r2, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step",
		InputResponses: mcp.InputResponseMap{
			"input": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"domain": "example.com"}},
		},
		RequestState: "sess-42",
	}})
	require.NoError(t, err)
	require.Equal(t, "session sess-42 got example.com", r2.Content[0].(*mcp.TextContent).Text)
}

// TestInputResponsesWireRoundTrip proves the SDK decodes an inputResponses
// value into *mcp.ElicitResult when it arrives as real JSON on the wire (the
// exact path a retried call follows), so our struct decode in
// acceptedElicitationValue operates on the SDK's concrete type.
func TestInputResponsesWireRoundTrip(t *testing.T) {
	wire := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{
			"name":"websites_wizard_step",
			"inputResponses":{
				"input":{"action":"accept","content":{"domain":"example.com","plan":"free"}}
			}
		}
	}`)
	var req mcp.CallToolRequest
	require.NoError(t, json.Unmarshal(wire, &req))
	require.NotNil(t, req.Params)
	require.NotNil(t, req.Params.InputResponses)

	// The SDK must have decoded "input" to a *ElicitResult, not a raw map.
	raw, ok := req.Params.InputResponses["input"]
	require.True(t, ok, "inputResponses['input'] missing after wire decode")
	require.IsType(t, &mcp.ElicitResult{}, raw, "SDK must decode inputResponses to *ElicitResult")

	// And our reader must return the submitted fields through the struct.
	content, ok := sdk.AcceptedElicitation(&req, "input")
	require.True(t, ok)
	require.Equal(t, "example.com", content["domain"])
	require.Equal(t, "free", content["plan"])
}

// TestSchemaRequiresInput verifies only steps with a REQUIRED field trigger a
// native form; all-optional and empty schemas auto-advance on the StepResponse
// path.
func TestSchemaRequiresInput(t *testing.T) {
	require.False(t, schemaRequiresInput(nil))
	require.False(t, schemaRequiresInput(&jsonschema.Schema{}), "empty schema must be treated as no-input")
	require.False(t, schemaRequiresInput(toolargs.SchemaFor[ValidateInput]()), "ValidateInput (optional retry only) must not elicit")
	require.False(t, schemaRequiresInput(toolargs.SchemaFor[SetupCompletionInput]()), "all-optional fields must auto-advance, not elicit")
	require.True(t, schemaRequiresInput(toolargs.SchemaFor[DomainInput]()), "a step with a required field must elicit")
	require.True(t, schemaRequiresInput(&jsonschema.Schema{Required: []string{"x"}}))
}

// TestOfficialToolHandlerFlagsFormRetry verifies the SDK seam flags a
// ToolRequest that arrived via an elicitation FormSubmission retry
// (InputResponses), so a handler can re-present the native form on a failed
// submission instead of falling back to plain StepResponse JSON.
func TestOfficialToolHandlerFlagsFormRetry(t *testing.T) {
	got := make(chan bool, 1)
	stepHandler := model.PinnerToolHandler(func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
		got <- req.InputResponses
		return model.ToolResult{Text: "ok"}, nil
	})
	handler := officialToolHandler(stepHandler)

	// Fresh call with no inputResponses must NOT be flagged as a retry.
	if _, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step", Arguments: json.RawMessage(`{"session_id":"s"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	require.False(t, <-got, "fresh call must not be flagged as a form retry")

	// Retry carrying InputResponses MUST be flagged.
	if _, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "websites_wizard_step",
		InputResponses: mcp.InputResponseMap{
			"input": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"domain": "x.com"}},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	require.True(t, <-got, "InputResponses retry must be flagged as a form retry")
}

// TestElicitForStep builds the native form for a step that needs input and
// carries the session id across the round-trip via a signed RequestState;
// NoInput steps yield nil so they stay on the StepResponse path.
func TestElicitForStep(t *testing.T) {
	now := time.Now()
	spec := elicitForStep("sess-9", StepResponse{CurrentStep: "domain", NextStepSchema: toolargs.SchemaFor[DomainInput]()}, now)
	require.NotNil(t, spec, "a step that needs input must elicit")
	require.Equal(t, "input", spec.ID)
	require.NotEmpty(t, spec.RequestState, "requestState must be set")
	// The requestState is a signed token: verifying it returns the session id
	// (and any tamper fails closed).
	got, err := session.VerifyWizardRequestState(spec.RequestState, now)
	require.NoError(t, err)
	require.Equal(t, "sess-9", got, "signed requestState must carry the session id")
	require.NotEqual(t, "sess-9", spec.RequestState, "requestState must be opaque/signed, not the raw id")
	// Tampering must fail closed. Flip a character in the authenticated BODY
	// (the segment before the final '.' separator), which always breaks the MAC
	// and is deterministic regardless of the MAC's own base64url chars.
	dot := strings.LastIndex(spec.RequestState, ".")
	body := spec.RequestState[len(session.RequestStatePrefix):dot]
	first := body[0]
	replace := byte('A')
	if first == 'A' {
		replace = 'B'
	}
	flipped := spec.RequestState[:len(session.RequestStatePrefix)] + string(replace) + body[1:] + spec.RequestState[dot:]
	require.NotEqual(t, spec.RequestState, flipped, "helper must have changed the token")
	_, err = session.VerifyWizardRequestState(flipped, now)
	require.Error(t, err, "tampered requestState must be rejected")

	var schema map[string]any
	rawSchema, err := json.Marshal(spec.FormSchema)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawSchema, &schema))
	require.NotEmpty(t, schema["properties"], "form schema must carry the step's fields")

	require.Nil(t, elicitForStep("sess-9", StepResponse{CurrentStep: "dns_setup", NextStepSchema: toolargs.SchemaFor[NoInput]()}, now), "NoInput steps must not elicit")
	require.Nil(t, elicitForStep("sess-9", StepResponse{CurrentStep: "done", NextStepSchema: nil}, now), "nil schema must not elicit")
}

// TestRePresentFormOnFailure verifies a failed form retry re-presents the
// native form WITH the validation error in the message (so the user gets
// feedback, not a blank form) and fails over to nil for NoInput steps.
func TestRePresentFormOnFailure(t *testing.T) {
	now := time.Now()
	resp := StepResponse{CurrentStep: "domain", NextStepSchema: toolargs.SchemaFor[DomainInput]()}

	spec := rePresentFormOnFailure("sess-9", resp, errors.New("domain is required"), now)
	require.NotNil(t, spec, "a step that still needs input must re-present the form")
	require.Contains(t, spec.Message, "Step 'domain' needs input", "message must name the step")
	require.Contains(t, spec.Message, "domain is required", "message must carry the validation error")
	// The session id still rides the signed requestState.
	got, err := session.VerifyWizardRequestState(spec.RequestState, now)
	require.NoError(t, err)
	require.Equal(t, "sess-9", got)

	require.Nil(t, rePresentFormOnFailure("sess-9", StepResponse{CurrentStep: "dns_setup", NextStepSchema: toolargs.SchemaFor[NoInput]()}, errors.New("x"), now), "NoInput steps must not re-present a form")
}
