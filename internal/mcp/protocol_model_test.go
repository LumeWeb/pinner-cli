package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

func TestDescriptorFromToolPreservesCatalogContract(t *testing.T) {
	entry := &model.ToolEntry{
		Name:        "pinner_status",
		Title:       "Status",
		Description: "Read status",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"json":{"type":"boolean"}}}`),
		ReadOnly:    true,
		Destructive: false,
	}

	descriptor := model.DescriptorFromTool(entry)
	require.Equal(t, entry.Name, descriptor.Name)
	require.Equal(t, entry.Title, descriptor.Title)
	require.Equal(t, entry.Description, descriptor.Description)
	require.JSONEq(t, string(entry.InputSchema), string(descriptor.InputSchema))
	require.True(t, descriptor.ReadOnly)
	require.False(t, descriptor.Destructive)
}

func TestToolDescriptorHandlerContract(t *testing.T) {
	handler := model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: request.Name + ":" + request.Arguments["value"].(string)}, nil
	})

	result, err := handler(context.Background(), model.ToolRequest{Name: "echo", Arguments: map[string]any{"value": "ok"}})
	require.NoError(t, err)
	require.Equal(t, "echo:ok", result.Text)
}

func TestResourceAndPromptDescriptorContracts(t *testing.T) {
	resource := model.ResourceDescriptor{
		URI:      "pinner://account/status",
		MIMEType: "application/json",
		Handler: func(_ context.Context, request model.ResourceRequest) (model.ResourceResult, error) {
			return model.ResourceResult{URI: request.URI, MIMEType: "application/json", Text: "{}"}, nil
		},
	}
	result, err := resource.Handler(context.Background(), model.ResourceRequest{URI: resource.URI})
	require.NoError(t, err)
	require.Equal(t, resource.URI, result.URI)

	prompt := model.PromptDescriptor{
		Name:      "setup",
		Arguments: []model.PromptArgumentDescriptor{{Name: "domain", Required: false}},
		Handler: func(_ context.Context, request model.PromptRequest) (model.PromptResult, error) {
			return model.PromptResult{Messages: []model.PromptMessage{{Role: "user", Text: request.Arguments["domain"]}}}, nil
		},
	}
	promptResult, err := prompt.Handler(context.Background(), model.PromptRequest{Arguments: map[string]string{"domain": "example.com"}})
	require.NoError(t, err)
	require.Equal(t, "example.com", promptResult.Messages[0].Text)
}
