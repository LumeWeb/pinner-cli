// Package mcp adapts a urfave/cli/v3 command tree into an MCP (Model Context
// Protocol) server. It was originally based on thepwagner/urfave-cli-mcp
// (https://github.com/thepwagner/urfave-cli-mcp) and extended with support
// for additional flag types (Float, Duration, StringSlice) and minor
// robustness improvements.
//
// Original source: https://github.com/thepwagner/urfave-cli-mcp
// Original license: MIT (see LICENSE in upstream repository)
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/cors"
	"github.com/urfave/cli/v3"
	mcptransfer "go.lumeweb.com/mcpplane/transfer"
	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/build"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	corevault "go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
	"go.uber.org/zap"

	"go.lumeweb.com/mcpplane/session"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// ToolDelimiter separates command path segments in MCP tool names.
const ToolDelimiter = "_"

// ansiEscapeRE matches ANSI/VT escape sequences (SGR color codes, cursor
// movement, erase, reset) so agent-facing tool output is always clean plain
// text. The CLI's human formatter colors status text (e.g. \x1b[32mpinned\x1b[0m);
// even with --agent forcing JSON, strip any stray escape sequence at the MCP
// boundary so a terminal code can never reach an agent.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[PX^_].*?\x1b\\`)

// stripANSI removes ANSI/VT escape sequences from s.
func stripANSI(s string) string { return ansiEscapeRE.ReplaceAllString(s, "") }

// healthzHandler is the unauthenticated liveness probe used by PaaS/container
// health checks (Railway, Koyeb, Render, Fly, Cloud Run, DO). It always returns
// 200 {"ok":true} once the transport mux is serving. It is deliberately outside
// the bearer-token/OAuth guards so orchestrators can probe without credentials.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// The progressive-disclosure meta-tool names are a single authoritative source.
// Every place that must agree on the membership — the RegisterOfficialMetaTools
// registration path (sdk_official.go), the server card derivation (below),
// and the tests — references these constants. There is deliberately no second
// literal copy: changing a name here must update registration and the card
// together rather than letting them drift apart.
const (
	toolSearchTools           = "search_tools"
	toolDescribeTool          = "describe_tool"
	toolInvokeReadTool        = "invoke_read_tool"
	toolInvokeWriteTool       = "invoke_write_tool"
	toolInvokeDestructiveTool = "invoke_destructive_tool"
)

// metaToolNames is the fixed progressive-disclosure meta-tool set registered
// by RegisterOfficialMetaTools (search_tools, describe_tool, and the typed
// invoke dispatchers invoke_read_tool / invoke_write_tool /
// invoke_destructive_tool). It is derived from the authoritative name
// constants (toolSearchTools etc.), so registration and this listing can never
// drift apart. These five are part of the direct tool surface alongside the
// direct set, so server-card membership treats them as policy and derives
// them from here rather than a separate hardcoded mirror.
var metaToolNames = []string{
	toolSearchTools,
	toolDescribeTool,
	toolInvokeReadTool,
	toolInvokeWriteTool,
	toolInvokeDestructiveTool,
}

// serverCardToolDescriptions is the stable human-readable description shown on
// the static server card for each derived tool. It is display metadata ONLY and
// never decides which tools appear on the card. COMPATIBILITY TABLE (documented
// fallback): it is consulted FIRST for the direct/meta names so the long-served
// card copy directory scanners index cannot drift, and for a finalized
// MaterializedTooling every name NOT in this table resolves from the surface's
// own descriptor/entry description instead of degenerating to the tool name.
//
// IMPORTANT — what the card represents: under the default progressive strategy
// it is the direct + meta surface (directToolNamesFor(surface) +
// metaToolNames). Under the flat strategy it is the actually-materialized
// agent-safe direct surface (the catalog's DirectVisible entries, which are far
// larger than the direct + meta set) plus the meta-tools only when
// IncludeMetaOnFlat keeps them on the wire. The descriptions below cover the
// direct + meta names; any flat surface beyond those falls back to the tool
// name itself.
// A name that has no entry falls back to the tool name itself on the card.
var serverCardToolDescriptions = map[string]string{
	"auth_status":             "Check authentication status",
	"vault_create":            "Create a new encrypted vault",
	"vault_restore":           "Restore a vault from a recovery seed",
	"vault_status":            "Get vault status",
	"vault_share_accept":      "Accept an encrypted vault share",
	"websites_create":         "Create a new website deployment",
	"websites_get":            "Get a deployed website",
	"search_tools":            "Search the tool catalog",
	"describe_tool":           "Get a tool's input schema",
	"invoke_read_tool":        "Invoke a read-only catalog tool",
	"invoke_write_tool":       "Invoke a mutating catalog tool",
	"invoke_destructive_tool": "Invoke a destructive catalog tool",
}

// cardCatalogVar is a TEST-ONLY observation seam: the tests that drive the
// legacy deriveServerCardTools helper directly record the catalog whose card
// they want to compare against. Production never writes or reads it —
// serveHTTP builds an immutable ServerCard from each assembled catalog
// (NewServerCard) and registers that as the served handler, and even this
// helper derives listing policy from the recorded catalog's own captured
// fields, never from the deprecated construction-time globals.
var cardCatalogVar *ToolCatalog

// ServerCard is the immutable per-server view of the static MCP server card. It
// captures every value the card needs (surface, listing strategy, meta-on-flat,
// and the materialized catalog plus its direct custom tools) ONCE at server
// construction, so the served card reflects that server's policy rather than
// mutable package globals read at request time. Two servers built sequentially
// each get their own ServerCard and cannot contaminate one another.
type ServerCard struct {
	surface           DomainScope
	hosted            bool
	strategy          ToolListingStrategy
	includeMetaOnFlat bool
	// catalog is the fully-materialized ToolCatalog for this server (captured
	// after registerCustomTools runs), used to derive the flat direct surface from
	// actual materialized entries rather than a name list.
	catalog *ToolCatalog
}

// NewServerCard captures the server-card state for the given materialized
// catalog. The catalog must already carry its Strategy / IncludeMetaOnFlat /
// DomainScope / Hosted / DirectCustom state (set by buildCatalog and the
// custom-tool registry), i.e. it should be captured after assemble completes.
// It reads only the catalog's own fields — never package globals — so the
// returned value is immutable and per-server.
func NewServerCard(catalog *ToolCatalog) *ServerCard {
	if catalog == nil {
		return &ServerCard{surface: FullDomainScope, catalog: nil}
	}
	return &ServerCard{
		surface:           catalog.DomainScope,
		hosted:            catalog.Hosted,
		strategy:          catalog.Strategy,
		includeMetaOnFlat: catalog.IncludeMetaOnFlat,
		catalog:           catalog,
	}
}

// Tools returns the card tool list derived from this server's captured state.
// When the catalog carries a finalized one-pass MaterializedTooling (the
// production assembly path), the card derives from that single authoritative
// record — the same membership facts the direct tools/list projection
// registered. Catalogs assembled WITHOUT the plan (legacy/tests) fall back to
// the pre-plan derivation over the catalog's captured fields plus the
// DirectCustom side channel (documented compatibility fallback).
func (c *ServerCard) Tools() []map[string]any {
	// Read the finalized surface through the canonical synchronized accessor
	// (FinalizedTooling takes the catalog's own RLock) so a card served on a
	// racing goroutine can never observe a torn/unset write from setFinalized.
	// It takes its own lock — do not hold c.catalog.mu (or any other lock)
	// across this call.
	if c.catalog != nil {
		if surf := c.catalog.FinalizedTooling(); surf != nil {
			return surf.cardTools(c.surface, c.strategy)
		}
	}
	return deriveCardTools(c.surface, c.strategy, c.includeMetaOnFlat, c.catalog)
}

// ServeHTTP writes the static MCP server card JSON for this captured server.
func (c *ServerCard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	card := buildServerCardJSON(c.Tools())
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(card)
}

// buildServerCardJSON assembles the static server-card JSON envelope around the
// given tool list. Shared by the global-backed handler and the immutable
// ServerCard handler so the shape can never drift.
func buildServerCardJSON(tools []map[string]any) map[string]any {
	return map[string]any{
		"serverInfo": map[string]any{
			"name":    "pinner",
			"version": build.Version,
		},
		"authentication": map[string]any{
			"required": true,
			"schemes":  []string{"bearer"},
		},
		"tools":     tools,
		"resources": []any{},
		"prompts":   []any{},
	}
}

