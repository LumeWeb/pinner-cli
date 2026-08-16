package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadFileAvailable(t *testing.T) {
	t.Run("co-located registers when the local-path handler is wired", func(t *testing.T) {
		require.True(t, uploadFileAvailable(true, true, false, false))
		require.False(t, uploadFileAvailable(true, false, false, false))
	})

	t.Run("remote requires both the coordinator and a reachable HTTP mux", func(t *testing.T) {
		// Plain HTTP / ngrok / cloudflared: mux mounted, remote upload_file OK.
		require.True(t, uploadFileAvailable(false, false, true, true))
		// No coordinator wired: nothing to mint.
		require.False(t, uploadFileAvailable(false, false, false, true))
		// Embedded openai tunnel: only MCP RPC, no reachable HTTP mux — the
		// minted presigned PUT would be an unreachable loopback URL.
		require.False(t, uploadFileAvailable(false, false, true, false))
	})
}
