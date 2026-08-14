package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// ToolCategory classifies a tool for filtering during discovery.
type ToolCategory string

const (
	CategoryCore       ToolCategory = "core"
	CategoryAccount    ToolCategory = "account"
	CategoryVault      ToolCategory = "vault"
	CategoryIPNS       ToolCategory = "ipns"
	CategoryOperations ToolCategory = "operations"
	CategoryAdmin      ToolCategory = "admin"
	CategoryWizard     ToolCategory = "wizard"
)

// Interaction classifies how a tool behaves when invoked by an agent over the
// MCP channel (via invoke_tool). It lets the server steer agents away from
// commands that would read drained stdin or block on a prompt, and instead
// return a structured redirect so an agent never hangs on a deep command.
//
// The classification comes from the compiler-backed operation surface and is
// stamped on each ToolEntry at registration time.
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
	// prompts interactively, or reads piped stdin. It is set at registration
	// time (e.g. the compiled surface's Operation metadata or the OOB setup
	// handlers).
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
	// CompilerMode records whether buildCatalog actually entered compiler mode
	// (opsCat resolved non-nil: the factory was supplied AND returned a
	// bundle). It is the single source of truth both buildCatalog and
	// registerCustomTools read to pick the curated tool set, so the two never
	// disagree on which naming surface applies.
	CompilerMode bool
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

// isOnboardingQuery reports whether a query selects the onboarding listing.
// Both an empty query and the literal "help" keyword do. It is the single
// routing predicate the search_tools handler uses to pick between the search
// surface (keyword matching) and the onboarding surface (curated start-here
// listing).
func isOnboardingQuery(query string) bool {
	return query == "" || query == "help"
}

// SearchResult is the wire envelope for the keyword-search path of the
// search_tools meta-tool: the matching tools plus their count.
type SearchResult struct {
	Tools []ToolSummary `json:"tools"`
	Total int           `json:"total"`
}

// OnboardingResult is the wire envelope for the onboarding path (empty/help
// query, no category): the curated primary start-here tools matching the
// agent_guide flows, plus a hint pointing the agent onward.
type OnboardingResult struct {
	Tools []ToolSummary `json:"tools"`
	Total int           `json:"total"`
	// Hint is presented guidance for the onboarding listing: where to get the
	// full flows (agent_guide) and how to browse a specific domain (category).
	Hint string `json:"hint,omitempty"`
}

// Onboarding returns the curated "start here" listing for an empty/help
// search: exactly the tool steps in the four agent_guide primary flows (auth,
// vault_create, vault_restore, pins), so a fresh agent sees a bounded set to
// begin with instead of the full catalog dump. It is the onboarding surface,
// distinct from Search; the handler routes to it via isOnboardingQuery when
// no category filter is given.
func (c *ToolCatalog) Onboarding() OnboardingResult {
	var tools []ToolSummary
	c.mu.RLock()
	for _, t := range c.tools {
		if t.Interaction == InteractionInteractive {
			continue
		}
		if t.Category == CategoryWizard {
			continue
		}
		if !isPrimaryTool(t.Name) {
			continue
		}
		tools = append(tools, ToolSummary{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			Interaction: t.Interaction,
		})
	}
	c.mu.RUnlock()

	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Category != tools[j].Category {
			return tools[i].Category < tools[j].Category
		}
		return tools[i].Name < tools[j].Name
	})

	return OnboardingResult{Tools: tools, Total: len(tools)}
}