// deriveCardTools builds the tools list for the server card from explicit
// captured state (surface, listing strategy, meta-on-flat, and the materialized
// catalog) rather than package globals. It is the single derivation used both by
// the immutable ServerCard (production) and by the global-backed test helpers.
//
//   - ListingProgressive (default): the direct set for the surface
//     INTERSECTED with the actually-materialized direct entries, plus the
//     progressive-disclosure meta-tools. Intersecting avoids advertising a
//     direct name (e.g. a vault tool) that gated/incomplete dependencies never
//     materialized into the catalog. When no catalog is available (no assembly
//     recorded) it falls back to the full direct set — the only genuinely
//     needed static fallback.
//   - ListingFlat: the agent-safe direct set actually materialized (the
//     catalog's DirectVisible entries) plus the direct custom tools registered
//     outside catalog indexing (DirectCustom), plus the meta-tools iff
//     IncludeMetaOnFlat keeps them on the wire. This matches flat registration
//     exactly, so the card never advertises a meta tool that is absent and never
//     omits an actually-registered direct tool.
func deriveCardTools(surface DomainScope, strategy ToolListingStrategy, includeMetaOnFlat bool, catalog *ToolCatalog) []map[string]any {
	if strategy == ListingFlat {
		return deriveCardToolsFlat(surface, includeMetaOnFlat, catalog)
	}
	return deriveCardToolsProgressive(surface, catalog)
}

// deriveCardToolsProgressive returns the direct (intersected with materialized
// entries) + meta surface a progressive server advertises on tools/list.
func deriveCardToolsProgressive(surface DomainScope, catalog *ToolCatalog) []map[string]any {
	var names []string
	if catalog != nil {
		names = materializedDirectNames(surface, catalog)
	} else {
		// No materialized catalog recorded: fall back to the full direct set
		// (the only genuinely needed static fallback — buildCatalog always
		// records a catalog in production).
		names = directToolNamesFor(surface)
	}
	names = append(names, metaToolNames...)
	return serverCardToolsFor(names)
}

// materializedDirectNames returns the direct names for the surface that are
// actually present among the catalog's DirectVisible entries, so a progressive
// server card never advertises a direct name that gated/incomplete dependencies
// failed to materialize.
func materializedDirectNames(surface DomainScope, catalog *ToolCatalog) []string {
	present := make(map[string]bool)
	for _, e := range catalog.Entries() {
		if isDirectCatalogEntry(e) {
			present[e.Name] = true
		}
	}
	var out []string
	for _, n := range directToolNamesFor(surface) {
		if present[n] {
			out = append(out, n)
		}
	}
	return out
}

// deriveCardToolsFlat returns the tool surface a flat server advertises: the
// actually-materialized agent-safe direct tools from the catalog, plus the
// direct custom tools registered outside catalog indexing (upload_file /
// vault_put_file in the no-app mode), plus the meta-tools only when
// includeMetaOnFlat keeps them on tools/list. Without a recorded catalog (nil —
// possible before buildCatalog records it) it falls back to the surface-specific
// direct set as a conservative subset rather than inventing names.
func deriveCardToolsFlat(surface DomainScope, includeMetaOnFlat bool, catalog *ToolCatalog) []map[string]any {
	var names []string
	if catalog != nil {
		for _, entry := range catalog.Entries() {
			if isDirectCatalogEntry(entry) {
				names = append(names, entry.Name)
			}
		}
		// Direct custom tools registered straight onto tools/list that are NOT
		// catalog entries must be advertised too, so the flat card matches the
		// wire exactly. Dedupe defensively in case a name also appears as a
		// catalog entry.
		seen := make(map[string]bool, len(names))
		for _, n := range names {
			seen[n] = true
		}
		for _, n := range catalog.DirectCustom {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	} else {
		// No materialized catalog recorded yet: fall back to the surface-specific
		// direct set (a conservative subset of the flat direct surface) rather
		// than inventing names. Use the given surface so a hosted flat card never
		// advertises a full-surface op (e.g. a vault tool) a hosted server omits.
		names = append([]string{}, directToolNamesFor(surface)...)
	}
	if includeMetaOnFlat {
		names = append(names, metaToolNames...)
	}
	// The flat surface is derived from the catalog map, whose iteration order is
	// non-deterministic; sort so the card output is stable across calls and
	// servers (progressive's direct+meta order is already deterministic above).
	sort.Strings(names)
	return serverCardToolsFor(names)
}

// deriveServerCardTools builds the tools list for the static MCP server card
// for the given surface, branching on the listing policy CAPTURED IN THE
// catalog recorded under cardCatalogVar (the test-only observation seam). It
// is the legacy helper kept for the tests that drive derivation directly;
// production serves from an immutable ServerCard (NewServerCard → deriveCardTools)
// instead. It deliberately does not consult the deprecated construction-time
// globals, so mutating them cannot alter the derived card.
func deriveServerCardTools(s DomainScope) []map[string]any {
	return deriveCardTools(s, cardCatalogVar.listingStrategy(), cardCatalogVar.metaOnFlat(), cardCatalogVar)
}

// cardToolDescription is the ONE card-description projection every server-card
// path shares: the long-served serverCardToolDescriptions compatibility table
// (display copy directory scanners index must not drift) first, then the
// finalized MaterializedTooling description (direct descriptor copy, then
// catalog entry) when one is recorded, then the materialized catalog entry,
// and empty — meaning the caller falls back to the tool name itself. A direct
// descriptor unknown to the compatibility table therefore yields its ACTUAL
// description on both the finalized path (finalized != nil) and the legacy
// catalog-backed path (finalized == nil).
func cardToolDescription(name string, catalog *ToolCatalog, finalized *MaterializedTooling) string {
	if desc := serverCardToolDescriptions[name]; desc != "" {
		return desc
	}
	if finalized == nil && catalog != nil {
		finalized = catalog.FinalizedTooling()
	}
	if finalized != nil {
		return finalized.description(name)
	}
	if catalog != nil {
		if entry, ok := catalog.Get(name); ok {
			return entry.Description
		}
	}
	return ""
}

// serverCardToolsFor builds the card tool list (name + display description) for
// a concrete name set using the shared cardToolDescription projection against
// the recorded catalog (the test-only observation seam the legacy derivation
// runs against). Unknown names fall back to their own name as the description.
func serverCardToolsFor(names []string) []map[string]any {
	return serverCardToolsForDescriptions(names, func(n string) string {
		return cardToolDescription(n, cardCatalogVar, cardCatalogVar.FinalizedTooling())
	})
}

// serverCardToolsForDescriptions builds the card tool list (name + description)
// for a concrete name set, resolving each description through resolve. The
// fallback chain everywhere is: the serverCardToolDescriptions compatibility
// table (the long-served direct/meta display copy), then the surface's own
// descriptor/entry description, then the name itself.
func serverCardToolsForDescriptions(names []string, resolve func(name string) string) []map[string]any {
	tools := make([]map[string]any, 0, len(names))
	for _, n := range names {
		desc := resolve(n)
		if desc == "" {
			desc = n
		}
		tools = append(tools, map[string]any{"name": n, "description": desc})
	}
	return tools
}

// serverCardHandler serves the static MCP server card used by directory
// scanners (Smithery, etc.). It is unauthenticated like /healthz. It derives the
// card from the package construction-time globals and is retained for the
// pre-refactor handler test; production HTTP servers register an immutable
// ServerCard (see ServerCard.ServeHTTP) instead.
func serverCardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Derive the tool list from the active surface's direct + meta set.
	card := buildServerCardJSON(deriveServerCardTools(activeDomainScope()))
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(card)
}

// log is the package-level zap logger for the MCP adapter and its out-of-band
// auth coordinators. It is a settable variable so a user-configured logger
// (built from the mcp command's --log-level/--log-format flags) replaces the
// default. The default uses a production config (Info level, JSON encoder) to
// avoid leaking debug output (including stderr buffers) onto the stdio JSON-RPC
// transport.
var log = zap.Must(zap.NewProduction())

// setPackageLogger installs a user-configured logger as the shared package
// logger. Components that hold their own logger reference are unaffected; this
// updates the fallback used by call sites reading the package-level log.
func setPackageLogger(l *zap.Logger) {
	log = l
}

// resolveHostUploadVault resolves the profile-aware upload_file and
// vault_put_file descriptions for a detected host profile. These are what a
// dedicated per-host HTTP server presents in tools/list; the startup server
// instead bakes the transport-only descriptions (resolved once at startup).
// The MCPTargets descriptors keep their target lists so describe_tool and
// search_tools continue to resolve per request.
func resolveHostUploadVault(profile hostenv.PlatformProfile) (uploadDesc, vaultDesc string) {
	u, _ := toolforge.ResolveDescription(toolforge.UploadFileTargets, profile)
	v, _ := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, profile)
	return u, v
}

