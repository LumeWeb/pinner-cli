package mcp

import (
	"fmt"

	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
)

// ServerConfig holds everything needed to assemble an MCP server independent
// of CLI flags, transport, or tunnelling. It is the reusable server construction
// seam shared by the full CLI/local server and a hosted (Portal-embedded)
// server, so a hosted product is a DIFFERENT ASSEMBLY of the same MCP
// implementation rather than a second implementation or a fork.
type ServerConfig struct {
	// Hosted reports whether this is a hosted (Portal-embedded) assembly. It is
	// the single, explicit source of truth for hosted mode: set to true by
	// BuildHostedServer and never elsewhere, so a new boolean is threaded
	// through to catalog assembly instead of being inferred from structural
	// signals (surface equality or CredentialResolver presence) that are
	// orthogonal to deployment context.
	Hosted bool

	// DomainScope declares which operation domains/tool families this server
	// exposes. The zero value is the full surface.
	DomainScope DomainScope

	// Policy, when non-nil, is the server-construction LISTING policy: it
	// carries the listing strategy (progressive/flat), the meta-on-flat switch,
	// and the onboarding override. It deliberately carries NO deployment axes:
	// DomainScope/Hosted above remain the single deployment seam and are always
	// honored, so a partial listing policy selecting only a strategy can never
	// silently erase this config's DomainScope/Hosted (MEDIUM-2). When nil,
	// BuildServer uses a progressive strategy with the safe meta-on-flat
	// default.
	Policy *ListingPolicy

	// CatalogDeps, when set, supplies the operation-catalog dependency factory
	// (the compiler-backed surface is the only source of the tool catalog, so a
	// hosted server MUST supply it). The factory is resolved per invocation.
	CatalogDeps func() *CatalogDepsBundle

	// StdioMode reports whether the server runs over co-located stdio. It only
	// affects invoke-time gating, not transport selection (the caller wires the
	// transport).
	StdioMode bool

	// SeedDrop / OOBRestore / OOBCreate back the vault create/restore OOB
	// hand-offs. They are optional and left nil for a hosted server (vault is
	// typically surface-disabled).
	SeedDrop   *oob.SeedDrop
	OOBRestore *oob.OOBRestore
	OOBCreate  *oob.OOBCreate

	// HandoffReg / AuthHandles back the out-of-band sign-in and resume tools.
	HandoffReg  *handoff.HandoffRegistry
	AuthHandles *session.AsyncHandleStore

	// CollectExtensions assembles this server's extension plan BEFORE the
	// official server object exists. It declares every server extension
	// against the assembled catalog and completes the collection phase (role
	// validation, direct-phase provisions, and the final index of every
	// searchable extension), returning the plan whose single Materialize pass
	// projects direct tools, app views, resources, and prompts onto the server
	// after construction. Because it runs ahead of server construction, the
	// initialize instructions and the per-server ServerCard derive from the
	// COMPLETED per-server catalog — every indexed extension present — instead
	// of an order-dependent partial one.
	//
	// Exactly one of CollectExtensions and RegisterCustom may be set.
	CollectExtensions func(catalog *ToolCatalog) (*MaterializationPlan, error)

	// RegisterCustom runs the custom/direct tool registration (upload/vault
	// tools, apps, resources, prompts) after the catalog surface is built. It
	// is nil for a server with no custom tools.
	//
	// COMPATIBILITY SEAM (documented fallback): this single callback receives
	// the server, so it can only run AFTER construction — any catalog entries
	// it indexes are therefore NOT reflected in the construction-time
	// instructions count and the card must fall back to the legacy
	// DirectVisible/DirectCustom derivation. Production callers use
	// CollectExtensions; RegisterCustom remains for tests and callers that
	// must project custom tools directly.
	RegisterCustom func(srv *sdk.Server, catalog *ToolCatalog) error
}

// BuildServer assembles a fully-registered MCP server from ServerConfig: it
// builds the operation-catalog surface, projects the meta-tools, and runs any
// custom registration. It does not wire a transport — the caller serves srv
// over stdio or a streamable HTTP handler.
func BuildServer(cfg ServerConfig) (*sdk.Server, *ToolCatalog, error) {
	// Deployment axes (DomainScope/Hosted) ALWAYS come from this config — the single
	// deployment seam. The listing policy (when supplied) contributes only the
	// listing knobs (strategy / meta-on-flat / onboarding) and structurally
	// cannot overwrite DomainScope or Hosted, so a partial policy selecting only a
	// strategy preserves the deployment axes (MEDIUM-2).
	opts := []buildCatalogOpt{withDomainScope(cfg.DomainScope), withHosted(cfg.Hosted)}
	if cfg.Policy != nil {
		opts = append(opts, withPolicy(*cfg.Policy))
	}
	if cfg.CatalogDeps != nil {
		opts = append(opts, withCatalogDeps(cfg.CatalogDeps))
	}
	// buildCatalog takes a urfave command tree root, but the compiler-backed
	// surface does not walk it; nil is safe here (and mandatory for a hosted
	// assembly, which must not import the CLI package).
	catalog, err := buildCatalog(nil, cfg.SeedDrop, cfg.OOBRestore, cfg.OOBCreate, cfg.HandoffReg, cfg.AuthHandles, opts...)
	if err != nil {
		return nil, nil, err
	}
	if cfg.CollectExtensions != nil && cfg.RegisterCustom != nil {
		return nil, nil, fmt.Errorf("BuildServer: exactly one of CollectExtensions and RegisterCustom may be set")
	}
	// One-pass extension collection happens BEFORE the server object exists:
	// every server extension is declared and collected (validated, curated
	// provisions indexed, searchable extensions indexed), so the catalog is
	// complete when the instructions are derived.
	var plan *MaterializationPlan
	if cfg.CollectExtensions != nil {
		plan, err = cfg.CollectExtensions(catalog)
		if err != nil {
			return nil, nil, err
		}
	}
	// Server instructions are always the catalog's own strategy-aware variant
	// (mcpInstructionsBase under the default progressive) with the real
	// assembled tool count substituted — never a custom/hosted override, and
	// read from THIS catalog's captured policy rather than package globals.
	// Because extension collection ran above, the count is computed from the
	// COMPLETED catalog (final indexed extensions present), never from the
	// partial pre-extension one.
	srv, err := OfficialServerFromCatalog(catalog, catalog.Instructions(), cfg.StdioMode, cfg.SeedDrop, cfg.OOBRestore, cfg.OOBCreate)
	if err != nil {
		return nil, nil, err
	}
	// The single materialization pass projects direct tools, app views,
	// resources, and prompts onto the server from the completed plan.
	if plan != nil {
		if _, err := plan.Materialize(srv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.RegisterCustom != nil {
		// Legacy single-callback seam: see the field's compatibility note.
		if err := cfg.RegisterCustom(srv, catalog); err != nil {
			return nil, nil, err
		}
	}
	return srv, catalog, nil
}
