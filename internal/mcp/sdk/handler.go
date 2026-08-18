package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// This file owns the tool-handler adapter chain: it turns a Pinner-owned
// model.PinnerToolHandler plus the request/response conversion into the
// go-sdk mcp.ToolHandler the server registers. Keeping it here (and not in the
// mcp package) is what makes sdk the only package that imports the SDK's mcp
// types for production code; the hub passes its behaviors in and gets a
// registered tool back.

// HandlerDeps carries the hub-specific behaviors the adapter needs. They are
// passed explicitly per registration so sdk stays stateless and the hub keeps
// ownership of its logging, app-annotation, and capability code.
type HandlerDeps struct {
	// RequestCaps builds the SDK-neutral per-request capability view from an
	// incoming call-tool request.
	RequestCaps func(req *CallToolRequest) *model.RequestCaps
	// ReservedRequestStateKey is the reserved input key the hub's catalog
	// uses for the client echo-back state; set to "" to skip the echo.
	ReservedRequestStateKey string
	// LogStart optional hook called before invoking the Pinner handler.
	LogStart func(name string, args map[string]any)
	// LogEnd optional hook called after invoking the Pinner handler.
	LogEnd func(name string, startedAt time.Time, result model.ToolResult, err error)
	// AnnotateApp optional hook that appends companion-app context to a
	// needs_human result.
	AnnotateApp func(toolName string, caps *model.RequestCaps, result *model.ToolResult)
}

type (
	// CallToolRequest and CallToolResult are the go-sdk request/result aliases
	// the adapter reads and writes. They are re-exported here so the hub never
	// imports the SDK's mcp package directly.
	CallToolRequest = mcp.CallToolRequest
	CallToolResult  = mcp.CallToolResult
	ToolHandler     = mcp.ToolHandler
	Content         = mcp.Content
	TextContent     = mcp.TextContent
)

// ToolResult converts a Pinner-owned tool result into an SDK result,
// preserving isError and single text-content semantics. When the result
// carries an Elicitation it is converted into an input_required response.
func ToolResult(result model.ToolResult) *CallToolResult {
	if result.Elicitation != nil {
		return CallToolResultFromElicitation(*result.Elicitation)
	}
	return &CallToolResult{
		IsError:           result.IsError,
		Content:           []Content{&TextContent{Text: result.Text}},
		StructuredContent: result.StructuredContent,
	}
}

// AdaptToolHandler wraps a Pinner-owned handler into the SDK tool handler the
// server registers. The arguments arrive on the wire as raw JSON; they are
// unmarshalled into a plain map for the Pinner-owned handler. When the call is
// a retry after an input_required elicitation, the accepted form content is
// merged into the arguments under their elicitation id.
func AdaptToolHandler(d HandlerDeps, handler model.PinnerToolHandler) ToolHandler {
	return func(ctx context.Context, req *CallToolRequest) (*CallToolResult, error) {
		args := map[string]any{}
		if req.Params.Arguments != nil {
			// Decode with UseNumber so JSON integers arrive as json.Number
			// instead of float64. Plain json.Unmarshal maps an integer to
			// float64, which silently loses precision for any value above
			// 2^53; for an id like ipns_keys_get/delete's that could address
			// (or with delete, remove) the wrong key. json.Number is exact,
			// and the catalog normalizer converts it losslessly.
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.UseNumber()
			if err := dec.Decode(&args); err != nil {
				return &CallToolResult{
					IsError: true,
					Content: []Content{&TextContent{Text: fmt.Sprintf("invalid arguments: %v", err)}},
				}, nil
			}
		}
		for id, content := range AcceptedElicitations(req) {
			args[id] = content
		}
		// Recover cross-round state the client echoed back on a retry after an
		// input_required result, so handlers can re-establish context (e.g. a
		// session id) even if the client did not echo the original arguments.
		if d.ReservedRequestStateKey != "" && req.Params.RequestState != "" {
			if _, ok := args[d.ReservedRequestStateKey]; !ok {
				args[d.ReservedRequestStateKey] = req.Params.RequestState
			}
		}
		var caps *model.RequestCaps
		if d.RequestCaps != nil {
			caps = d.RequestCaps(req)
		}
		startedAt := time.Now()
		if d.LogStart != nil {
			d.LogStart(req.Params.Name, args)
		}
		result, err := handler(ctx, model.ToolRequest{
			Name:           req.Params.Name,
			Arguments:      args,
			InputResponses: len(req.Params.InputResponses) > 0,
			Caps:           caps,
		})
		if d.LogEnd != nil {
			d.LogEnd(req.Params.Name, startedAt, result, err)
		}
		if err != nil {
			return &CallToolResult{
				IsError: true,
				Content: []Content{&TextContent{Text: err.Error()}},
			}, nil
		}
		if d.AnnotateApp != nil {
			d.AnnotateApp(req.Params.Name, caps, &result)
		}
		return ToolResult(result), nil
	}
}

// RegisterTool registers one Pinner-owned tool on the server, adapting its
// handler via AdaptToolHandler. A nil desc.Handler registers a tool whose
// invocation will fail at call time, mirroring the prior hub behavior; callers
// that need a strict guard check it beforehand (see RegisterOfficialDescriptor).
func RegisterTool(srv *Server, deps HandlerDeps, desc model.ToolDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil server")
	}
	srv.AddTool(Tool(desc), AdaptToolHandler(deps, desc.Handler))
	return nil
}