// uploadVaultMatchesTransport reports whether the startup server — whose
// upload_file / vault_put_file descriptions are resolved for the given
// transport — already presents the same upload/vault surface as the detected
// host profile. When it does, the host can reuse the shared startup server
// (avoiding an expensive per-host rebuild); when it does not (e.g. an
// OpenAI-over-HTTP host whose FeatFileHostInput demands the file-handoff
// presentation over a mint-only HTTP transport), a dedicated per-host server
// is required so tools/list advertises the right surface.
func uploadVaultMatchesTransport(profile hostenv.PlatformProfile, transport hostenv.TransportKind) bool {
	base := hostenv.ProfileForTransport(transport)
	baseUpload, _ := toolforge.ResolveDescription(toolforge.UploadFileTargets, base)
	baseVault, _ := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, base)
	uploadDesc, vaultDesc := resolveHostUploadVault(profile)
	if uploadDesc != baseUpload || vaultDesc != baseVault {
		return false
	}
	// The startup server registers feature-gated relay tools (upload_url on
	// FeatSourceURL, upload_data on FeatSourceData) from the transport's
	// generic profile. A host whose capability features declare those relay
	// features must get a dedicated server so the relay tools are actually
	// registered for it — reusing the startup server would bake the
	// generic-HTTP feature set (no data/url) and silently drop the tools.
	// The upload_file/vault_put_file descriptions are transport-bound, so they
	// would compare equal here; the relay-feature check is what forces Grok
	// (which declares FeatSourceData/FeatSourceURL) onto its own server.
	return base.Features.Has(hostenv.FeatSourceURL) == profile.Features.Has(hostenv.FeatSourceURL) &&
		base.Features.Has(hostenv.FeatSourceData) == profile.Features.Has(hostenv.FeatSourceData)
}

// mcpServerFlags returns the flags for the `mcp` command. Env-backed flags
// declare their MCP_* environment variable via Sources so the urfave/cli
// framework resolves flag -> env -> default with no ad-hoc env parsing; the
// values are read back with cmd.String / cmd.Bool / cmd.Int in the action and
// serveHTTP.
func mcpServerFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "http",
			Value: false,
			Usage: "Serve over the streamable-HTTP transport instead of stdio (endpoint /mcp)",
		},
		&cli.StringFlag{
			Name:    "host",
			Value:   "127.0.0.1",
			Usage:   "Local bind host for the HTTP transport",
			Sources: cli.EnvVars("MCP_HOST"),
		},
		&cli.IntFlag{
			Name:    "port",
			Value:   0,
			Usage:   "Local bind port for the HTTP transport (0 picks a free port)",
			Sources: cli.EnvVars("MCP_PORT"),
		},
		&cli.StringFlag{
			Name:    "tunnel",
			Usage:   "Tunnel provider: ngrok, cloudflared, or openai. openai requires --tunnel-id; ngrok requires --token or NGROK_AUTHTOKEN",
			Sources: cli.EnvVars("MCP_TUNNEL_PROVIDER"),
		},
		&cli.StringFlag{
			Name:    "domain",
			Usage:   "Custom domain for the tunnel (required for cloudflared, optional for ngrok on paid accounts)",
			Sources: cli.EnvVars("MCP_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Tunnel provider account token (e.g. ngrok authtoken). May also be set via the provider env var or config file",
			Sources: cli.EnvVars("MCP_TUNNEL_TOKEN", "NGROK_AUTHTOKEN"),
		},
		&cli.StringFlag{
			Name:    "tunnel-name",
			Usage:   "Cloudflare tunnel resource name (default: pinner-mcp)",
			Sources: cli.EnvVars("MCP_TUNNEL_NAME"),
		},
		&cli.StringFlag{
			Name:    "tunnel-id",
			Usage:   "OpenAI Secure MCP Tunnel ID (required with --tunnel openai). May also be set via CONTROL_PLANE_TUNNEL_ID or the pinner config manager",
			Sources: cli.EnvVars("MCP_TUNNEL_ID", "CONTROL_PLANE_TUNNEL_ID"),
		},
		&cli.StringFlag{
			Name:    "auth-token",
			Usage:   "Shared secret used to authorize public HTTP MCP endpoints. In OAuth mode (--oauth) the resource owner enters it on the login page as a password; otherwise it is accepted directly as a Bearer token. Required for ngrok and cloudflared; not used by the embedded OpenAI tunnel",
			Sources: cli.EnvVars("MCP_AUTH_TOKEN"),
		},
		&cli.BoolFlag{
			Name:    "oauth",
			Usage:   "Enable the OAuth 2.1 handshake (authorize/token/discovery endpoints). Without this, --auth-token is accepted directly as a Bearer token. Use --oauth to let OAuth-expecting MCP clients (ChatGPT, Claude.ai, Copilot, Vertex) authorize",
			Sources: cli.EnvVars("MCP_OAUTH"),
		},
		&cli.StringFlag{
			Name:    "public-url",
			Usage:   "Public base URL advertised in OAuth discovery metadata (issuer, authorize/token endpoints). Defaults to the tunnel URL when --tunnel is set, or the loopback address otherwise",
			Sources: cli.EnvVars("MCP_PUBLIC_URL"),
		},
		&cli.BoolFlag{
			Name:    "cors",
			Usage:   "Enable CORS for the HTTP transport, reflecting the request Origin (Access-Control-Allow-Origin echoes the client's Origin; Vary: Origin is set). Useful for browser-based MCP clients. Applies to all mounted endpoints (MCP and out-of-band)",
			Sources: cli.EnvVars("MCP_CORS"),
		},
		&cli.StringFlag{
			Name:  "log-level",
			Value: "info",
			Usage: "Log level for the MCP server and its out-of-band auth components: debug, info, warn, error",
		},
		&cli.StringFlag{
			Name:  "log-format",
			Value: "json",
			Usage: "Log encoding for the MCP server: json (default) or console",
		},
		&cli.BoolFlag{
			Name:    "dev-tools",
			Usage:   "Enable developer introspection tools (dev_host_env, dev_profile, dev_request) and capture the raw wire snapshot of the connected host. Intended for debugging the MCP server and host-env detection; these tools are read-only and absent from the surface unless this flag is set",
			Sources: cli.EnvVars("MCP_DEV_TOOLS"),
		},
		&cli.DurationFlag{
			Name:    "vault-sync-interval",
			Value:   corevault.SyncLoopInterval,
			Usage:   "Idle cadence of the background vault sync loop that keeps the active vault's local cache converged with the indexer (0 disables it). Ticks that find pending events re-run immediately, so this is the idle interval, not a worst-case bound",
			Sources: cli.EnvVars("PINNER_VAULT_SYNC_INTERVAL"),
		},
	}
}

// startVaultSync starts the background continuous vault sync loop for the
// active vault profile if it was wired (WithVaultSync) and not disabled via
// --vault-sync-interval 0. It returns immediately after starting the loop; the
// loop runs until the server's ctx is cancelled. A VaultSyncLoop reuses one
// VaultService across idle ticks and rebuilds it only when the resolved active
// profile changes (see corevault.VaultSyncLoop).
func startVaultSync(ctx context.Context, cmd *cli.Command, mcpOpts *mcpServerOptions) error {
	if mcpOpts.vaultSyncCfg.Service == nil {
		// No WithVaultSync wiring; no continuous sync.
		return nil
	}
	interval := cmd.Duration("vault-sync-interval")
	if interval <= 0 {
		// --vault-sync-interval 0 explicitly disables continuous sync.
		log.Debug("continuous vault sync disabled (vault-sync-interval=0)")
		return nil
	}

	syncCtx, cancel := context.WithCancel(ctx)
	loop := corevault.NewVaultSyncLoop(mcpOpts.vaultSyncCfg)
	// The background upload flush reuses the same per-profile service factory:
	// it drains staged ("pending") writes to durable Sia storage, packing them
	// into shared slabs. It runs as a sibling worker on the same scheduler so
	// staged uploads converge while the server runs.
	uploadLoop := corevault.NewVaultUploadLoop(mcpOpts.vaultSyncCfg)
	sched := corevault.NewServiceScheduler()
	sched.Register("vaultSync", interval, loop.Tick)
	sched.Register("vaultUpload", interval, uploadLoop.Tick)
	sched.Start(syncCtx)
	go func() {
		// Shutdown when the server context is done (signal/file-server exit),
		// so the sync/upload goroutines never outlive the process and the held
		// services (SDK/DB handles) are released.
		<-ctx.Done()
		cancel()
		sched.Shutdown()
		loop.Close()
		uploadLoop.Close()
		// Release the process-wide per-profile flush manager's held services so
		// a long-running server does not leak SDK/DB handles. Registered by the
		// CLI wiring (core/vault is a neutral package, so this import is safe).
		corevault.CloseFlushManager()
	}()
	log.Debug("continuous vault sync started", zap.Duration("interval", interval))
	return nil
}

