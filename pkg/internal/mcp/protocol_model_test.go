package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescriptorFromToolPreservesCatalogContract(t *testing.T) {
	entry := &ToolEntry{
		Name:        "pinner_status",
		Title:       "Status",
		Description: "Read status",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"json":{"type":"boolean"}}}`),
		ReadOnly:    true,
		Destructive: false,
	}

	descriptor := descriptorFromTool(entry)
	require.Equal(t, entry.Name, descriptor.Name)
	require.Equal(t, entry.Title, descriptor.Title)
	require.Equal(t, entry.Description, descriptor.Description)
	require.JSONEq(t, string(entry.InputSchema), string(descriptor.InputSchema))
	require.True(t, descriptor.ReadOnly)
	require.False(t, descriptor.Destructive)
}

func TestToolDescriptorHandlerContract(t *testing.T) {
	handler := PinnerToolHandler(func(_ context.Context, request ToolRequest) (ToolResult, error) {
		return ToolResult{Text: request.Name + ":" + request.Arguments["value"].(string)}, nil
	})

	result, err := handler(context.Background(), ToolRequest{Name: "echo", Arguments: map[string]any{"value": "ok"}})
	require.NoError(t, err)
	require.Equal(t, "echo:ok", result.Text)
}

func TestResourceAndPromptDescriptorContracts(t *testing.T) {
	resource := ResourceDescriptor{
		URI:      "pinner://account/status",
		MIMEType: "application/json",
		Handler: func(_ context.Context, request ResourceRequest) (ResourceResult, error) {
			return ResourceResult{URI: request.URI, MIMEType: "application/json", Text: "{}"}, nil
		},
	}
	result, err := resource.Handler(context.Background(), ResourceRequest{URI: resource.URI})
	require.NoError(t, err)
	require.Equal(t, resource.URI, result.URI)

	prompt := PromptDescriptor{
		Name:      "setup",
		Arguments: []PromptArgumentDescriptor{{Name: "domain", Required: false}},
		Handler: func(_ context.Context, request PromptRequest) (PromptResult, error) {
			return PromptResult{Messages: []PromptMessage{{Role: "user", Text: request.Arguments["domain"]}}}, nil
		},
	}
	promptResult, err := prompt.Handler(context.Background(), PromptRequest{Arguments: map[string]string{"domain": "example.com"}})
	require.NoError(t, err)
	require.Equal(t, "example.com", promptResult.Messages[0].Text)
}
