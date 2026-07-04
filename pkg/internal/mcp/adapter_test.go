// Tests adapted from github.com/thepwagner/urfave-cli-mcp (command_test.go).
// Original source: https://github.com/thepwagner/urfave-cli-mcp
// Original license: MIT.
//
// Extended with tests for additional flag types (Float, Duration,
// StringSlice) that were added in this internal package.
package mcp_test

import (
	"context"
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

func TestMCPCommandServer(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:    "test",
		Usage:   "do a test",
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)

	initResult, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	assert.Equal(t, "test", initResult.ServerInfo.Name)
	assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)
	assert.NotNil(t, initResult.Capabilities.Tools)
	assert.Nil(t, initResult.Capabilities.Resources)
	assert.Nil(t, initResult.Capabilities.Prompts)
	assert.Nil(t, initResult.Capabilities.Logging)
	assert.Nil(t, initResult.Capabilities.Experimental)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	if assert.Len(t, tools.Tools, 2) {
		assert.Equal(t, "test", tools.Tools[0].Name)
		assert.Equal(t, "do a test", tools.Tools[0].Description)
		assert.Empty(t, tools.Tools[0].InputSchema.Properties)
		assert.Empty(t, tools.Tools[0].InputSchema.Required)

		assert.Equal(t, "test_sub", tools.Tools[1].Name)
		assert.Equal(t, "do a sub test", tools.Tools[1].Description)
		assert.Equal(t, map[string]any{
			"type":        "number",
			"description": "submarine to target",
			"default":     float64(688),
		}, tools.Tools[1].InputSchema.Properties["target"])
		assert.Equal(t, []string{"target"}, tools.Tools[1].InputSchema.Required)
	}
}

func TestMCPCommandServer_CallTool(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	req := mcp.CallToolRequest{}
	req.Params.Name = "test"
	req.Params.Arguments = map[string]any{"target": "689"}
	callResult, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	assert.Len(t, callResult.Content, 1)
	content, ok := callResult.Content[0].(mcp.TextContent)
	assert.True(t, ok)

	// The Action writes to the command's writer, which the in-process handler
	// captures into a buffer and returns as the tool result.
	assert.Equal(t, "target=689", content.Text)
}

func TestMCPCommandServer_CallTool_Subcommand(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, false)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	req := mcp.CallToolRequest{}
	req.Params.Name = strings.Join([]string{"test", "sub"}, mcpadapter.ToolDelimiter)

	callResult, err := c.CallTool(t.Context(), req)
	require.NoError(t, err)
	assert.Len(t, callResult.Content, 1)
	content, ok := callResult.Content[0].(mcp.TextContent)
	assert.True(t, ok)

	// The subcommand's Action writes to the command's writer, which the
	// in-process handler captures and returns.
	assert.Equal(t, "foo bar sub", content.Text)
}

func TestMCPCommandServer_IgnoresHiddenCommandsAndSubcommands(t *testing.T) {
	t.Parallel()

	root := &cli.Command{
		Name:   "test",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{
				Name:   "visible",
				Usage:  "a visible command",
				Action: func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name:   "mcp",
				Usage:  "should be hidden",
				Action: func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name:   "hidden",
				Usage:  "should be hidden",
				Hidden: true,
				Action: func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name:   "help",
				Usage:  "should be hidden",
				Action: func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name:   "parent",
				Usage:  "parent with hidden subcommands",
				Action: func(context.Context, *cli.Command) error { return nil },
				Commands: []*cli.Command{
					{
						Name:   "visible-sub",
						Usage:  "a visible subcommand",
						Action: func(context.Context, *cli.Command) error { return nil },
					},
					{
						Name:   "mcp",
						Action: func(context.Context, *cli.Command) error { return nil },
					},
					{
						Name:   "hidden",
						Usage:  "hidden subcommand",
						Hidden: true,
						Action: func(context.Context, *cli.Command) error { return nil },
					},
					{
						Name:   "help",
						Usage:  "hidden subcommand",
						Action: func(context.Context, *cli.Command) error { return nil },
					},
				},
			},
		},
	}

	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)

	expectedTools := []string{"test", "test_visible", "test_parent", "test_parent_visible-sub"}

	assert.Len(t, tools.Tools, len(expectedTools))
	toolNames := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolNames = append(toolNames, tool.Name)
	}

	for _, expected := range expectedTools {
		assert.Contains(t, toolNames, expected)
	}
}

// --- Extended tests for additional flag types ---

func TestMCPCommandServer_FloatFlag(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)

	prop := tools.Tools[0].InputSchema.Properties["price"].(map[string]any)
	assert.Equal(t, "number", prop["type"])
	assert.Equal(t, "price in dollars", prop["description"])
	assert.Equal(t, float64(9.99), prop["default"])
	assert.Contains(t, tools.Tools[0].InputSchema.Required, "price")
}

func TestMCPCommandServer_DurationFlag(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)

	prop := tools.Tools[0].InputSchema.Properties["timeout"].(map[string]any)
	assert.Equal(t, "string", prop["type"])
	assert.Contains(t, prop["description"].(string), "duration")
	assert.Contains(t, prop["description"].(string), "5m")
	// Duration is optional by default
	assert.NotContains(t, tools.Tools[0].InputSchema.Required, "timeout")
}

func TestMCPCommandServer_StringSliceFlag(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)

	prop := tools.Tools[0].InputSchema.Properties["tags"].(map[string]any)
	assert.Equal(t, "string", prop["type"])
	assert.Contains(t, prop["description"].(string), "comma-separated")
	assert.Contains(t, tools.Tools[0].InputSchema.Required, "tags")
}

func TestMCPCommandServer_VersionFlagSkipped(t *testing.T) {
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
	srv, err := mcpadapter.MCPServer(root, true)
	assert.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)

	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)

	for propName := range tools.Tools[0].InputSchema.Properties {
		assert.NotEqual(t, "version", propName, "version flag should be skipped")
	}
}