// oauthStorePath returns the filesystem path of the OAuth state SQLite file.
// Like the CLI's config, it lives under the user config dir under pinner/ so a
// long-running or restarted MCP server keeps durable OAuth clients and refresh
// tokens. Falls back to ~/.pinner on platforms without a standard config dir.
func oauthStorePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, ".pinner", "mcp-oauth.db")
		}
		return "mcp-oauth.db"
	}
	return filepath.Join(base, "pinner", "mcp-oauth.db")
}

// serveHTTP serves an MCP server over the streamable-HTTP transport, binding
// to the local address derived from the --host/--port flags. When --tunnel is
// set, it starts and manages the selected tunnel so a remote MCP client can
// reach the server over a public URL, then blocks until ctx is cancelled.
// oob, when provided, is the out-of-band login coordinator: its /login/
// handlers are mounted on the shared mux (reachable without the transport
// bearer token, like the OAuth authorize page) so a remote human can open the
// login URL on the public/tunnel URL rather than an unreachable loopback.
// seedDrop and oobRestore, when provided, mount the one-time seed and restore
// URLs on the same shared mux. oobCreate mounts the one-time create URL.
// curlUpload, when provided, mounts the one-time upload PUT route on the
// shared mux (the Upload coordinator in HTTP/tunnel mode).
//
// mcpHostProtectionDisabled reports whether the go-sdk's DNS-rebinding guard
// must be disabled for this serve. When the server is reached over a
// non-loopback public origin, remote clients send that hostname as the Host
// header while the server sees a loopback local address, which the guard would
// reject with 403. Disable it whenever the server is exposed publicly: when a
// tunnel fronts the loopback listener (`tunnelActive`), or when the user
// explicitly serves HTTP with a --public-url (e.g. behind their own external
// reverse proxy or a manually managed tunnel). Keep it on for direct loopback
// serving. See serveHTTP for the caller.
func mcpHostProtectionDisabled(tunnelActive, httpMode bool, publicURL string) bool {
	return tunnelActive || (httpMode && publicURL != "")
}

// generateOAuthSecret returns a random 32-byte hex string suitable for use
// as the shared OAuth login secret on a localhost server with no tunnel.
func generateOAuthSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// corsHandler wraps next with CORS middleware that reflects the request Origin
// dynamically: Access-Control-Allow-Origin echoes whatever Origin header the
// client sent (with Vary: Origin), so no static allow-list is needed. The
// allowed methods/headers and exposed headers cover the streamable-HTTP MCP
// transport as seen by a browser client. Preflight OPTIONS requests are
// answered by the middleware. reflect-origin + no AllowCredentials deliberately
// mirrors an Access-Control-Allow-Origin: <origin> response (never "*").
func corsHandler(next http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			// Admit any request Origin and reflect it back; the browser's
			// same-origin policy plus the transport auth still gate access.
			return true
		},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"Mcp-Session-Id",
			"MCP-Protocol-Version",
			"Last-Event-ID",
		},
		ExposedHeaders: []string{
			"Mcp-Session-Id",
		},
	}).Handler(next)
}

// mcpServerOptions carries resolved MCP command configuration.
type mcpServerOptions struct {
	// prompts enables registration of the prompt templates.
	prompts           bool
	uploadHandler     mcptransfer.UploadHandler
	vaultPutHandler   vault.VaultPutHandler
	uploadTasks       *mcptransfer.UploadTaskManager
	relayURLUpload    mcptransfer.RelayURLUploadHandler
	relayAllowedHosts []string
	dataURIUpload     mcptransfer.DataURIUploadHandler
	localPathUpload   mcptransfer.LocalPathUploadHandler
	localPathVaultPut vault.LocalPathVaultPutHandler
	// ipfsDownload is the authenticated IPFS download executor used by the
	// download_file tool's local sink (it streams a CID's bytes to a writer).
	// Homing it in the CLI layer mirrors upload; the tool never decides the
	// mechanism.
	ipfsDownload transfer.IPFSDownloadHandler
	// vaultGet is the authenticated vault-read executor used by the
	// vault_get_file tool's sinks (it streams a vault file's decrypted bytes
	// to a writer). Mirror of vaultPutHandler.
	vaultGet transfer.VaultGetHandler
	// downloadTrustedOrigins are additional origins (beyond the server's own
	// base/loopback origin) that the filedrop GET routes reflect over CORS for
	// the ui:// app. Configured for deployments where the app iframe is served
	// from an MCP-host origin distinct from the Pinner server origin.
	downloadTrustedOrigins []string
	// downloadRoot confines download_file / vault_get_file local-sink writes to
	// a single host directory. Resolved lazily from config at registration; a
	// caller-supplied output_path is resolved relative to it and rejected if it
	// escapes. Empty means "use config default (<config-dir>/downloads)".
	downloadRoot func() string
	// uploadTrustedOrigins are additional origins (beyond the server's own
	// base/loopback origin) that the presigned PUT routes reflect over CORS for
	// the Uppy XHR uploader. Configured for deployments where the ui:// app
	// iframe is served from an MCP host origin distinct from the Pinner server
	// origin. See LoopbackServer.AddTrustedOrigins.
	uploadTrustedOrigins []string
	// maxRelayBytes is the per-tool cap (in bytes) for MCP file uploads,
	// overriding the package default (512 MiB). 0 means "use the default".
	// It is honored across the relay URL, data URI, and capability-report
	// surfaces. Resolved lazily from the config manager at server setup.
	maxRelayBytes int64
	// pinnerPins, when set, wires the "Create a Pin" MCP App (ui:// view,
	// app-only status helper) using a live pinning provider built at setup.
	pinnerPins apps.PinningProviderFactory
	// catalogDeps, when set, supplies the operation-catalog dependency graph
	// (config manager + core service factories) so the MCP surface can be
	// populated from the operation catalog. Since the compiler-backed surface
	// is the only source, a nil bundle fails fast at buildCatalog time rather
	// than silently serving an empty catalog.
	catalogDeps func() *CatalogDepsBundle
	// vaultSyncCfg, when its Service function is set, starts a background
	// continuous vault sync loop ("for any active vault") while the MCP server
	// runs. A zero-value cfg disables the loop (no background syncing).
	vaultSyncCfg corevault.SyncLoopConfig
}

// MCPServerOption configures the MCP command served by MCPCommand.
type MCPServerOption func(*mcpServerOptions)

// ResourceProvidersFactory builds ResourceProviders at Action time, when the
// session store and other runtime deps are available.
type ResourceProvidersFactory func(store *session.SessionStore) ResourceProviders

// WithPrompts attaches MCP prompt templates (website-onboarding, setup).
func WithPrompts() MCPServerOption {
	return func(o *mcpServerOptions) {
		o.prompts = true
	}
}

// WithPinningProvider wires the "Create a Pin" MCP App (ui:// view + app-only
// pin status helper) using provider, which builds a live pinning backend at
// server setup time. Without it, no pin App is registered.
func WithPinningProvider(provider apps.PinningProviderFactory) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.pinnerPins = provider
	}
}

// WithUploadHandler registers the authenticated IPFS upload executor used by
// the upload_file tool's relay/data source modes (OpenAI tunnel) and the async
// upload manager. Passing nil disables the relay path.
func WithUploadHandler(handler mcptransfer.UploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadHandler = handler
	}
}

// WithVaultPutHandler registers the authenticated vault write executor used by
// the vault_put_file tool's relay (OpenAI tunnel) source modes.
func WithVaultPutHandler(handler vault.VaultPutHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultPutHandler = handler
	}
}

// WithIPFSDownload registers the authenticated IPFS download executor used by
// the download_file tool's local sink. Passing nil disables the download tool.
func WithIPFSDownload(handler transfer.IPFSDownloadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.ipfsDownload = handler
	}
}

// WithVaultGet registers the authenticated vault-read executor used by the
// vault_get_file tool's sinks (it streams a vault file's decrypted bytes to a
// writer). Mirror of WithVaultPutHandler. Passing nil disables the tool.
func WithVaultGet(handler transfer.VaultGetHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultGet = handler
	}
}

// WithDownloadRoot sets the supplier for the host directory that confines
// download_file / vault_get_file local-sink writes. The supplier is called at
// registration (and must return an absolute path); a caller-supplied
// output_path is resolved relative to it and rejected if it escapes. When nil,
// the config default (<config-dir>/downloads) is used.
func WithDownloadRoot(supplier func() string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.downloadRoot = supplier
	}
}

