package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/samber/lo"
	"go.uber.org/zap"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// ToolSummary is the lightweight representation returned by search_tools.
// It deliberately omits the input schema so that discovery stays cheap.
type ToolSummary struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Category    model.ToolCategory `json:"category,omitempty"`
	// ReadOnly/Destructive surface the operation's Safety classification on the
	// cheap discovery pass (search_tools / onboarding) so a framework author can
	// gate autonomy on the safety tier without a per-tool describe_tool round-trip.
	ReadOnly    bool `json:"readOnlyHint,omitempty"`
	Destructive bool `json:"destructiveHint,omitempty"`
	// Interaction tells an agent whether direct invocation is safe
	// (agent_safe), prompts interactively (interactive), or reads piped stdin
	// (stdin_input). Interactive tools are omitted from search_tools entirely;
	// stdin_input tools remain discoverable so agents see the steering signal.
	Interaction model.Interaction `json:"interaction,omitempty"`
}

// ToolDetail is the full representation returned by describe_tool.
type ToolDetail struct {
	Name        string             `json:"name"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description"`
	Category    model.ToolCategory `json:"category,omitempty"`
	ReadOnly    bool               `json:"readOnlyHint,omitempty"`
	Destructive bool               `json:"destructiveHint,omitempty"`
	Interaction model.Interaction  `json:"interaction,omitempty"`
	InputSchema json.RawMessage    `json:"inputSchema"`
}

// ToolCatalog is an in-memory registry of tools that are discovered through
// the meta-tools (search_tools, describe_tool, invoke_tool) instead of being
// listed directly in tools/list. This implements server-side progressive
// disclosure: the MCP client sees only 3 meta-tools, while the real tool
// catalog stays internal.
type ToolCatalog struct {
	mu    sync.RWMutex
	tools map[string]*model.ToolEntry
	// CatalogDeps, when set, holds the operation-catalog dependency factory
	// (config manager + core service factories) handed to buildCatalog via the
	// withCatalogDeps option. It is plumbing only at this stage: nothing in the
	// catalog consumes it yet. A later unit reads it to populate the surface
	// from the operation catalog instead of (or alongside) the CLI command
	// tree.
	CatalogDeps func() *CatalogDepsBundle
	// Surface records the server construction surface (which operation domains
	// and tool families are exposed). It is set by buildCatalog and read by
	// registerCustomTools and markCurated so the whole surface agrees on what
	// was registered. The zero value is the full surface.
	Surface Surface
	// CompilerMode records whether buildCatalog actually entered compiler mode
	// (opsCat resolved non-nil: the factory was supplied AND returned a
	// bundle). It is the single source of truth both buildCatalog and
	// registerCustomTools read to pick the curated tool set, so the two never
	// disagree on which naming surface applies.
	CompilerMode bool
}

// NewToolCatalog returns an empty catalog.
func NewToolCatalog() *ToolCatalog {
	return &ToolCatalog{tools: make(map[string]*model.ToolEntry)}
}

// Add registers a tool entry. If a tool with the same name already exists it
// is replaced.
//
// Every catalog entry is guaranteed to carry MCPTargets: when a descriptor
// declares none, its static Description is wrapped in a universal Fallback
// target so describe_tool/search_tools resolve it through the same
// profile-aware seam as every other tool. A Fallback always matches, so
// resolution is unchanged — the wrap only establishes the invariant that no
// catalog tool lacks a target list (enforced by TestCatalogEntriesCarryMCPTargets).
func (c *ToolCatalog) Add(entry *model.ToolEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(entry.MCPTargets) == 0 {
		entry.MCPTargets = toolforge.MCPTargets(toolforge.Fallback(entry.Description))
	}
	c.tools[entry.Name] = entry
}

