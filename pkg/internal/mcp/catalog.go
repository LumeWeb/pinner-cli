package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// ToolCategory classifies a tool for filtering during discovery.
type ToolCategory string

const (
	CategoryCore   ToolCategory = "core"
	CategoryAdmin  ToolCategory = "admin"
	CategoryWizard ToolCategory = "wizard"
)

// ToolEntry is a single tool in the internal catalog. It stores everything
// the meta-tools need to describe and invoke a tool without exposing it
// via the standard tools/list endpoint.
type ToolEntry struct {
	Name        string
	Description string
	Category    ToolCategory
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// ToolSummary is the lightweight representation returned by search_tools.
// It deliberately omits the input schema so that discovery stays cheap.
type ToolSummary struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Category    ToolCategory `json:"category,omitempty"`
}

// ToolDetail is the full representation returned by describe_tool.
type ToolDetail struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    ToolCategory    `json:"category,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolHandler is the function signature for executing a catalog tool.
type ToolHandler = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)

// ToolCatalog is an in-memory registry of tools that are discovered through
// the meta-tools (search_tools, describe_tool, invoke_tool) instead of being
// listed directly in tools/list. This implements server-side progressive
// disclosure: the MCP client sees only 3 meta-tools, while the real tool
// catalog stays internal.
type ToolCatalog struct {
	mu    sync.RWMutex
	tools map[string]*ToolEntry
}

// NewToolCatalog returns an empty catalog.
func NewToolCatalog() *ToolCatalog {
	return &ToolCatalog{tools: make(map[string]*ToolEntry)}
}

// Add registers a tool entry. If a tool with the same name already exists it
// is replaced.
func (c *ToolCatalog) Add(entry *ToolEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[entry.Name] = entry
}

// Get returns the entry for name, or false if not found.
func (c *ToolCatalog) Get(name string) (*ToolEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.tools[name]
	return entry, ok
}

// Search finds tools matching the query. The matching strategy is layered:
//
//  1. Empty query returns all tools (sorted by category then name).
//  2. Non-empty query ranks each tool:
//     0 = exact name match
//     1 = name starts with query
//     2 = name contains query
//     3 = name is a subsequence match of query
//     4 = description contains query
//     Tools that do not match at any level are excluded.
//
// If category is non-empty, only tools in that category are considered.
func (c *ToolCatalog) Search(query, category string) []ToolSummary {
	query = strings.ToLower(strings.TrimSpace(query))

	c.mu.RLock()
	defer c.mu.RUnlock()

	type ranked struct {
		summary ToolSummary
		rank    int
	}

	var results []ranked
	for _, t := range c.tools {
		if category != "" && string(t.Category) != category {
			continue
		}

		summary := ToolSummary{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
		}

		if query == "" {
			results = append(results, ranked{summary: summary, rank: 0})
			continue
		}

		nameLower := strings.ToLower(t.Name)
		descLower := strings.ToLower(t.Description)
		rank := matchRank(query, nameLower, descLower)
		if rank >= 0 {
			results = append(results, ranked{summary: summary, rank: rank})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].rank != results[j].rank {
			return results[i].rank < results[j].rank
		}
		if results[i].summary.Category != results[j].summary.Category {
			return results[i].summary.Category < results[j].summary.Category
		}
		return results[i].summary.Name < results[j].summary.Name
	})

	summaries := make([]ToolSummary, len(results))
	for i, r := range results {
		summaries[i] = r.summary
	}
	return summaries
}

// Describe returns the full detail (including input schema) for a single tool.
func (c *ToolCatalog) Describe(name string) (*ToolDetail, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return &ToolDetail{
		Name:        entry.Name,
		Description: entry.Description,
		Category:    entry.Category,
		InputSchema: entry.InputSchema,
	}, nil
}

// Invoke dispatches to the named tool's handler.
func (c *ToolCatalog) Invoke(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	c.mu.RLock()
	entry, ok := c.tools[name]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	log.Info("meta-tool invoke", zap.String("tool", name))
	return entry.Handler(ctx, req)
}

// RegisterFromCommand walks a urfave/cli/v3 command tree and adds every
// non-hidden command with an action as a ToolEntry in the catalog. The
// handler dispatches to the shared toolHandler (in-process command execution).
// The MCP server itself is not modified — only the catalog is populated.
func (c *ToolCatalog) RegisterFromCommand(root *cli.Command, hasRootAction bool, prefix []string, handler ToolHandler) error {
	var walk func(cmd *cli.Command, prefix ...string) error
	walk = func(cmd *cli.Command, prefix ...string) error {
		if cmd.Name == "mcp" || cmd.Name == "help" {
			return nil
		}

		loc := append(prefix, cmd.Name)
		if !cmd.Hidden && cmd.Action != nil && (len(prefix) > 0 || hasRootAction) {
			toolOpts, err := FlagsToTools(cmd.Flags)
			if err != nil {
				return fmt.Errorf("failed to convert flags to tools %s: %w", loc, err)
			}

			var desc string
			if cmd.Description != "" {
				desc = cmd.Description
			} else {
				desc = cmd.Usage
			}
			toolOpts = append(toolOpts, mcp.WithDescription(desc))

			// Build the tool to extract its generated input schema.
			toolName := strings.Join(loc, ToolDelimiter)
			tool := mcp.NewTool(toolName, toolOpts...)

			// If the command has positional args, add an _args property
			// to the schema so MCP clients know they can pass positionals
			// via the _args array field.
			if cmd.ArgsUsage != "" {
				if tool.InputSchema.Properties == nil {
					tool.InputSchema.Properties = make(map[string]any)
				}
				tool.InputSchema.Properties["_args"] = map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Positional arguments: " + cmd.ArgsUsage,
				}
			}

			schemaBytes, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return fmt.Errorf("failed to marshal input schema for %s: %w", toolName, err)
			}

			category := categorize(loc)
			c.Add(&ToolEntry{
				Name:        toolName,
				Description: desc,
				Category:    category,
				InputSchema: schemaBytes,
				Handler:     handler,
			})

			log.Debug("cataloged command", zap.Strings("loc", loc), zap.String("category", string(category)))
		}

		for _, sub := range cmd.Commands {
			if err := walk(sub, loc...); err != nil {
				return err
			}
		}
		return nil
	}

	return walk(root, prefix...)
}

// categorize determines the tool category from its command path.
// Commands under an "admin" segment are admin tools. Wizard session tools
// (matching *_wizard_*) are wizard category. Everything else is core.
func categorize(loc []string) ToolCategory {
	for _, segment := range loc {
		if segment == "admin" {
			return CategoryAdmin
		}
	}

	toolName := strings.Join(loc, ToolDelimiter)
	if strings.Contains(toolName, "wizard") {
		return CategoryWizard
	}

	return CategoryCore
}

// matchRank returns -1 if the query does not match the tool at any level.
// Otherwise it returns a lower-is-better rank:
//
//	0 = exact name match
//	1 = name starts with query
//	2 = name contains query
//	3 = name is a subsequence match of query
//	4 = description contains query
func matchRank(query, name, desc string) int {
	if name == query {
		return 0
	}
	if strings.HasPrefix(name, query) {
		return 1
	}
	if strings.Contains(name, query) {
		return 2
	}
	if isSubsequence(query, name) {
		return 3
	}
	if strings.Contains(desc, query) {
		return 4
	}
	return -1
}

// isSubsequence checks whether every character in src appears in target in
// the same order, but not necessarily contiguously. For example,
// isSubsequence("pload", "pinner_upload") returns true.
func isSubsequence(src, target string) bool {
	if len(src) == 0 {
		return true
	}
	if len(src) > len(target) {
		return false
	}

	i := 0
	for _, c := range target {
		if byte(src[i]) == byte(c) {
			i++
			if i == len(src) {
				return true
			}
		}
	}
	return false
}