// WithUploadTaskManager registers async upload-management tools backed by the
// given manager. Passing nil disables them.
func WithUploadTaskManager(mgr *mcptransfer.UploadTaskManager) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadTasks = mgr
	}
}

// WithUploadTrustedOrigins adds origins (beyond the server's own base/loopback
// origin) that the presigned PUT routes reflect over CORS for the Uppy XHR
// uploader. Use when the ui:// app iframe is served from an MCP host origin
// distinct from the Pinner server origin and that host is trusted; without
// this, only the server's own origin is reflected, so a cross-origin host
// cannot PUT. Trusted origins are appended to the endpoint's own origin, never
// replacing it, and are still scoped to the unguessable one-time token + (for
// the vault) the uploads path scope.
func WithUploadTrustedOrigins(origins ...string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadTrustedOrigins = append(o.uploadTrustedOrigins, origins...)
	}
}

// WithRelayURLUpload registers the generic relay URL upload tool
// (pinner_upload_url). allowedHosts restricts which hosts Pinner will fetch;
// pass nil/empty to allow any HTTPS host (subject to the SSRF dial guard).
func WithRelayURLUpload(handler mcptransfer.RelayURLUploadHandler, allowedHosts []string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.relayURLUpload = handler
		o.relayAllowedHosts = allowedHosts
	}
}

// WithDataURIUpload registers the draft SEP-2356 data: URI upload tool
// (pinner_upload_data). Passing nil disables it.
func WithDataURIUpload(handler mcptransfer.DataURIUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.dataURIUpload = handler
	}
}

// WithLocalPathUpload registers the co-located local-path upload handler that
// backs the consolidated upload_file tool's co-located branch.
// which uploads a host-side file/directory/archive directly. It is only
// meaningful when the MCP server is co-located with the caller's files.
func WithLocalPathUpload(handler mcptransfer.LocalPathUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.localPathUpload = handler
	}
}

// WithLocalPathVaultPut registers the SDIO local-path vault write used by the
// unified vault_put_file tool's stdio source mode, which writes a host-side
// file/directory/archive into the encrypted vault directly. It is only
// meaningful when the MCP server is co-located with the caller's files.
func WithLocalPathVaultPut(handler vault.LocalPathVaultPutHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.localPathVaultPut = handler
	}
}

// WithMaxMCPUploadSize sets the per-tool cap (in bytes) for MCP file uploads.
// The wiring supplies this from the config's max_mcp_upload_size, which
// defaults to 1 GiB when unset (see core.Config.GetMaxMCPUploadSize). The cap
// is honored across the relay URL, data URI, curl-upload, and local-path
// upload surfaces, and reported in the capability report.
//
// supplier is resolved lazily at server setup (inside the MCP command's
// Action), when the config manager is available — the same call pattern the
// wizard factory and catalog-ops bundle use. This keeps config reads out of
// command-construction time. If supplier is nil or panics (e.g. config not
// yet available), the option is a no-op and the package's fallback default
// (512 MiB, ieo.EffectiveRelayMaxBytes) is kept.
func WithMaxMCPUploadSize(supplier func() uint64) MCPServerOption {
	return func(o *mcpServerOptions) {
		if supplier == nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				o.maxRelayBytes = 0
			}
		}()
		o.maxRelayBytes = int64(supplier())
	}
}

// WithCatalogOps supplies the operation-catalog dependency graph (config
// manager + core service factories) so the MCP surface can be populated from
// the operation catalog. The factory is a closure built at Action time when
// config and services are available (mirroring WizardDepsFactory); it returns
// a fresh bundle per call so a test/global override stays live. Without it the
// catalog remains purely legacy-derived from the CLI command tree.
func WithCatalogOps(factory func() *CatalogDepsBundle) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.catalogDeps = factory
	}
}

// WithVaultSync enables the background continuous vault sync loop for any
// active vault while the MCP server runs. cfg carries the profile resolver and
// service builder used by the loop; a zero-value cfg (nil Service) disables
// the loop.
//
// The idle cadence and on/off switch are controlled by the
// --vault-sync-interval flag (default corevault.SyncLoopInterval; 0 disables
// the loop entirely).
//
// The loop mirrors the Sia Storage App's event-cursor sync-down: it drains the
// active vault's pending indexer events into the local cache every interval so
// an agent does not need to call vault_sync explicitly to see another device's
// writes. Writes are non-blocking: vault_put_file stages locally (status
// staged) — the canonical staged-write / durability-source / flush-job-shape
// contract lives in internal/mcp/mintcontract
// (see mintcontract.DurabilitySource / mintcontract.FlushJobShape), so there
// is no sync-up or dirty-flag component.
func WithVaultSync(cfg corevault.SyncLoopConfig) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultSyncCfg = cfg
	}
}

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (wizard.WebsitesWizardDeps, wizard.SetupWizardDeps, wizard.DomainWizardDeps, error)

// buildCatalog walks a urfave/cli/v3 command tree and populates a ToolCatalog
// with every invocable non-hidden command. The public command tree is
// cataloged identically for the official SDK builder (OfficialMCPServer).
// seedDrop, oobRestore, and oobCreate, when non-nil, let the tool handler mint
// one-time seed/restore/create URLs for vault-create/vault-restore agent output
// so the human can retrieve or supply a recovery seed in a browser without it
// transiting the MCP channel.
func buildCatalog(root *cli.Command, seedDrop *oobpkg.SeedDrop, oobRestore *oobpkg.OOBRestore, oobCreate *oobpkg.OOBCreate, handoffReg *handoff.HandoffRegistry, authHandles *session.AsyncHandleStore, opts ...buildCatalogOpt) (*ToolCatalog, error) {
	catalog := NewToolCatalog()

	// Apply the functional options (withCatalogDeps, withDomainScope). The surface
	// declares which operation domains/tool families this server exposes; it is
	// recorded on the catalog and as the package active surface so per-request
	// profile-aware resolution (agent_guide, startup profile) agrees with what
	// was actually registered.
	cfg := &buildCatalogConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	surface := cfg.resolveDomainScope()
	catalog.DomainScope = surface
	SetDomainScope(surface)
	// Record the deployment mode (hosted vs local) for the prompt DSL, so the
	// guide/prompts can gate hosted-specific copy without conflating it with
	// the domain surface. It is captured on the catalog too so a reassembled
	// (host-profile) server reuses the startup deployment mode (MEDIUM-3).
	SetHosted(cfg.hosted)
	catalog.Hosted = cfg.hosted
	// Resolve the effective meta-on-flat switch. A nil (unset) includeMetaOnFlat
	// resolves to the SAFE default true, so a no-policy build (or a partial
	// flat policy that omits the field) keeps the discovery meta-tools on a
	// flat surface rather than silently hiding gated ops (LOW-4 / MEDIUM-1).
	includeMeta := cfg.resolveIncludeMetaOnFlat()
	// Record the strategy, meta-on-flat switch, and onboarding override on the
	// catalog itself. The catalog is the SOLE production source of listing
	// policy: the materialization steps (stampDirectTools, RegisterOfficialMetaTools)
	// and the instructions selection (ToolCatalog.Instructions) read these
	// captured per-server fields, never the deprecated construction-time
	// globals, so interleaved builds cannot rebranch each other.
	// Strategy/IncludeMetaOnFlat are captured into the
	// per-server server card, and OnboardingOverride drives the onboarding path.
	catalog.Strategy = cfg.strategy
	catalog.IncludeMetaOnFlat = includeMeta
	catalog.OnboardingOverride = append([]string(nil), cfg.onboarding...)
	if cfg.catalogDeps != nil {
		catalog.CatalogDeps = cfg.catalogDeps
	}

	// Compiled operation surface. When catalogDeps is set, buildCatalog derives
	// the MCP tool surface from the operation catalog (compiler-backed
	// descriptions/schemas). Each compiled operation is surfaced as a ToolEntry
	// whose Handler routes through opmesh.Catalog.Invoke directly (see
	// populateCatalogTools), so there is no separate argv-dispatcher routing.
	var opsCat opmesh.Catalog
	if cfg.catalogDeps != nil {
		if deps := cfg.catalogDeps(); deps != nil {
			oc, err := AssembleCatalogOps(deps, surface, cfg.hosted)
			if err != nil {
				return nil, err
			}
			opsCat = oc
		}
	}
	// Record whether compiler mode is actually active (factory supplied AND
	// resolved non-nil) so registerCustomTools picks the same direct set
	// buildCatalog used. This is the single source of truth for the mode.
	catalog.CompilerMode = opsCat != nil

	// The compiler-backed operation catalog is the sole population mechanism:
	// the legacy CLI-tree walk is intentionally not run. A nil opsCat therefore
	// means there is no model surface at all (only transport/custom tools), so
	// fail fast with an explicit error instead of silently serving an empty
	// catalog. Callers must supply a resolving WithCatalogOps bundle.
	if opsCat == nil {
		return nil, fmt.Errorf("mcp: no catalog-deps bundle resolved; the compiler-backed surface is the only source and requires withCatalogDeps")
	}

	// Populate the catalog. The compiler-backed operation catalog is the single
	// source of truth for the MCP surface: the covered domains (auth,
	// vault/setup, pins, websites, dns, ipns, api-keys, operations) come from
	// populateCatalogTools below, and custom transport tools (SSO/resume,
	// wizards, upload backends) are layered by registerCustomTools. The legacy
	// CLI-tree walk is not run at all, so no pinner_* tools are produced.

	// Register the compiler-derived operation surface (auth, vault-setup,
	// vault, pins, websites, dns, ipns, api-keys, operations). These entries
	// carry the catalogops MCPTargets/typed schemas and dispatch through
	// the operation catalog's Invoke gate at runtime. stampDirectTools promotes the
	// compiled direct names to tools/list.
	names, err := populateCatalogToolsFor(catalog, opsCat, surface, cfg.hosted)
	if err != nil {
		return nil, err
	}
	_ = names // populateCatalogTools registers the compiled entries; the name set is informational only.
	// Route the compiled vault_create / vault_restore entries through the
	// out-of-band setup handlers, so a model invoking the compiled vault-setup
	// tool receives the full create_url / restore_url + resume-handle +
	// needs_human hand-off its MCPTargets fallback promises, rather than a bare
	// JSON-serialized VaultCreateHandoff/VaultRestoreHandoff{Profile} plaintext.
	routeVaultSetupHandlers(catalog,
		oobpkg.VaultCreateSetupHandler(oobCreate, handoffReg, authHandles),
		oobpkg.VaultRestoreSetupHandler(oobRestore, handoffReg, authHandles),
	)
	stampDirectTools(catalog)

	return catalog, nil
}