// Get returns the entry for name, or false if not found.
func (c *ToolCatalog) Get(name string) (*model.ToolEntry, bool) {
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
func (c *ToolCatalog) Entries() []*model.ToolEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entries := make([]*model.ToolEntry, 0, len(c.tools))
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
		if t.Interaction == model.InteractionInteractive {
			continue
		}
		if t.Category == model.CategoryWizard {
			continue
		}
		if !isPrimaryTool(t.Name) {
			continue
		}
		tools = append(tools, ToolSummary{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			ReadOnly:    t.ReadOnly,
			Destructive: t.Destructive,
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
// explicitly "wizard". Admin tools (CategoryAdmin) are likewise excluded from
// general keyword search — a vague query must not retrieve an admin_* op —
// and only returned when category is explicitly "admin". limit caps the number
// of results returned (<=0 means no cap).
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
		if t.Interaction == model.InteractionInteractive {
			continue
		}
		// Wizards are interactive by nature; keep them out of general keyword
		// search unless the category filter names them explicitly.
		if t.Category == model.CategoryWizard && category != string(model.CategoryWizard) {
			continue
		}
		// Admin tools are gated: never surfaced by a general keyword search (a
		// vague query like "cancel" must not retrieve admin_billing_*). They
		// are only returned when the caller explicitly browses category=admin.
		if t.Category == model.CategoryAdmin && category != string(model.CategoryAdmin) {
			continue
		}
		if category != "" && string(t.Category) != category {
			continue
		}

		summary := ToolSummary{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			ReadOnly:    t.ReadOnly,
			Destructive: t.Destructive,
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

	summaries := lo.Map(results, func(r ranked, _ int) ToolSummary { return r.summary })
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
	case "agent_guide",
		"auth_status", "auth_sso", "auth_resume",
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
		if t.Category == model.CategoryWizard || t.Category == model.CategoryAdmin || t.Interaction == model.InteractionInteractive {
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
	if entry.Category == model.CategoryAdmin {
		return nil, fmt.Errorf("admin tool %s is not available through describe_tool; use search_tools with category=admin to discover admin tools", name)
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

// DescribeFor returns the full detail for a single tool, resolving
// description variants against the per-request profile when the tool
// carries MCPTargets. A nil profile falls back to the static Description.
func (c *ToolCatalog) DescribeFor(name string, profile *hostenv.PlatformProfile) (*ToolDetail, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	if entry.Category == model.CategoryAdmin {
		return nil, fmt.Errorf("admin tool %s is not available through describe_tool; use search_tools with category=admin to discover admin tools", name)
	}

	return &ToolDetail{
		Name:        entry.Name,
		Title:       entry.Title,
		Description: resolveDescription(entry, profile),
		Category:    entry.Category,
		ReadOnly:    entry.ReadOnly,
		Destructive: entry.Destructive,
		Interaction: entry.Interaction,
		InputSchema: entry.InputSchema,
	}, nil
}

// resolveDescription returns the profile-resolved description for an entry,
// falling back to the static Description when MCPTargets is empty or profile
// is nil.
func resolveDescription(entry *model.ToolEntry, profile *hostenv.PlatformProfile) string {
	if len(entry.MCPTargets) > 0 && profile != nil {
		if resolved, ok := toolforge.ResolveDescription(entry.MCPTargets, *profile); ok {
			return resolved
		}
	}
	return entry.Description
}

// SearchFor is the profile-aware variant of Search. It returns matching tools
// with descriptions resolved against the per-request profile when MCPTargets
// are present. Callers without a profile should use Search instead.
func (c *ToolCatalog) SearchFor(query, category string, limit int, profile *hostenv.PlatformProfile) []ToolSummary {
	query = strings.ToLower(strings.TrimSpace(query))

	c.mu.RLock()
	defer c.mu.RUnlock()

	type ranked struct {
		summary ToolSummary
		rank    int
	}

	var results []ranked
	for _, t := range c.tools {
		if t.Interaction == model.InteractionInteractive {
			continue
		}
		if t.Category == model.CategoryWizard && category != string(model.CategoryWizard) {
			continue
		}
		if t.Category == model.CategoryAdmin && category != string(model.CategoryAdmin) {
			continue
		}
		if category != "" && string(t.Category) != category {
			continue
		}

		summary := ToolSummary{
			Name:        t.Name,
			Description: resolveDescription(t, profile),
			Category:    t.Category,
			ReadOnly:    t.ReadOnly,
			Destructive: t.Destructive,
			Interaction: t.Interaction,
		}

		nameLower := strings.ToLower(t.Name)
		descLower := strings.ToLower(summary.Description)
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

	summaries := lo.Map(results, func(r ranked, _ int) ToolSummary { return r.summary })
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries
}

// Invoke dispatches to the named tool's handler and returns the Pinner-neutral
// result.
func (c *ToolCatalog) Invoke(ctx context.Context, name string, args map[string]any) (model.ToolResult, error) {
	c.mu.RLock()
	entry, ok := c.tools[name]
	c.mu.RUnlock()

	if !ok {
		return model.ToolResult{}, fmt.Errorf("unknown tool: %s", name)
	}
	if entry.Category == model.CategoryAdmin {
		return model.ToolResult{IsError: true, Text: fmt.Sprintf("admin tool %s is not available through invoke_tool; use search_tools with category=admin to discover admin tools", name)}, nil
	}

	log.Info("meta-tool invoke", zap.String("tool", name))
	return entry.Handler(ctx, model.ToolRequest{Name: name, Arguments: args})
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
//	3 = name is a subsequence match of query within a single name segment
//	4 = description contains query as a whole token
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
	if matchSegmentSubsequence(query, name) {
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

// matchSegmentSubsequence reports whether src is a subsequence of any single
// underscore- or hyphen-delimited segment of name. Fuzzy subsequence matching
// is deliberately scoped to one segment so a query does not match by scattering
// its letters across unrelated segments: e.g. "auth" contains a,u,t in "vault"
// and h in "share", but matches no single segment of "vault_share", so it no
// longer shows up as noise after the real auth_* tools. Genuine within-segment
// abbreviations still match: "pload" matches "upload".
func matchSegmentSubsequence(src, name string) bool {
	src = strings.ToLower(strings.TrimSpace(src))
	if src == "" {
		return false
	}
	for _, seg := range segmentize(name) {
		if isSubsequence(src, seg) {
			return true
		}
	}
	return false
}

// segmentize splits a tool name into its underscore- and hyphen-delimited
// words (e.g. "vault_cache_rebuild" -> ["vault", "cache", "rebuild"]).
func segmentize(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})
}

// isSubsequence checks whether every character in src appears in target in
// the same order, but not necessarily contiguously. For example,
// isSubsequence("pload", "upload") returns true.
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
