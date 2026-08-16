package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalPathVaultPutDescriptorRequiresPath(t *testing.T) {
	desc := LocalPathVaultPutDescriptor(func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		return nil, nil
	})
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "path is required")
}

func TestLocalPathVaultPutDescriptorRequiresVaultPath(t *testing.T) {
	desc := LocalPathVaultPutDescriptor(func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		return nil, nil
	})
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"path": "/tmp/x"}})
	require.ErrorContains(t, err, "vault_path is required")
}

func TestLocalPathVaultPutDescriptorNotConfigured(t *testing.T) {
	desc := LocalPathVaultPutDescriptor(nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"path": "/tmp/x", "vault_path": "vault:/docs"}})
	require.ErrorContains(t, err, "local path vault handler is not configured")
}

func TestLocalPathVaultPutDescriptorCallsHandler(t *testing.T) {
	var gotPath, gotVaultPath, gotMode string
	result := map[string]any{"base": "vault:/docs", "total": 2}
	desc := LocalPathVaultPutDescriptor(func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		gotPath = path
		gotVaultPath = vaultPath
		gotMode = archiveMode
		return result, nil
	})
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"path":         "/host/abs/file.bin",
		"vault_path":   "vault:/docs",
		"archive_mode": "preserve",
	}})
	require.NoError(t, err)
	require.Equal(t, "/host/abs/file.bin", gotPath)
	require.Equal(t, "vault:/docs", gotVaultPath)
	require.Equal(t, "preserve", gotMode)
	require.Equal(t, result, res.StructuredContent)
	require.Equal(t, "Stored in the vault.", res.Text)
	require.False(t, res.IsError)
}
