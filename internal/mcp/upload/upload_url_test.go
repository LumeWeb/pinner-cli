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

// TestRelayURLUploadDescriptionProfileRegisters pins the cross-host gating
// design: upload_url is registered (and advertised with its usable copy) for
// any host whose feature set declares FeatSourceURL — including Grok, which
// supports the server-fetch URL relay. It must NOT be forbidden for Grok in
// prose. A host WITHOUT FeatSourceURL (generic HTTP) omits the tool entirely
// at registration rather than advertising a forbidden tool.
func TestRelayURLUploadDescriptionProfileRegisters(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)

	grok, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.NotContains(t, grok, "This transport has no URL-fetch relay", "Grok declares FeatSourceURL so upload_url is usable for it")

	genericHTTP, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileHTTPGeneric)
	require.True(t, ok)
	require.Contains(t, genericHTTP, "This transport has no URL-fetch relay")

	openai, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileOpenAITunnel)
	require.True(t, ok)
	require.NotContains(t, openai, "This transport has no URL-fetch relay", "OpenAI tunnel keeps upload_url usable")
}

func TestRelayURLUploadDescriptorRejectsNonHTTPS(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"url": "http://example.com/x"}})
	require.ErrorContains(t, err, "HTTPS")
}