// routeVaultSetupHandlers swaps the compiled vault_create / vault_restore
// entries onto their out-of-band setup handlers. Beyond re-pointing the
// handler, it re-declares each entry's OutputSchema: catalogDescriptorToEntry
// stamps the {status:ok,value} success envelope on every compiled op, but the
// setup handlers return the NeedsHumanResult shape (status:needs_human plus
// reason/action_url/handle/resume_tool/detail), so the success envelope would
// misdescribe what these two tools actually emit. Routing them onto the
// needs_human schema keeps each tool's declared output matching its emitted
// StructuredContent.
func routeVaultSetupHandlers(catalog *ToolCatalog, create, restore model.ToolHandler) {
	if restoreEntry, ok := catalog.Get(vault.CompiledVaultRestoreToolName); ok {
		restoreEntry.Handler = restore
		restoreEntry.Interaction = model.InteractionAgentSafe
		restoreEntry.OutputSchema = catalogNeedsHumanOutputSchema
		catalog.Add(restoreEntry)
	}
	if createEntry, ok := catalog.Get(vault.CompiledVaultCreateToolName); ok {
		createEntry.Handler = create
		createEntry.Interaction = model.InteractionAgentSafe
		createEntry.OutputSchema = catalogNeedsHumanOutputSchema
		catalog.Add(createEntry)
	}
}

// catalogCountSentence is the ONE source of the closing catalog-count sentence
// every instruction variant embeds ("\n\nThe internal catalog has %d tools."):
// mcpInstructionsTailText continues it with the file-handoff guidance and
// buildInstructionsFromCatalog uses it verbatim as its tail, so the wording
// and format verb cannot drift between the two call sites.
func catalogCountSentence(count int) string {
	return fmt.Sprintf("\n\nThe internal catalog has %d tools.", count)
}

// mcpInstructionsTailText builds the closing prose shared by every instruction
// variant (progressive, flat, flat-no-meta): the internal-catalog count and the
// file-handoff / vault-transport guidance, defined ONCE here so the paragraph
// is edited in one place. Every NAMED tool claim inside it derives from the
// completed per-server membership: a hosted/minimal assembly whose surface
// never registered upload_file / vault_put_file / a vault read must not have
// those tools promised in its instructions. available==nil (the pure prose
// builder used by buildInstructionsFor's unit tests) renders the full
// historical copy — production always goes through ToolCatalog.Instructions.
func mcpInstructionsTailText(count int, available func(string) bool) string {
	has := func(names ...string) bool {
		if available == nil {
			return true
		}
		for _, n := range names {
			if !available(n) {
				return false
			}
		}
		return true
	}
	var b strings.Builder
	b.WriteString(catalogCountSentence(count))
	// The "vault copy" hand-off claim applies only when a vault file tool is
	// actually exposed: a restricted (hosted) assembly has no vault family at
	// all, so its tail must not steer an agent toward "vault copy" (there is
	// no vault_put_file/vault_get_file to hand bytes to). available==nil keeps
	// the full historical sentence for the pure-prose/legacy builder.
	b.WriteString(" Local path arguments refer to the MCP server host, not the remote agent's filesystem.")
	if available == nil || available("vault_put_file") || available("vault_get_file") {
		b.WriteString(" Upload and vault copy therefore require a host-side file handoff.")
	} else {
		b.WriteString(" Upload therefore requires a host-side file handoff.")
	}
	switch {
	case has("upload_file") && has("vault_put_file"):
		b.WriteString(" File attachments can use the directly visible upload_file (IPFS) and vault_put_file (vault) tools over the banner-visible source modes;")
	case has("upload_file"):
		b.WriteString(" File attachments can use the directly visible upload_file (IPFS) tool over the banner-visible source modes;")
	case has("vault_put_file"):
		b.WriteString(" File attachments can use the directly visible vault_put_file (vault) tool over the banner-visible source modes;")
	}
	if has("upload_file") || has("vault_put_file") {
		b.WriteString(" Pinner fetches the temporary file URL locally and uses its existing authenticated TUS path. Large uploads use TUS internally; the SDK result includes an upload location for resume/status management. TUS is never anonymous.")
	}
	if has("vault_get_file") {
		b.WriteString(" Vault cat returns bounded base64 JSON in agent mode and never writes raw bytes to the MCP transport.")
	}
	return b.String()
}

// instructionsExposureList derives the "including ..." tool-family enumeration
// at the head of every instruction variant from the completed per-server
// surface: a family word appears only when its family actually contributes
// registered tools, so a hosted/minimal assembly never claims vault or upload
// families it lacks. wizardTail preserves each variant's historical trailing
// phrase (the base variant says "website/domain wizard tools", the flat
// variants say "website/domain publishing"). available==nil renders the full
// historical enumeration.
func instructionsExposureList(available func(string) bool, s DomainScope, wizardTail string) string {
	if available == nil {
		return "upload, pin, list, status, download, vault, website, " + wizardTail
	}
	has := available
	var parts []string
	if s.UploadOn() && (has("upload_file") || has("upload_url") || has("upload_data")) {
		parts = append(parts, "upload")
	}
	if s.PinsOn() && (has("pins_add") || has("pins_list")) {
		parts = append(parts, "pin")
	}
	// The `list` family (the legacy nil-catalog literal names it): the async
	// upload management list (upload_list) and the pins list (pins_list) are
	// real finalized-catalog members, so a catalog-derived list that omits the
	// family silently diverges from the legacy path's guarantee.
	if has("upload_list") || has("pins_list") {
		parts = append(parts, "list")
	}
	if (s.PinsOn() && has("pins_status")) || s.OperationsOn() {
		parts = append(parts, "status")
	}
	if has("download_file") || has("vault_get_file") {
		parts = append(parts, "download")
	}
	if s.VaultOn() && has("vault_status") {
		parts = append(parts, "vault")
	}
	if s.WebsitesOn() {
		parts = append(parts, "website")
	}
	if has("websites_wizard_start") {
		parts = append(parts, wizardTail)
	} else if s.WebsitesOn() {
		parts = append(parts, "website/domain publishing")
	}
	if len(parts) == 0 {
		return "the direct Pinner tools"
	}
	return strings.Join(parts, ", ")
}

