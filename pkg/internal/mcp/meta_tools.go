package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterMetaTools registers the three meta-tools (search_tools,
// describe_tool, invoke_tool) on the MCP server. These are the only tools
// visible via standard tools/list. The real tool catalog is accessible only
// through these meta-tools, implementing server-side progressive disclosure.
//
// The MCP spec (2025-06-18) defines tools/list, tools/call, and
// notifications/tools/list_changed. There is no get_tool(name) primitive.
// Progressive disclosure is a design pattern layered on top of the spec via
// meta-tools, not a protocol feature. This implementation follows the pattern
// described by Solo.io/agentgateway: the server exposes a small fixed set of
// meta-tools while keeping the full catalog internal.
func RegisterMetaTools(srv *server.MCPServer, catalog *ToolCatalog) {
	registerSearchTools(srv, catalog)
	registerDescribeTool(srv, catalog)
	registerInvokeTool(srv, catalog)
}

// registerSearchTools registers the search_tools meta-tool.
func registerSearchTools(srv *server.MCPServer, catalog *ToolCatalog) {
	tool := mcp.NewTool("search_tools",
		mcp.WithDescription("Search the internal tool catalog by keyword. "+
			"Returns matching tool names, descriptions, and categories (without input schemas). "+
			"Use describe_tool to get the full input schema for a specific tool. "+
			"Leave query empty to list all available tools."),
		mcp.WithString("query",
			mcp.Description("Keywords to search for in tool names and descriptions. "+
				"Supports subsequence matching (e.g. 'pload' matches 'pinner_upload'). "+
				"Leave empty to return all tools."),
		),
		mcp.WithString("category",
			mcp.Description("Filter by category: 'core' (user commands), 'admin' (admin operations), 'wizard' (interactive wizards). "+
				"Leave empty to search all categories."),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		query, _ := args["query"].(string)
		category, _ := args["category"].(string)

		summaries := catalog.Search(query, category)

		result := map[string]any{
			"tools": summaries,
			"total": len(summaries),
		}

		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal search results: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(data))},
		}, nil
	})
}

// registerDescribeTool registers the describe_tool meta-tool.
func registerDescribeTool(srv *server.MCPServer, catalog *ToolCatalog) {
	tool := mcp.NewTool("describe_tool",
		mcp.WithDescription("Get the full input schema for a single tool by name. "+
			"Use the tool name returned by search_tools. The inputSchema field contains "+
			"the JSON Schema that the tool's arguments must conform to."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Tool name from search_tools result"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		name, _ := args["name"].(string)
		if name == "" {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent("name is required")},
			}, nil
		}

		detail, err := catalog.Describe(name)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent(err.Error())},
			}, nil
		}

		data, err := json.Marshal(detail)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool detail: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(data))},
		}, nil
	})
}

// registerInvokeTool registers the invoke_tool meta-tool.
func registerInvokeTool(srv *server.MCPServer, catalog *ToolCatalog) {
	tool := mcp.NewTool("invoke_tool",
		mcp.WithDescription("Execute a tool by name with the given arguments. "+
			"Use describe_tool first to discover the required argument schema. "+
			"The arguments object must match the tool's inputSchema."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Tool name from search_tools result"),
		),
		mcp.WithObject("arguments",
			mcp.Description("Arguments object matching the tool's inputSchema. "+
				"Use describe_tool to see the schema."),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		name, _ := args["name"].(string)
		if name == "" {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent("name is required")},
			}, nil
		}

		toolArgs, _ := args["arguments"].(map[string]any)
		if toolArgs == nil {
			toolArgs = map[string]any{}
		}

		result, err := catalog.Invoke(ctx, name, toolArgs)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent(err.Error())},
			}, nil
		}
		return result, nil
	})
}
