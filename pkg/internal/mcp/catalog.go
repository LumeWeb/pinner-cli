package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

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

// Interaction classifies how a tool behaves when invoked by an agent over the
// MCP channel (via invoke_tool). It lets the server steer agents away from
// commands that would read drained stdin or block on a prompt, and instead
// return a structured redirect — so an agent can never hang on a deep command.
//
// The classification is inferred from the CLI command path at registration
// (see classifyInteraction), mirroring how categorize/isReadOnlyName work.
type Interaction string

const (
	// InteractionAgentSafe marks a tool that is non-blocking for agents: it
	// either completes, fast-fails, or returns a needs_human redirect. This is
	// the default.
	InteractionAgentSafe Interaction = "agent_safe"
	// InteractionInteractive marks a tool that is purely human-facing (a
	// wizard/setup flow that prompts interactively). Agents should not invoke
	// it; invoke_tool redirects, and search_tools hides it.
	InteractionInteractive Interaction = "interactive"
	// InteractionStdinInput marks a tool whose action reads piped stdin as its
	// input (e.g. --seed-stdin restore, upload-from-stdin). Over MCP no such
	// data is piped, so invoking it would block; invoke_tool steers agents to
	// an alternative instead.
	InteractionStdinInput Interaction = "stdin_input"
)

// ToolEntry is a single tool in the internal catalog. It stores everything
// the meta-tools need to describe and invoke a tool without exposing it
// via the standard tools/list endpoint.
type ToolEntry struct {
	Name        string
	Title       string
	Description string
	Category    ToolCategory
	ReadOnly    bool
	Destructive bool
	// Interaction tells agents whether this tool is safe to invoke directly,
	// prompts interactively, or reads piped stdin. Only the MCP server sets it
	// (via classifyInteraction); CLI paths built via RegisterFromCommand get a
	// value at registration.
	Interaction Interaction
	InputSchema json.RawMessage
	// Meta carries arbitrary tool metadata (e.g. MCP Apps `_meta.ui`) through
	// curated registration. SDK-neutral; the wire seam encodes it onto the
	// tool. Extended, never replaced, when attaching app metadata.
	Meta map[string]any
	// Behavior carries agent-facing execution behavior (stdin gating, OOB
	// hand-offs). The invoke gate and post-processing layers read this instead
	// of hardcoded tool-name checks.
	Behavior ToolBehavior
	Handler  PinnerToolHandler
}

// ToolSummary is the lightweight representation returned by search_tools.
// It deliberately omits the input schema so that discovery stays cheap.
type ToolSummary struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Category    ToolCategory `json:"category,omitempty"`
	// Interaction tells an agent whether direct invocation is safe
	// (agent_safe), prompts interactively (interactive), or reads piped stdin
	// (stdin_input). Interactive tools are omitted from search_tools entirely;
	// stdin_input tools remain discoverable so agents see the steering signal.
	Interaction Interaction `json:"interaction,omitempty"`
}