// instructionsPublishLine derives the "- publish:" flow line from membership:
// the upload_file -> websites_create chain only when BOTH tools registered;
// a websites_create-only line when upload is absent; nothing when the
// surface carries no website publishing at all. available==nil keeps the full
// historical line.
func instructionsPublishLine(available func(string) bool) string {
	see := " (see agent_guide for domain/label/custom-domain branching)"
	switch {
	case available == nil || (available("upload_file") && available("websites_create")):
		return "- publish:  upload_file -> websites_create" + see
	case available("websites_create"):
		return "- publish:  websites_create" + see
	}
	return ""
}

// instructionsFilterLine derives the "- filter:" search example of the
// progressive instructions' "Common flows" block from the completed surface
// and membership. The example suggests search_tools({ "category": "vault", ...})
// to disclose vault tools progressively, so it is meaningful ONLY when the
// vault family is actually exposed on this server — a restricted (hosted)
// assembly has no vault domain at all, and its search index resolves no "vault"
// category, so steering an agent to it would advertise a surface-disabled
// family. It gates on the SAME surface+membership predicate the exposure list
// uses (s.VaultOn() && a registered vault_status) so the two derived projections
// cannot disagree. available==nil (the pure prose builder) keeps the classic
// vault example (full-surface historical copy).
//
// The returned value owns its trailing newlines so a hidden line leaves exactly
// the single blank separator the template renders between the block and the
// following paragraph.
func instructionsFilterLine(available func(string) bool, s DomainScope) string {
	clause := "- filter:   search_tools({ \"category\": \"vault\", \"query\": \"<one keyword>\" })"
	if available != nil && !(s.VaultOn() && available("vault_status")) {
		return "\n"
	}
	return clause + "\n\n"
}

// mcpInstructionsBase is the PROGRESSIVE instructions template (the default):
// a direct surface plus the progressive-disclosure meta-tools, with
// every other catalog tool reachable via search_tools -> describe_tool ->
// invoke_*. Its variable slots are ALL derived by buildInstructionsFromCatalog
// from the completed per-server membership: the exposure list, the OOB clause,
// the wizard sentence, the auth paragraph, the primary flow lines, the vault
// filter line, and the publish line — so a hosted/minimal assembly's
// instructions never claim a family, flow, or tool it does not register.
// buildInstructionsFor (the legacy pure-prose builder used by unit tests)
// passes availability nil, which renders the full historical copy of every
// slot.
const mcpInstructionsBase = `This server exposes a curated set of common Pinner tools directly, including %s%s%s.

The tool surface is intentionally two-tier. The tools listed directly in tools/list are the curated, most-used surface. The rest of the catalog (see count below) is served through progressive disclosure and is NOT broken or missing: any tool not listed directly is reachable via search_tools -> describe_tool -> invoke_read_tool/invoke_write_tool/invoke_destructive_tool (the describe_tool response names the typed dispatcher for each tool). If a tool you expect is absent from tools/list, search for it rather than assuming it is unavailable. A large catalog is deliberately kept off the direct list to keep the initial tool surface small and the context budget predictable.

%s

Common flows start here:
- guide:    call agent_guide first for the full ordered flow chains and decision trees
%s%s
- search:   search_tools({ "query": "<one keyword>" })
%sSome internal commands are human-only or read piped stdin; when an agent invokes one via the invoke dispatchers, the server returns a structured needs_human redirect instead of blocking. Commands that prompt interactively are hidden from search_tools entirely.

Less common CLI tools remain available through progressive disclosure:
1. search_tools({ "query": "..." }): Find tools by keyword. Returns matching names, descriptions, and categories.
2. describe_tool({ "name": "..." }): Get the full input schema for one internal tool; the response carries invokeTool, the dispatcher that executes it.
3. invoke_read_tool / invoke_write_tool / invoke_destructive_tool({ "name": "...", "arguments": { ... } }): Execute one internal tool with the dispatcher named by invokeTool.`

// mcpInstructionsFlat is sent to MCP clients for a FLAT server that keeps the
// discovery meta-tools (the safe default, IncludeMetaOnFlat=true). Every
// agent-safe catalog tool is exposed directly on tools/list; gated admin/
// wizard-interactive ops are deliberately NOT direct but stay reachable through
// the discovery meta-tools, so the guidance must not claim the gated ops are
// absent or that a tool must be searched for and then invoked — the direct set
// is already the whole safe surface.
const mcpInstructionsFlat = `This server exposes the whole agent-safe tool catalog directly on tools/list, including %s%s. There is no two-tier curated surface: every tool that is safe for an agent to invoke is listed directly, so you do not need to discover then invoke — the tool is already on tools/list and callable by name.

Privileged and interactive operations (admin, setup wizards, and human-only flows) are intentionally NOT directly exposed, because they require human authorization or an interactive prompt. They remain reachable through the discovery meta-tools, which index the whole catalog:
1. search_tools({ "query": "..." }): Find tools by keyword. Returns matching names, descriptions, and categories.
2. describe_tool({ "name": "..." }): Get the full input schema for one internal tool; the response carries invokeTool, the dispatcher that executes it.
3. invoke_read_tool / invoke_write_tool / invoke_destructive_tool({ "name": "...", "arguments": { ... } }): Execute one gated internal tool with the dispatcher named by invokeTool. Admin invocations are refused and human-only tools return a needs_human hand-off.

%s

Common flows start here:
- guide:    call agent_guide first for the full ordered flow chains and decision trees
%s%s`

// mcpInstructionsFlatNoMeta is sent to MCP clients for a FLAT server built with
// the explicit IncludeMetaOnFlat=false override, which intentionally hides the
// gated admin/wizard/interactive operations from the MCP channel: they are
// neither directly listed nor discoverable via the meta-tools (the meta-tools
// are omitted). The guidance must be honest that only the agent-safe direct
// surface exists and that gated ops cannot be reached here at all.
const mcpInstructionsFlatNoMeta = `This server exposes the whole agent-safe tool catalog directly on tools/list, including %s%s. Every tool safe for an agent to invoke is already listed directly and callable by name — there is no discovery step and no progressive-disclosure meta-tool set on this server.

Privileged and interactive operations (admin, setup wizards, and human-only flows) are intentionally NOT exposed on this server: they are neither listed directly nor discoverable through meta-tools, because running them requires human authorization or an interactive prompt that this endpoint does not carry. If you need one of those operations, tell the human to run it via the pinner CLI.

%s

Common flows start here:
- guide:    call agent_guide first for the full ordered flow chains and decision trees
%s%s`

// instructionsWizardSentence builds the base instructions' wizard-exclusion
// sentence from completed-catalog membership: it is emitted only when the
// setup-wizard provision actually ran on this assembly (the sentence names the
// auth_sso/vault flows the wizard tools duplicate, which a wizard-free host —
// notably a hosted assembly — must not reference). available==nil (the pure
// prose builder) keeps the sentence.
func instructionsWizardSentence(available func(string) bool) string {
	if available == nil || available("setup_wizard_start") {
		return ". Setup wizard tools are kept out of the curated direct list because they duplicate the auth_sso/vault_create/vault_restore flows for CLI-style onboarding; they never accept passwords or OTP over this channel and remain reachable via search_tools"
	}
	return ""
}

// instructionsAuthParagraph builds the "For authentication" paragraph from
// completed-catalog membership: the out-of-band hand-off guidance only when
// the OOB pair is actually registered; when it is not (the hosted assembly —
// Portal OAuth establishes identity before the request), the server must not
// recommend tools that cannot resolve, so the paragraph reports the truth
// instead. available==nil (the pure prose builder) keeps the OOB paragraph.
func instructionsAuthParagraph(available func(string) bool) string {
	if available == nil || (available("auth_sso") && available("auth_resume")) {
		return "For authentication, prefer the out-of-band flow: call auth_sso, give the returned approval URL to the human, then poll auth_resume with the returned handle until it reports done."
	}
	if available != nil && !available("auth_status") {
		return "For authentication, identity is established by this deployment for the session (a hosted deployment resolves it per request) — there is no out-of-band sign-in flow on this server."
	}
	return "For authentication, call auth_status to report the current state; identity itself is established by this deployment for the session (a hosted deployment resolves it per request) — there is no out-of-band sign-in flow on this server."
}