// Search finds tools matching a non-empty keyword query. This is the pure
// keyword-search surface; onboarding (empty/help) is handled by Onboarding.
//
// The matching strategy is layered:
//
//  1. Each tool is ranked:
//     0 = exact name match
//     1 = name starts with query
//     2 = name contains query
//     3 = name is a subsequence match of query
//     4 = description contains query as a whole token
//     Tools that do not match at any level are excluded. Description matches
//     are token-based (the query must appear as its own word, so "auth" does
//     not match "authenticated") and capped so name hits always dominate.
//
// An empty query with an explicit category browses that whole category
// (every tools in it matches, ordered by category then name); the handler
// routes pure onboarding (empty/help, no category) to Onboarding instead.
//
// Wizard tools (CategoryWizard) are excluded by default so an agent cannot
// stumble onto an interactive flow; they are only returned when category is
// explicitly "wizard". limit caps the number of results returned (<=0 means
// no cap).
//
// If category is non-empty, only tools in that category are considered.
func (c *ToolCatalog) Search(query, category string, limit int) []ToolSummary {
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
		// Wizards are interactive by nature; keep them out of general keyword
		// search unless the category filter names them explicitly.
		if t.Category == CategoryWizard && category != string(CategoryWizard) {
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
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries
}

// isPrimaryTool reports whether a tool belongs to the curated "start here"
// set surfaced on an empty/help search. It mirrors exactly the tool steps in
// the four agent_guide primary flows (auth, vault_create, vault_restore,
// pins), so a fresh agent sees the tools it needs to begin.
func isPrimaryTool(name string) bool {
	switch name {
	case "auth_status", "auth_sso", "auth_resume",
		"vault_create", "vault_create_resume", "vault_status",
		"vault_restore", "vault_restore_resume",
		"pins_add", "pins_list", "pins_status", "pins_rm":
		return true
	}
	return false
}

// Suggest returns up to max tool names close to the given (unknown) name,
// ordered by ascending Levenshtein distance then name. It lets describe_tool
// and invoke_tool answer with "did you mean ...?" instead of a bare
// unknown-tool error. Tools that Search deliberately hides — wizards and
// interactive/human-only tools — are excluded so suggestions never surface a
// tool the agent could not discover. Distance uses a local zero-dependency
// Levenshtein (the same subsequence/rank family we forked rather than
// importing a fuzzy-search library).
func (c *ToolCatalog) Suggest(name string, max int) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	target := strings.ToLower(name)
	type scored struct {
		dist int
		name string
	}
	all := make([]scored, 0, len(c.tools))
	for _, t := range c.tools {
		if t.Category == CategoryWizard || t.Interaction == InteractionInteractive {
			continue
		}
		d := levenshtein(strings.ToLower(t.Name), target)
		all = append(all, scored{dist: d, name: t.Name})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].dist != all[j].dist {
			return all[i].dist < all[j].dist
		}
		return all[i].name < all[j].name
	})
	var out []string
	for _, s := range all {
		if max > 0 && len(out) >= max {
			break
		}
		out = append(out, s.name)
	}
	return out
}

// levenshtein returns the edit distance between two strings (case-sensitive;
// callers pass lowercased inputs). It is a compact, dependency-free
// implementation; the repo avoids pulling a fuzzy-search library (and its
// golang.org/x/text dependency) for a static ~67-tool catalog.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
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
//		0 = exact name match
//		1 = name starts with query
//		2 = name contains query
//	    3 = name is a subsequence match of query
//	    4 = description contains query as a whole token
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
	// Description matches are whole-token only: "auth" must not match
	// "authenticated" or "authorized". This keeps description hits from
	// drowning a keyword search with semantically unrelated tools.
	if descContainsToken(desc, query) {
		return 4
	}
	return -1
}

// descContainsToken reports whether query appears in desc as a complete
// word/phrase, i.e. bounded on both sides by a non-alphanumeric boundary (or
// start/end of string). It is case-insensitive on both sides. Unlike a raw
// substring search, "auth" does not match within the longer word
// "authenticated", because the char before/after "auth" would be alphanumeric
// ("e"/"e"), not a boundary. Hyphenated phrases like "sign-in" match their
// own hyphenated occurrence because the query's internal punctuation is
// preserved (only the outer bounds must be word boundaries).
func descContainsToken(desc, query string) bool {
	if query == "" {
		return false
	}
	lowerDesc := strings.ToLower(desc)
	lowerQuery := strings.ToLower(query)
	start := 0
	for {
		idx := strings.Index(lowerDesc[start:], lowerQuery)
		if idx < 0 {
			return false
		}
		abs := start + idx
		beforeOK := abs == 0 || !isAlphaNum(rune(lowerDesc[abs-1]))
		after := abs + len(lowerQuery)
		afterOK := after >= len(lowerDesc) || !isAlphaNum(rune(lowerDesc[after]))
		if beforeOK && afterOK {
			return true
		}
		start = abs + 1
	}
}

// isAlphaNum reports whether r is an ASCII letter or digit (token body char).
func isAlphaNum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
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