// ToolDetail is the full representation returned by describe_tool.
type ToolDetail struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	Category    ToolCategory    `json:"category,omitempty"`
	ReadOnly    bool            `json:"readOnlyHint,omitempty"`
	Destructive bool            `json:"destructiveHint,omitempty"`
	Interaction Interaction     `json:"interaction,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

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

// Len returns the number of tools currently registered in the catalog.
func (c *ToolCatalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.tools)
}

// Entries returns a snapshot of all registered tools.
func (c *ToolCatalog) Entries() []*ToolEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entries := make([]*ToolEntry, 0, len(c.tools))
	for _, entry := range c.tools {
		entries = append(entries, entry)
	}
	return entries
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
		// Human-only (interactive) tools are hidden from agent discovery so an
		// agent cannot even find a trap that would block on a prompt.
		if t.Interaction == InteractionInteractive {
			continue
		}
		if category != "" && string(t.Category) != category {
			continue
		}

		summary := ToolSummary{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			Interaction: t.Interaction,
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
		Title:       entry.Title,
		Description: entry.Description,
		Category:    entry.Category,
		ReadOnly:    entry.ReadOnly,
		Destructive: entry.Destructive,
		Interaction: entry.Interaction,
		InputSchema: entry.InputSchema,
	}, nil
}

// Invoke dispatches to the named tool's handler and returns the Pinner-neutral
// result.
func (c *ToolCatalog) Invoke(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	c.mu.RLock()
	entry, ok := c.tools[name]
	c.mu.RUnlock()

	if !ok {
		return ToolResult{}, fmt.Errorf("unknown tool: %s", name)
	}

	log.Info("meta-tool invoke", zap.String("tool", name))
	return entry.Handler(ctx, ToolRequest{Name: name, Arguments: args})
}

// RegisterFromCommand walks a urfave/cli/v3 command tree and adds every
// non-hidden command with an action as a ToolEntry in the catalog. The
// handler dispatches to the shared toolHandler (in-process command execution).
// The MCP server itself is not modified: only the catalog is populated.
func (c *ToolCatalog) RegisterFromCommand(root *cli.Command, hasRootAction bool, prefix []string, handler PinnerToolHandler) error {
	var walk func(cmd *cli.Command, prefix ...string) error
	walk = func(cmd *cli.Command, prefix ...string) error {
		if cmd.Name == "mcp" || cmd.Name == "help" {
			return nil
		}

		loc := append(prefix, cmd.Name)
		if !cmd.Hidden && cmd.Action != nil && (len(prefix) > 0 || hasRootAction) {
			schema, err := flagsToSchema(cmd.Flags, cmd.ArgsUsage)
			if err != nil {
				return fmt.Errorf("failed to convert flags to schema %s: %w", loc, err)
			}

			var desc string
			if cmd.Description != "" {
				desc = cmd.Description
			} else {
				desc = cmd.Usage
			}

			// Emit MCP tool annotations so agents get steering hints without
			// reading the full description: a human-readable title, a
			// readOnlyHint for state reads, and a destructiveHint for
			// irreversible operations. The same values are stored on the
			// entry so describe_tool can surface them.
			title := humanTitle(loc)
			readOnly := isReadOnlyName(loc)
			destructive := isDestructiveName(loc)

			toolName := strings.Join(loc, ToolDelimiter)
			category := categorize(loc)
			interaction := classifyInteraction(loc)
			c.Add(&ToolEntry{
				Name:        toolName,
				Title:       title,
				Description: desc,
				Category:    category,
				ReadOnly:    readOnly,
				Destructive: destructive,
				Interaction: interaction,
				InputSchema: schema,
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

// interactiveLeafSegments are leaf command names whose action is purely
// human-driven (interactive setup flows) and has no meaningful agent-safe
// result. Agents are steered away from these and they are hidden from
// search_tools discovery.
var interactiveLeafSegments = map[string]bool{
	"setup": true,
}

// stdinInputLeaves are leaf command names whose action reads piped stdin
// unconditionally (not guarded by isStdinPipe) and would block over the MCP
// channel, where no such data is piped. "restore" covers vault restore
// --seed-stdin, which calls io.ReadAll(os.Stdin) directly.
//
// The Interaction enum both drives discovery/steering AND the invoke_tool
// stdin gate (sdk_official.go), which switches on entry.Interaction. So the
// signal must stay stdin_input: the non-stdin OOB restore hand-off is already
// permitted by invoke_tool's bypassGate (scoped to pinner_vault_restore without
// --seed-stdin), and keeping the enum here guarantees a --seed-stdin invocation
// is still gated instead of consuming the MCP transport pipe via os.Stdin.
var stdinInputLeaves = map[string]bool{
	"restore": true, // vault restore --seed-stdin
}

// classifyInteraction determines how a command behaves when an agent invokes
// it over the MCP channel, inferred from its command path. The rules:
//
//   - leaf == "setup"              -> interactive (human-only setup flow)
//   - leaf == "upload"/"restore"   -> stdin_input (reads piped stdin)
//   - everything else              -> agent_safe (the default)
func classifyInteraction(loc []string) Interaction {
	if len(loc) == 0 {
		return InteractionAgentSafe
	}
	leaf := loc[len(loc)-1]
	if interactiveLeafSegments[leaf] {
		return InteractionInteractive
	}
	if stdinInputLeaves[leaf] {
		return InteractionStdinInput
	}
	return InteractionAgentSafe
}

// destructiveSegments are leaf command names that perform destructive or
// irreversible changes (deletes, purges, cancellation of an active contract).
// These get the MCP destructiveHint annotation so agents can present the
// action to the user before invoking it.
var destructiveSegments = map[string]bool{
	"rm": true, "delete": true, "purge": true, "unpin": true,
	"unpin-all": true, "abort-cancel": true, "cancel": true, "forget": true,
	"remove": true, "revoke": true, "clear": true, "reset": true,
}

// readOnlySegments are leaf command names that only read state and do not
// modify the environment. These get the MCP readOnlyHint annotation.
var readOnlySegments = map[string]bool{
	"list": true, "ls": true, "get": true, "status": true, "stat": true,
	"show": true, "describe": true, "search": true, "resolve": true,
	"verify": true, "peek": true, "version": true, "whoami": true,
	"login-check": true, "profiles": true, "list-users": true,
	"list-gateway": true, "overview": true, "price-lines": true,
	"pricing-plans": true,
}

// isDestructiveName reports whether the leaf command name indicates a
// destructive operation.
func isDestructiveName(loc []string) bool {
	if len(loc) == 0 {
		return false
	}
	return destructiveSegments[loc[len(loc)-1]]
}

// isReadOnlyName reports whether the leaf command name indicates a read-only
// operation. Commands that are explicitly destructive are never read-only.
func isReadOnlyName(loc []string) bool {
	if len(loc) == 0 {
		return false
	}
	leaf := loc[len(loc)-1]
	return !destructiveSegments[leaf] && readOnlySegments[leaf]
}

// humanTitle derives a human-friendly tool title from the command path,
// always dropping the root application name segment. For example
// ["pinner", "websites", "domains", "create"] becomes "Websites Domains Create".
// A path with only the root name yields an empty title.
func humanTitle(loc []string) string {
	segments := loc
	if len(segments) >= 1 {
		// Drop the root application name segment.
		segments = segments[1:]
	}
	words := make([]string, 0, len(segments))
	for _, s := range segments {
		if s == "" {
			continue
		}
		words = append(words, titleWord(s))
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ")
}

// titleWord capitalizes the first letter of a command word, including each
// hyphen-separated component so "list-users" becomes "List-Users".
func titleWord(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "-")
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
