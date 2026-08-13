package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentGuideDescriptor(t *testing.T) {
	desc := NewAgentGuideDescriptor()
	require.Equal(t, "agent_guide", desc.Name)
	require.Equal(t, CategoryCore, desc.Category)

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)

	guid, ok := res.StructuredContent.(AgentGuide)
	require.True(t, ok, "StructuredContent must be an AgentGuide")
	require.NotEmpty(t, guid.Summary)
	require.Len(t, guid.Flows, 4, "guide must cover the four primary flows")

	names := make([]string, 0, len(guid.Flows))
	for _, f := range guid.Flows {
		names = append(names, f.Name)
		require.NotEmpty(t, f.Title)
		require.GreaterOrEqual(t, len(f.Steps), 3, "each flow must list an ordered tool chain: %s", f.Name)
	}
	for _, want := range []string{"auth", "vault_create", "vault_restore", "pins"} {
		require.Contains(t, names, want)
	}

	// Serializes cleanly (structured content reaches the wire as JSON).
	_, err = json.Marshal(guid)
	require.NoError(t, err)
}

func TestAgentGuideDescriptorIsDirectVisible(t *testing.T) {
	desc := NewAgentGuideDescriptor()
	tool := officialTool(desc)
	require.Equal(t, "agent_guide", tool.Name)
}
