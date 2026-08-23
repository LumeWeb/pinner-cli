package upload

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

func TestRelayURLUploadDescriptorRequiresURL(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "url is required")
}

func TestRelayURLUploadDescriptorRejectsNonHTTPS(t *testing.T) {
	desc := RelayURLUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"url": "http://example.com/x"}})
	require.ErrorContains(t, err, "HTTPS")
}
