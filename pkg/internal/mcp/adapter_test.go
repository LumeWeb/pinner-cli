// Tests adapted from github.com/thepwagner/urfave-cli-mcp (command_test.go).
// Original source: https://github.com/thepwagner/urfave-cli-mcp
// Original license: MIT.
//
// Extended with tests for additional flag types (Float, Duration,
// StringSlice) that were added in this internal package.
//
// Updated for progressive disclosure: tools are no longer listed directly
// in tools/list. Instead, 3 meta-tools (search_tools, describe_tool,
// invoke_tool) provide discovery and invocation.
package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

func TestMCPCommand(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
	}

	cmd := mcpadapter.MCPCommand(root, nil, nil)
	assert.Equal(t, "mcp", cmd.Name)
	assert.NotEmpty(t, cmd.Usage)
}

// setupTestServer builds an MCP server with progressive disclosure from a
// command tree, initializes a client, and returns the ready client.
func setupTestServer(t *testing.T, root *cli.Command, hasRootAction bool) (*client.Client, *mcpadapter.ToolCatalog) {
	t.Helper()
	srv, catalog, err := mcpadapter.MCPServer(root, hasRootAction)
	require.NoError(t, err)

	// Register meta-tools so the client can discover and invoke tools.
	mcpadapter.RegisterMetaTools(srv, catalog)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)

	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	return c, catalog
}

// callTool invokes a meta-tool by name with the given arguments.
func callTool(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

// searchTools calls the search_tools meta-tool and returns the result.
func searchTools(t *testing.T, c *client.Client, query string) map[string]any {
	t.Helper()
	raw := callTool(t, c, "search_tools", map[string]any{"query": query})
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	return result
}

// describeTool calls the describe_tool meta-tool and returns the result.
func describeTool(t *testing.T, c *client.Client, name string) map[string]any {
	t.Helper()
	raw := callTool(t, c, "describe_tool", map[string]any{"name": name})
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	return result
}

// invokeTool calls the invoke_tool meta-tool and returns the result text.
func invokeTool(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	return callTool(t, c, "invoke_tool", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

func TestMetaTools_ListOnlyThreeMetaTools(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)

	assert.Len(t, tools.Tools, 3)
	names := make([]string, 3)
	for i, tool := range tools.Tools {
		names[i] = tool.Name
	}
	assert.Contains(t, names, "search_tools")
	assert.Contains(t, names, "describe_tool")
	assert.Contains(t, names, "invoke_tool")
}

func TestSearchTools_EmptyQueryReturnsAll(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{Name: "sub", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	c, _ := setupTestServer(t, root, true)

	result := searchTools(t, c, "")
	tools := result["tools"].([]any)
	assert.Equal(t, float64(2), result["total"])
	assert.Len(t, tools, 2)

	// Verify summaries have names, descriptions, and categories — but no inputSchema.
	for _, raw := range tools {
		summary := raw.(map[string]any)
		assert.NotEmpty(t, summary["name"])
		_, hasSchema := summary["inputSchema"]
		assert.False(t, hasSchema, "search results should not include inputSchema")
	}
}

func TestSearchTools_KeywordMatch(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{Name: "upload", Usage: "upload files", Action: func(context.Context, *cli.Command) error { return nil }},
			{Name: "download", Usage: "download content", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	c, _ := setupTestServer(t, root, true)

	result := searchTools(t, c, "upload")
	tools := result["tools"].([]any)
	assert.Len(t, tools, 1)
	summary := tools[0].(map[string]any)
	assert.Equal(t, "test_upload", summary["name"])
	assert.Equal(t, "upload files", summary["description"])
}

func TestSearchTools_SubsequenceMatch(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{Name: "upload", Usage: "upload files", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	c, _ := setupTestServer(t, root, true)

	// "pload" is a subsequence of "test_upload" but not a substring.
	result := searchTools(t, c, "pload")
	tools := result["tools"].([]any)
	assert.Len(t, tools, 1)
	summary := tools[0].(map[string]any)
	assert.Equal(t, "test_upload", summary["name"])
}

func TestSearchTools_CategoryFilter(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{
				Name:   "admin",
				Action: func(context.Context, *cli.Command) error { return nil },
				Commands: []*cli.Command{
					{Name: "billing", Action: func(context.Context, *cli.Command) error { return nil }},
				},
			},
			{Name: "upload", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	c, _ := setupTestServer(t, root, true)

	// Search with category=core — should exclude admin tools.
	req := mcp.CallToolRequest{}
	req.Params.Name = "search_tools"
	req.Params.Arguments = map[string]any{"query": "", "category": "core"}
	callResult, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, callResult.Content, 1)
	text, ok := callResult.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var coreResult map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &coreResult))
	coreTools := coreResult["tools"].([]any)
	for _, raw := range coreTools {
		summary := raw.(map[string]any)
		assert.NotEqual(t, "admin", summary["category"], "admin tools should be excluded from core category filter")
	}

	// Now search with category=admin.
	req.Params.Arguments = map[string]any{"query": "", "category": "admin"}
	callResult, err = c.CallTool(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, callResult.Content, 1)
	text, ok = callResult.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var filtered map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &filtered))
	adminTools := filtered["tools"].([]any)
	assert.Len(t, adminTools, 2)
	for _, raw := range adminTools {
		summary := raw.(map[string]any)
		assert.Equal(t, "admin", summary["category"])
		assert.Contains(t, summary["name"].(string), "admin")
	}
}

func TestDescribeTool_ReturnsFullSchema(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:    "test",
		Version: "1.0.0",
		Action:  func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{
				Name:        "sub",
				Description: "do a sub test",
				Flags: []cli.Flag{
					&cli.Int64Flag{
						Name:     "target",
						Usage:    "submarine to target",
						Value:    688,
						Required: true,
					},
					&cli.BoolFlag{
						Name:   "hidden",
						Usage:  "hidden flag",
						Hidden: true,
					},
				},
				Action: func(context.Context, *cli.Command) error { return nil },
			},
		},
	}
	c, _ := setupTestServer(t, root, true)

	detail := describeTool(t, c, "test_sub")
	assert.Equal(t, "test_sub", detail["name"])
	assert.Equal(t, "do a sub test", detail["description"])

	// InputSchema should be present as a JSON object.
	schema, ok := detail["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be present")
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	targetProp := props["target"].(map[string]any)
	assert.Equal(t, "number", targetProp["type"])
	assert.Equal(t, "submarine to target", targetProp["description"])
	assert.Equal(t, float64(688), targetProp["default"])

	required, ok := schema["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "target")

	// Hidden flag should not appear.
	_, hasHidden := props["hidden"]
	assert.False(t, hasHidden, "hidden flag should not be in schema")
}

