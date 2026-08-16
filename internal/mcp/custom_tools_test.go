package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadFileAvailable(t *testing.T) {
	t.Run("co-located registers when the local-path handler is wired", func(t *testing.T) {
		require.True(t, uploadFileAvailable(true, true, false, false, false))
		require.False(t, uploadFileAvailable(true, false, false, false, false))
	})

	t.Run("HTTP requires both the coordinator and a reachable HTTP mux", func(t *testing.T) {
		// Plain HTTP / ngrok / cloudflared: mux mounted, remote upload_file OK.
		require.True(t, uploadFileAvailable(false, false, true, false, true))
		// No coordinator wired: nothing to mint.
		require.False(t, uploadFileAvailable(false, false, false, false, true))
	})

	t.Run("openai tunnel requires the relay executor (no reachable mux)", func(t *testing.T) {
		// Embedded openai tunnel: only MCP RPC, no reachable HTTP mux — mint
		// (curl) is impossible, but a wired relay executor (url/data) works.
		require.True(t, uploadFileAvailable(false, false, true, true, false))
		require.True(t, uploadFileAvailable(false, false, false, true, false))
		// No relay executor wired on the openai tunnel: nothing works.
		require.False(t, uploadFileAvailable(false, false, true, false, false))
	})

	t.Run("HTTP does not need a relay executor", func(t *testing.T) {
		// curl path is independent of the relay executor.
		require.True(t, uploadFileAvailable(false, false, true, false, true))
	})
}