// instructionsOOBClause builds the first-sentence clause naming the
// agent-facing out-of-band sign-in tools, derived from completed-catalog
// membership: the clause is emitted only when BOTH auth tools are actually
// registered on this server (a hosted assembly has neither — Portal OAuth
// replaces the CLI OOB flow — and must never see them promised). It is ALSO
// strategy-aware, because the flat variants contradict the progressive copy's
// characterization of the pair:
//
//   - ListingProgressive: auth_resume is reachable only through the search
//     meta-tools, so the copy calls it progressively discoverable; auth_sso
//     carries DirectVisible and IS directly listed, so the clause states that
//     split rather than claiming the pair is unlisted.
//   - ListingFlat (with meta): auth_resume's "not directly listed /
//     progressively discoverable" claim is false — flat materialization stamps
//     every agent-safe op direct — so the copy says the pair is directly
//     listed on this flat surface.
//   - ListingFlat WITHOUT meta: the explicit no-discovery override removes the
//     meta-tools, so there is no progressive-discovery path on this server and
//     the copy must not promise one. The clause names only the directly listed
//     auth_sso and omits auth_resume rather than characterizing it as
//     progressively discoverable.
//
// available==nil (the pure prose builder used by buildInstructionsFor's unit
// tests) keeps the clause for all three variants.
func instructionsOOBClause(available func(string) bool, strategy ToolListingStrategy, includeMetaOnFlat bool) string {
	if available != nil && (!available("auth_sso") || !available("auth_resume")) {
		return ""
	}
	switch {
	case strategy == ListingFlat && !includeMetaOnFlat:
		// Flat + no meta: no discovery meta-tools exist here, so drop the
		// unreachable-by-discovery auth_resume characterization and keep the
		// truthful, directly listed auth_sso.
		return ", and the directly listed out-of-band sign-in tool auth_sso"
	case strategy == ListingFlat:
		// Flat + meta: the flat safe-surface contract already includes the pair
		// directly; never claim it is unlisted or progressively discovered.
		return ", and the agent-facing out-of-band sign-in tools (auth_sso and auth_resume, directly listed on this flat surface)"
	default:
		// auth_sso is DirectVisible (directly listed on tools/list); only
		// auth_resume is search/progressive-only. Never claim the direct
		// listing does not exist for the pair.
		return ", and the agent-facing out-of-band sign-in tools (auth_sso directly listed; auth_resume progressively discoverable via search)"
	}
}

// instructionsFlowLines derives the "Common flows" onboarding primary tool
// chains from the SAME declarative flow-spec table the guide renders from
// (guideFlowSpecs, onboarding entries), filtered by the flow's surface gate
// (restricted assemblies cannot advertise a flow absent from their surface)
// and, when available is non-nil, by completed-catalog membership (a step
// whose tool never registered is dropped, not recommended — including
// vault-resume tails on a hosted assembly). The pins flow keeps its
// slash-list style and names pins_rm exactly as the flow spec does; the auth
// chain renders auth_status's repeated final step exactly as the flow spec
// does. available==nil (the pure prose builder) skips the membership filter.
func instructionsFlowLines(available func(string) bool, s DomainScope) string {
	var b strings.Builder
	for _, f := range guideFlowSpecs {
		if !f.onboarding || !f.gate(s) {
			continue
		}
		steps := make([]string, 0, len(f.steps))
		for _, step := range f.steps {
			if stepsContains(steps, step) {
				continue
			}
			if available != nil && !available(step) {
				continue
			}
			steps = append(steps, step)
		}
		if len(steps) == 0 {
			continue
		}
		sep := " -> "
		if f.name == "pins" {
			sep = " / "
		}
		fmt.Fprintf(&b, "- %-9s %s\n", f.name+":", strings.Join(steps, sep))
	}
	return b.String()
}

// stepsContains reports whether the step list already carries step (the auth
// flow repeats auth_status deliberately; the derived line renders it once).
func stepsContains(steps []string, step string) bool {
	for _, s := range steps {
		if s == step {
			return true
		}
	}
	return false
}

// buildInstructionsFor is the pure strategy/meta -> instructions selection:
// under ListingFlat it selects the flat instructions (with or without the
// discovery meta-tools per includeMetaOnFlat) so the guidance never promises a
// progressive workflow the flat server does not serve. It takes the policy
// inputs explicitly so tests can drive all three variants without touching any
// shared state. Without completed-catalog facts it renders the full unfiltered
// primary-flow chains and the OOB clause; production construction ALWAYS goes
// through ToolCatalog.Instructions, which filters both by the completed
// per-server membership.
func buildInstructionsFor(strategy ToolListingStrategy, includeMetaOnFlat bool, toolCount int) string {
	return buildInstructionsFromCatalog(strategy, includeMetaOnFlat, toolCount, nil)
}

// buildInstructionsFromCatalog is buildInstructionsFor's completed-facts
// variant: when catalog is non-nil, EVERY tool-naming slot derives from the
// completed per-server membership and the catalog's own captured surface — the
// exposure list, the OOB out-of-band clause, the wizard sentence, the auth
// paragraph, the primary flow chains, the publish line, and the closing
// tail (a hosted assembly never carries the OOB clause and never advertises a
// primary flow step — or whole flow, vault/upload family, or transfer tool —
// its surface omits, and its declared tool count equals its final indexed
// catalog count because the consolidated open_app launcher is indexed during
// the collection phase). nil keeps the pure unfiltered prose (tests/legacy).
func buildInstructionsFromCatalog(strategy ToolListingStrategy, includeMetaOnFlat bool, toolCount int, catalog *ToolCatalog) string {
	authParagraph := instructionsAuthParagraph(nil)
	// The OOB clause is strategy-aware even without completed-catalog facts, so
	// the pure flat variations never characterize auth_resume as progressively
	// discoverable (the no-discovery claim is made by the flat templates).
	oob := instructionsOOBClause(nil, strategy, includeMetaOnFlat)
	wizards := instructionsWizardSentence(nil)
	flows := instructionsFlowLines(nil, DomainScope{})
	exposure := instructionsExposureList(nil, DomainScope{}, "")
	publish := instructionsPublishLine(nil)
	filter := instructionsFilterLine(nil, DomainScope{})
	tail := catalogCountSentence(toolCount)
	if catalog != nil {
		// The ONE completed-surface membership predicate (agent_guide.go): a
		// tool is available when it is a completed catalog member OR was
		// directly registered outside indexing (DirectCustom — e.g. the
		// no-app-mode upload_file / vault_put_file direct-only registrations).
		// A plain catalog.Get closure would drop those direct-only tools from
		// the exposure list, the OOB clause, the flows, and the closing tail,
		// contradicting what the same surface lists on the wire.
		available := catalogToolAvailable(catalog)
		authParagraph = instructionsAuthParagraph(available)
		oob = instructionsOOBClause(available, strategy, includeMetaOnFlat)
		wizards = instructionsWizardSentence(available)
		flows = instructionsFlowLines(available, catalog.DomainScope)
		// Flat variants share the same exposure tail ("website/domain
		// publishing") — the wizard-exclusion phrasing differs only under the
		// progressive default, where the wizard tools stay behind the meta-tools.
		switch {
		case strategy == ListingFlat:
			exposure = instructionsExposureList(available, catalog.DomainScope, "website/domain publishing")
		default:
			exposure = instructionsExposureList(available, catalog.DomainScope, "website/domain wizard tools")
		}
		publish = instructionsPublishLine(available)
		filter = instructionsFilterLine(available, catalog.DomainScope)
		tail = mcpInstructionsTailText(toolCount, available)
	}
	switch {
	case strategy == ListingFlat && !includeMetaOnFlat:
		return fmt.Sprintf(mcpInstructionsFlatNoMeta, exposure, oob, authParagraph, flows, publish) + tail
	case strategy == ListingFlat:
		return fmt.Sprintf(mcpInstructionsFlat, exposure, oob, authParagraph, flows, publish) + tail
	default:
		// The wizard-exclusion sentence is only emitted when wizard tools are
		// actually provisioned on this assembly: a hosted (or wizard-free)
		// server must not name auth_sso/vault flows its surface omits. The
		// vault filter example is likewise surface-gated (instructionsFilterLine),
		// so a hosted assembly never advertises a vault search category.
		return fmt.Sprintf(mcpInstructionsBase, exposure, oob, wizards, authParagraph, flows, publish, filter) + tail
	}
}

// Instructions returns the MCP server instructions for THIS catalog with its
// own tool count and captured listing policy substituted: the strategy- and
// meta-aware variant selection plus the real catalog tool count, so the
// guidance given to agents stays accurate as commands are added or removed.
// The policy is read exclusively from the catalog's captured fields — never
// the deprecated construction-time globals — so a server's instructions are
// fixed by its own construction (an interleaved second build cannot re-branch
// them). The out-of-band clause and the primary flow chains derive from the
// COMPLETED per-server membership (built with the finalize helper), never from
// a hard-coded inventory: a hosted assembly cannot claim auth_sso/auth_resume
// or vault flows it never registered.
func (c *ToolCatalog) Instructions() string {
	return buildInstructionsFromCatalog(c.listingStrategy(), c.metaOnFlat(), c.Len(), c)
}
