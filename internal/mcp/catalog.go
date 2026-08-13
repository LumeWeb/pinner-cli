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
// return a structured redirect so an agent never hangs on a deep command.
//
// The classification is inferred from the CLI command path at registration
// (see classifyInteraction), in the same way categorize/isReadOnlyName work.
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
	// DirectVisible reports whether the tool is part of the directly-exposed
	// surface (tools/list) in addition to progressive discovery. The curated
	// registration loop registers every DirectVisible entry; the search/describe
	// meta-tools index the whole catalog regardless.
	DirectVisible bool
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
	// SensitiveFlags lists the long flag names whose values are credential
	// material and must be redacted from the in-process arg-trace log. It is
	// derived from the command's flag declarations (SensitiveProvider) at
	// registration time, so the redaction vocabulary cannot drift from the
	// CLI.
	SensitiveFlags []string
	Handler        PinnerToolHandler
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
	// CatalogDeps, when set, holds the operation-catalog dependency factory
	// (config manager + core service factories) handed to buildCatalog via the
	// withCatalogDeps option. It is plumbing only at this stage: nothing in the
	// catalog consumes it yet. A later unit reads it to populate the surface
	// from the operation catalog instead of (or alongside) the CLI command
	// tree.
	CatalogDeps func() *CatalogDepsBundle
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
	// Global flags declared on the root command (e.g. --auth-token) apply to
	// every subcommand. Union them with each command's own sensitive flags so
	// an agent passing a root-level credential flag to any tool still has its
	// value redacted from the arg trace.
	rootSensitive := sensitiveFlagNames(root.Flags)
	var walk func(cmd *cli.Command, inherited []cli.Flag, inheritedSensitive []string, prefix ...string) error
	walk = func(cmd *cli.Command, inherited []cli.Flag, inheritedSensitive []string, prefix ...string) error {
		if cmd.Name == "mcp" || cmd.Name == "help" {
			return nil
		}

		loc := append(prefix, cmd.Name)
		if !cmd.Hidden && cmd.Action != nil && (len(prefix) > 0 || hasRootAction) {
			// A subcommand inherits its parent command's flags (urfave/cli
			// semantics), so flags declared only on a parent (e.g. the vault
			// command's --profile, which every vault subcommand needs) appear
			// in the tool's input schema instead of being hidden inside the
			// _args array. Child flags take precedence over inherited ones.
			schemaFlags := mergeInheritedFlags(inherited, cmd.Flags)
			schema, err := flagsToSchema(schemaFlags, cmd.ArgsUsage)
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
				Name:           toolName,
				Title:          title,
				Description:    desc,
				Category:       category,
				ReadOnly:       readOnly,
				Destructive:    destructive,
				Interaction:    interaction,
				InputSchema:    schema,
				SensitiveFlags: unionSensitiveFlags(unionSensitiveFlags(inheritedSensitive, sensitiveFlagNames(cmd.Flags)), rootSensitive),
				Handler:        handler,
			})

			log.Debug("cataloged command", zap.Strings("loc", loc), zap.String("category", string(category)))
		}

		for _, sub := range cmd.Commands {
			// Accumulate inherited flags AND inherited sensitive flag names down
			// the tree: a sensitive flag declared on an intermediate parent
			// (e.g. vault --password used by a nested action) must be redacted
			// from arg-trace logs just like the tool's own, otherwise the
			// schema advertises it while the adapter's redaction (driven only
			// by entry.SensitiveFlags) leaves its value in plaintext.
			childInherited := mergeInheritedFlags(inherited, cmd.Flags)
			childSensitive := unionSensitiveFlags(inheritedSensitive, sensitiveFlagNames(cmd.Flags))
			if err := walk(sub, childInherited, childSensitive, loc...); err != nil {
				return err
			}
		}
		return nil
	}

	return walk(root, nil, nil, prefix...)
}

// mergeInheritedFlags unions a command's inherited parent flags with its own
// flags, so subcommands expose flags declared only on their parent in their
// input schema. Copies are made (never mutating the caller's slices). A flag
// is identified by its Name; the child's declaration wins over the inherited
// one when both name the same flag.
func mergeInheritedFlags(inherited, own []cli.Flag) []cli.Flag {
	if len(inherited) == 0 {
		return own
	}
	merged := make([]cli.Flag, 0, len(inherited)+len(own))
	seen := make(map[string]bool, len(inherited)+len(own))
	for _, f := range inherited {
		n := flagName(f)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		merged = append(merged, f)
	}
	for _, f := range own {
		n := flagName(f)
		if n == "" {
			continue
		}
		if seen[n] {
			// Child overrides inherited: drop the inherited copy.
			for i := range merged {
				if flagName(merged[i]) == n {
					merged = append(merged[:i], merged[i+1:]...)
					break
				}
			}
		}
		seen[n] = true
		merged = append(merged, f)
	}
	return merged
}

// flagName returns the primary name of a cli.Flag, or "" if it cannot be
// determined.
func flagName(f cli.Flag) string {
	switch v := f.(type) {
	case *cli.StringFlag:
		return v.Name
	case *sensitiveStringFlag:
		if v.StringFlag != nil {
			return v.StringFlag.Name
		}
	case *enumStringFlag:
		return v.Name
	case *cli.BoolFlag:
		return v.Name
	case *cli.StringSliceFlag:
		return v.Name
	case *cli.DurationFlag:
		return v.Name
	case interface{ GetName() string }:
		return v.GetName()
	}
	return ""
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
// human-facing (a wizard/setup flow that prompts interactively) and have no
// agent-safe form. Agents are steered away from these and they are hidden from
// search_tools discovery.
var interactiveLeafSegments = map[string]bool{
	"setup": true,
}

// classifyInteraction determines how a command behaves when an agent invokes
// it over the MCP channel, inferred from its command path. The rules:
//
//   - leaf == "setup"   -> interactive (human-only setup flow)
//   - everything else   -> agent_safe (the default)
//
// Stdin-reading is a CLI-side concern only: a command whose action reads piped
// stdin (e.g. `vault restore --seed-stdin`) is a human/terminal mechanism and
// is never exposed through the MCP tools (the agent-safe OOB hand-off is used
// instead). The MCP layer does not reason about, gate, or expose stdin.
func classifyInteraction(loc []string) Interaction {
	if len(loc) == 0 {
		return InteractionAgentSafe
	}
	leaf := loc[len(loc)-1]
	if interactiveLeafSegments[leaf] {
		return InteractionInteractive
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