func TestDescribeTool_UnknownTool(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	req := mcp.CallToolRequest{}
	req.Params.Name = "describe_tool"
	req.Params.Arguments = map[string]any{"name": "nonexistent"}
	result, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestInvokeTool_ExecutesCommand(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "target"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Fprintf(cmd.Root().Writer, "target=%s", cmd.String("target"))
			return nil
		},
	}
	c, _ := setupTestServer(t, root, true)

	// Verify the tool exists in the catalog.
	detail := describeTool(t, c, "test")
	assert.Equal(t, "test", detail["name"])

	// Invoke it.
	result := invokeTool(t, c, "test", map[string]any{"target": "689"})
	assert.Equal(t, "target=689", result)
}

func TestInvokeTool_Subcommand(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name: "sub",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					fmt.Fprintf(cmd.Root().Writer, "foo bar sub")
					return nil
				},
			},
		},
	}
	c, _ := setupTestServer(t, root, false)

	toolName := strings.Join([]string{"test", "sub"}, mcpadapter.ToolDelimiter)
	result := invokeTool(t, c, toolName, map[string]any{})
	assert.Equal(t, "foo bar sub", result)
}

func TestInvokeTool_UnknownTool(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	req := mcp.CallToolRequest{}
	req.Params.Name = "invoke_tool"
	req.Params.Arguments = map[string]any{"name": "nonexistent", "arguments": map[string]any{}}
	result, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestProgressiveDisclosure_HidesHiddenCommands(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{Name: "visible", Action: func(context.Context, *cli.Command) error { return nil }},
			{Name: "mcp", Action: func(context.Context, *cli.Command) error { return nil }},
			{Name: "hidden", Hidden: true, Action: func(context.Context, *cli.Command) error { return nil }},
			{Name: "help", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	c, _ := setupTestServer(t, root, true)

	// tools/list should only show 3 meta-tools.
	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	assert.Len(t, tools.Tools, 3)

	// But search_tools should find the visible commands (test, test_visible).
	result := searchTools(t, c, "")
	toolList := result["tools"].([]any)
	assert.Equal(t, float64(2), result["total"])

	// Verify hidden/mcp/help commands are NOT in the catalog.
	for _, raw := range toolList {
		summary := raw.(map[string]any)
		name := summary["name"].(string)
		assert.NotContains(t, name, "mcp")
		assert.NotContains(t, name, "hidden")
		assert.NotContains(t, name, "help")
	}
}

// --- Extended tests for additional flag types (via describe_tool) ---

func TestDescribeTool_FloatFlag(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.FloatFlag{
				Name:     "price",
				Usage:    "price in dollars",
				Value:    9.99,
				Required: true,
			},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	detail := describeTool(t, c, "test")
	schema := detail["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	prop := props["price"].(map[string]any)
	assert.Equal(t, "number", prop["type"])
	assert.Equal(t, "price in dollars", prop["description"])
	assert.Equal(t, float64(9.99), prop["default"])
	assert.Contains(t, schema["required"].([]any), "price")
}

func TestDescribeTool_DurationFlag(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "request timeout",
				Value: 5_000_000_000, // 5s in nanoseconds
			},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	detail := describeTool(t, c, "test")
	schema := detail["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	prop := props["timeout"].(map[string]any)
	assert.Equal(t, "string", prop["type"])
	assert.Contains(t, prop["description"].(string), "duration")
}

func TestDescribeTool_StringSliceFlag(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:     "tags",
				Usage:    "tags for the resource",
				Required: true,
			},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	detail := describeTool(t, c, "test")
	schema := detail["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	prop := props["tags"].(map[string]any)
	assert.Equal(t, "string", prop["type"])
	assert.Contains(t, prop["description"].(string), "comma-separated")
	assert.Contains(t, schema["required"].([]any), "tags")
}

func TestDescribeTool_VersionFlagSkipped(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:   "version",
				Usage:  "print version",
				Hidden: false,
			},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	detail := describeTool(t, c, "test")
	schema := detail["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	_, hasVersion := props["version"]
	assert.False(t, hasVersion, "version flag should be skipped")
}

func TestInvoketool_PositionalArgs(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Fprintf(cmd.Root().Writer, "args=%v", cmd.Args().Slice())
			return nil
		},
	}
	c, _ := setupTestServer(t, root, true)

	result := invokeTool(t, c, "test", map[string]any{
		"_args": []any{"hello", "world"},
	})
	assert.Equal(t, "args=[hello world]", result)
}

func TestInvoketool_PositionalArgsWithFlags(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mode"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Fprintf(cmd.Root().Writer, "mode=%s args=%v", cmd.String("mode"), cmd.Args().Slice())
			return nil
		},
	}
	c, _ := setupTestServer(t, root, true)

	result := invokeTool(t, c, "test", map[string]any{
		"mode":  "fast",
		"_args": []any{"file1", "file2"},
	})
	assert.Equal(t, "mode=fast args=[file1 file2]", result)
}

func TestInvoketool_PositionalArgsEmpty(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name: "test",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Fprintf(cmd.Root().Writer, "count=%d", cmd.Args().Len())
			return nil
		},
	}
	c, _ := setupTestServer(t, root, true)

	result := invokeTool(t, c, "test", map[string]any{})
	assert.Equal(t, "count=0", result)
}

func TestInvoketool_PositionalArgsNonString(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	c, _ := setupTestServer(t, root, true)

	req := mcp.CallToolRequest{}
	req.Params.Name = "invoke_tool"
	req.Params.Arguments = map[string]any{
		"name":      "test",
		"arguments": map[string]any{"_args": []any{123}},
	}
	result, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.IsError)
}
