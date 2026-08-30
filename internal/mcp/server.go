package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
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

	// Surface declares which operation domains/tool families this server
	// exposes. The zero value is the full surface.
	Surface Surface

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

	// RegisterCustom runs the custom/direct tool registration (upload/vault
	// tools, apps, resources, prompts) after the catalog surface is built. It
	// is nil for a server with no custom tools.
	RegisterCustom func(srv *sdk.Server, catalog *ToolCatalog) error
}

// BuildServer assembles a fully-registered MCP server from ServerConfig: it
// builds the operation-catalog surface, projects the meta-tools, and runs any
// custom registration. It does not wire a transport — the caller serves srv
// over stdio or a streamable HTTP handler.
func BuildServer(cfg ServerConfig) (*sdk.Server, *ToolCatalog, error) {
	opts := []buildCatalogOpt{withSurface(cfg.Surface), withHosted(cfg.Hosted)}
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
	// Server instructions are always the CLI's (mcpInstructionsBase) with the
	// real assembled tool count substituted — never a custom/hosted override.
	srv, err := OfficialServerFromCatalog(catalog, buildInstructions(catalog.Len()), cfg.StdioMode, cfg.SeedDrop, cfg.OOBRestore, cfg.OOBCreate)
	if err != nil {
		return nil, nil, err
	}
	if cfg.RegisterCustom != nil {
		if err := cfg.RegisterCustom(srv, catalog); err != nil {
			return nil, nil, err
		}
	}
	return srv, catalog, nil
}
