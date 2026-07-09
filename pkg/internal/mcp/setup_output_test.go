package mcp_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

// TestMCPCommandServer_OutputNotLeaked_WithStaleCommandContext is a regression
// test for a bug where command output leaked to os.Stdout when the MCP server's
// context carried a stale commandContextKey from the outer root.Run().
//
// Root cause: urfave/cli v3 stores the running root command in the context via
// an unexported commandContextKey. When the MCP server's tool handler calls
// rootCopy.Run(ctx, ...), the inherited context causes urfave/cli to set
// rootCopy.parent = originalRoot, making cmd.Root() in subcommands resolve to
// the original root (Writer=os.Stdout) instead of rootCopy (Writer=buffer).
//
// This test reproduces the scenario by running the root once (simulating
// "pinner mcp" startup), capturing the context, then calling the tool handler
// with that context. Without the fix, cmd.Root().Writer resolves to os.Stdout
// and output is NOT captured in the MCP response.
func TestMCPCommandServer_OutputNotLeaked_WithStaleCommandContext(t *testing.T) {
	t.Parallel()

	// simulateOutput mirrors the real CLI's setupOutput -> Print chain:
	//   1. NewOutputFormatter defaults writer to os.Stdout
	//   2. Override with cmd.Root().Writer
	//   3. Write through the formatter
	simulateOutput := func(cmd *cli.Command, msg string) {
		// Step 1: default writer (os.Stdout in real code, Discard in tests)
		writer := io.Discard
		// Step 2: override with cmd.Root().Writer
		if rw := cmd.Root().Writer; rw != nil {
			writer = rw
		}
		// Step 3: write through the formatter
		fmt.Fprintln(writer, msg)
	}

	root := &cli.Command{
		Name: "pinner",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "agent"},
		},
		Commands: []*cli.Command{
			{
				Name: "auth",
				Commands: []*cli.Command{
					{
						Name: "status",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							simulateOutput(cmd, "Authentication status: authenticated")
							return nil
						},
					},
				},
			},
		},
	}

	// Step 1: Run the root once to populate commandContextKey in a context.
	// In the real binary, "pinner mcp" runs the root, and the mcp subcommand's
	// Action captures the context. We simulate this by running a capture command.
	var capturedCtx context.Context
	root.Commands = append(root.Commands, &cli.Command{
		Name: "__capture_ctx",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			capturedCtx = ctx
			return nil
		},
	})
	_ = root.Run(context.Background(), []string{"pinner", "__capture_ctx"})
	require.NotNil(t, capturedCtx, "context should be captured from first Run")

	// Step 2: Create the MCP server from the already-initialized root.
	srv, catalog, err := mcpadapter.MCPServer(root, false)
	require.NoError(t, err)
	_ = srv // meta-tools not needed for this test; we invoke via catalog

	// Step 3: Call the catalog handler directly with the captured context.
	// This simulates what happens when invoke_tool dispatches to the tool's
	// handler — the ctx comes from Listen(ctx, ...), which is the Action's
	// context containing the stale commandContextKey.
	entry, ok := catalog.Get("pinner_auth_status")
	require.True(t, ok, "tool should be in catalog")

	req := mcp.CallToolRequest{}
	req.Params.Name = "pinner_auth_status"

	result, err := entry.Handler(capturedCtx, req)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)

	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, "Authentication status: authenticated")
	assert.NotEmpty(t, content.Text,
		"output should be captured in MCP response, not leaked to stdout "+
			"(if empty, the commandContextKey from the outer Run is causing "+
			"cmd.Root() to resolve to the original root with os.Stdout)")
}
