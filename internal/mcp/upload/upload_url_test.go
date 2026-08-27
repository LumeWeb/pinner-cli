package upload

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

func TestRelayURLUploadDescriptorRequiresURL(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "url is required")
}

// TestRelayURLUploadDescriptionProfileForbid regresses the cross-host leak:
// upload_url must be unconditionally forbidden on a host without a URL-fetch
// relay (Grok), not silently usable — a Grok model should mint+PUT, not invent
// a URL it cannot have the server fetch on its behalf.
func TestRelayURLUploadDescriptionProfileForbid(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)

	grok, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.Contains(t, grok, "Do NOT call this tool on this host")
	require.Contains(t, grok, "upload_file(source.mode=mint)")

	openai, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileOpenAITunnel)
	require.True(t, ok)
	require.NotContains(t, openai, "Do NOT call this tool on this host", "OpenAI tunnel keeps upload_url usable")
}

func TestRelayURLUploadDescriptorRejectsNonHTTPS(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"url": "http://example.com/x"}})
	require.ErrorContains(t, err, "HTTPS")
}
