package sdk

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// Prompt converts a Pinner-owned prompt descriptor into an official SDK
// prompt.
func Prompt(desc model.PromptDescriptor) *mcp.Prompt {
	p := &mcp.Prompt{
		Name:        desc.Name,
		Title:       desc.Title,
		Description: desc.Description,
	}
	for _, a := range desc.Arguments {
		p.Arguments = append(p.Arguments, &mcp.PromptArgument{
			Name:        a.Name,
			Title:       a.Title,
			Description: a.Description,
			Required:    a.Required,
		})
	}
	return p
}

// messageContent converts a Pinner-owned prompt message into official SDK
// content (text or embedded resource), preserving role and text verbatim.
func messageContent(msg model.PromptMessage) (mcp.Role, mcp.Content) {
	if msg.EmbeddedResource != nil {
		return mcp.Role(msg.Role), &mcp.EmbeddedResource{
			Resource: &mcp.ResourceContents{
				URI:      msg.EmbeddedResource.URI,
				MIMEType: msg.EmbeddedResource.MIMEType,
				Text:     msg.EmbeddedResource.Text,
			},
		}
	}
	return mcp.Role(msg.Role), &mcp.TextContent{Text: msg.Text}
}

// promptHandler adapts a Pinner-owned prompt handler to the official SDK
// prompt-handler shape. The official SDK delivers arguments as
// map[string]string for downstream command execution.
func promptHandler(handler func(context.Context, model.PromptRequest) (model.PromptResult, error)) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		result, err := handler(ctx, model.PromptRequest{Arguments: req.Params.Arguments})
		if err != nil {
			return nil, err
		}
		out := &mcp.GetPromptResult{Description: result.Description}
		for _, m := range result.Messages {
			role, content := messageContent(m)
			out.Messages = append(out.Messages, &mcp.PromptMessage{Role: role, Content: content})
		}
		return out, nil
	}
}

// RegisterPrompts registers prompt templates on an official-SDK server.
func RegisterPrompts(srv *mcp.Server, prompts []model.PromptDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	for _, p := range prompts {
		srv.AddPrompt(Prompt(p), promptHandler(p.Handler))
	}
	return nil
}
