package toolforge

import (
	"strings"
	"text/template"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// ToolDefinition is a declarative tool specification whose concrete
// presentation (description, schema, metadata) varies by platform. It
// carries a set of ToolTargets — each a complete, self-contained
// presentation keyed by feature requirements. The forge selects the
// best-matching target for a platform's features at materialization
// time.
//
// Tools that don't vary by host have exactly one ToolTarget with an
// empty Require set. Tools that present differently to different hosts
// (e.g. upload_file, download_file) declare multiple targets with
// distinct Require sets.
type ToolDefinition struct {
	Name     string
	Title    string
	Category model.ToolCategory

	ReadOnly    bool
	Destructive bool
	// DirectVisible controls whether the tool appears in tools/list
	// (in addition to progressive disclosure).
	DirectVisible bool
	// Handler is the single handler shared across all targets. The
	// handler must be capable of processing any argument shape the
	// targets' schemas accept.
	Handler model.PinnerToolHandler

	// Targets are complete, self-contained presentations of this tool
	// for specific capability contexts. The forge resolves the
	// best-matching target: among all targets whose Require set is
	// fully satisfied by the platform's features, the one with the
	// most required features wins. Ties are broken by declaration
	// order (first wins).
	Targets []model.ToolTarget
}

// ToolForge materializes ToolDefinitions into concrete
// model.ToolDescriptors for a specific hostenv.PlatformProfile. It is a pure
// function of (definitions, profile) — no side effects, no mutation of
// inputs.
type ToolForge struct {
	defs []ToolDefinition
}

// NewToolForge creates a ToolForge from a set of tool definitions.
func NewToolForge(defs ...ToolDefinition) *ToolForge {
	return &ToolForge{defs: defs}
}

// Add appends a tool definition to the forge.
func (f *ToolForge) Add(def ToolDefinition) {
	f.defs = append(f.defs, def)
}

// Len returns the number of tool definitions.
func (f *ToolForge) Len() int {
	return len(f.defs)
}

// Materialize resolves ToolTargets for a platform profile and produces
// concrete model.ToolDescriptors. For each ToolDefinition:
//  1. The forge finds the best-matching ToolTarget (most specific
//     feature match).
//  2. If no target matches or the target has Visible=false, the tool
//     is excluded.
//  3. Otherwise, a ToolDescriptor is built from the shared identity
//     fields + the target's presentation fields.
func (f *ToolForge) Materialize(profile hostenv.PlatformProfile) []model.ToolDescriptor {
	result := make([]model.ToolDescriptor, 0, len(f.defs))
	for _, def := range f.defs {
		target := resolveTarget(def.Targets, profile)
		if target == nil || !target.Visible {
			continue
		}
		result = append(result, model.ToolDescriptor{
			Name:            def.Name,
			Title:           def.Title,
			Description:     target.Description,
			Category:        def.Category,
			ReadOnly:        def.ReadOnly,
			Destructive:     def.Destructive,
			DirectVisible:   def.DirectVisible,
			InputSchema:     target.InputSchema,
			OutputSchema:    target.OutputSchema,
			Meta:            copyMeta(target.Meta),
			SecuritySchemes: target.SecuritySchemes,
			SensitiveFlags:  target.SensitiveFlags,
			Handler:         def.Handler,
		})
	}
	return result
}

// InstructionsFor builds the MCP server instructions string for a
// specific platform profile. The instructions are platform-aware: they
// only mention file input modes, sinks, and UI features the platform
// actually supports.
func (f *ToolForge) InstructionsFor(profile hostenv.PlatformProfile, toolCount int) string {
	return buildInstructions(profile, toolCount)
}

// resolveTarget resolves the best-matching MCP target for a platform profile.
// This operates purely on the MCP-profile axis (host × transport → features):
// among all targets whose Require set is a subset of the profile's features,
// the one with the most required features wins. Ties are broken by declaration
// order (first wins). Returns nil if no target matches. The interface axis
// (CLI vs MCP) is handled separately at compile time and never reaches this
// function.
func resolveTarget(targets []model.ToolTarget, profile hostenv.PlatformProfile) *model.ToolTarget {
	var best *model.ToolTarget
	bestScore := -1
	for i := range targets {
		t := &targets[i]
		if !profile.Features.HasAll(t.Require) {
			continue
		}
		score := len(t.Require)
		if score > bestScore {
			bestScore = score
			best = t
		}
	}
	return best
}

// copyMeta returns a shallow copy of m, or nil if m is empty.
func copyMeta(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// instructionsTemplate is the text/template for platform-aware server
// instructions. Conditional sections render only when the profile supports
// the corresponding feature.
var instructionsTemplate = template.Must(template.New("instructions").Parse(`This server exposes a curated set of common Pinner tools directly, including upload, pin, list, status, download, vault, website, website/domain wizard tools, and the agent-facing out-of-band sign-in tools (auth_sso and auth_resume). Setup wizard tools are not exposed because they accept credentials.

The tool surface is intentionally two-tier. The tools listed directly in tools/list are the curated, most-used surface. The rest of the catalog (see count below) is served through progressive disclosure and is NOT broken or missing: any tool not listed directly is reachable via search_tools -> describe_tool -> invoke_tool. If a tool you expect is absent from tools/list, search for it rather than assuming it is unavailable. A large catalog is deliberately kept off the direct list to keep the initial tool surface small and the context budget predictable.

For authentication, prefer the out-of-band flow: call auth_sso, give the returned approval URL to the human, then poll auth_resume with the returned handle until it reports done. This avoids an invalid or missing API key blocking work.

Common flows start here:
- guide:    call agent_guide first for the full ordered flow chains and decision trees
- auth:     auth_status -> auth_sso -> auth_resume (then auth_status to verify)
- vault:    vault_create -> vault_create_resume -> vault_status; restore via vault_restore -> vault_restore_resume
- pins:     pins_add / pins_list / pins_status
- publish:  upload_file -> websites_create (see agent_guide for domain/label/custom-domain branching)
- search:   search_tools({ "query": "<one keyword>" })
- filter:   search_tools({ "category": "vault", "query": "<one keyword>" })

Some internal commands are human-only or read piped stdin; when an agent invokes one via invoke_tool, the server returns a structured needs_human redirect instead of blocking. Commands that prompt interactively are hidden from search_tools entirely.

The internal catalog has {{.ToolCount}} tools.
{{if .FileHostInput}}
File input: when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox), pass it via the ` + "`file`" + ` parameter on upload_file/vault_put_file. Do NOT base64-encode, create a data URI, or manually construct the download_url object. The host runtime resolves the file reference into a temporary download_url + file_id this tool receives.
{{end}}{{if .SourcePath}}
File source: use source.mode=path with a host-side file/directory/archive path. The server reads it directly because it shares the host filesystem. Local path arguments refer to the MCP server host, not a remote agent's filesystem.
{{end}}{{if .SourceMint}}
File source: use source.mode=mint to get a one-time presigned HTTP PUT endpoint. Stream bytes to it with curl, then poll upload_status with the returned upload_handle.
{{end}}{{if .SourceRelay}}
File source: use source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI). The server fetches/decodes and uploads them.
{{end}}{{if .MCPApps}}
Companion interactive pages (MCP Apps) may render alongside tool results when a tool returns a needs_human redirect. These are ui:// resources rendered by the host; the model always reads content[].text for the canonical result.
{{end}}`))

// instructionsData is the template execution context.
type instructionsData struct {
	ToolCount      int
	FileHostInput  bool
	SourcePath     bool
	SourceMint     bool
	SourceRelay    bool
	MCPApps        bool
}

// buildInstructions returns the MCP server instructions for the given
// platform. The instructions are platform-aware: they only mention file
// input modes, sinks, and UI features the platform actually supports.
func buildInstructions(profile hostenv.PlatformProfile, toolCount int) string {
	var buf strings.Builder
	_ = instructionsTemplate.Execute(&buf, instructionsData{
		ToolCount:     toolCount,
		FileHostInput: profile.Has(hostenv.FeatFileHostInput),
		SourcePath:    profile.Has(hostenv.FeatSourcePath),
		SourceMint:    profile.Has(hostenv.FeatSourceMint),
		SourceRelay:   profile.Has(hostenv.FeatSourceURL) || profile.Has(hostenv.FeatSourceData),
		MCPApps:       profile.Has(hostenv.FeatMCPApps),
	})
	return buf.String()
}
