package mcpembed

import (
	"fmt"
	"os"
	"path/filepath"

	"go.lumeweb.com/pinner-cli/internal/cli"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp"
)

// NewCatalogDeps builds the production CatalogDepsBundle for a hosted
// (Portal-embedded) MCP server, pointed at the given Portal API endpoint.
//
// It creates a pinner-cli config.Manager configured with BaseEndpoint and
// Secure, then delegates to cli.BuildCatalogOpsDepsForHosted to assemble the
// full operation-catalog dependency graph (auth, account, pins, websites, DNS,
// IPNS, ENS, API keys, operations) using the same service factory wiring the
// CLI uses — just resolved against the hosted config instead of an on-disk
// config file.
//
// The per-request auth token is NOT stored in the config; it is threaded
// through the CredentialResolver seam (set on the bundle by mcpembed.New).
//
// The returned closure is suitable for use as Options.CatalogDeps. It returns
// the same pre-built bundle on each call (the bundle's closures re-read config
// and resolve services lazily per invocation, so live token reload is
// preserved).
func NewCatalogDeps(apiEndpoint string, secure bool) (func() *mcp.CatalogDepsBundle, error) {
	dir, err := os.MkdirTemp("", "mcpembed-*")
	if err != nil {
		return nil, fmt.Errorf("mcpembed: create temp config dir: %w", err)
	}
	// The config manager reads its values from in-memory state after Load, so the
	// backing temp dir is only needed during construction; drop it to avoid
	// leaking a directory-plus-file into the process temp dir on every call.
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("mcpembed: create config manager: %w", err)
	}
	if err := cfgMgr.SetBaseEndpoint(apiEndpoint); err != nil {
		return nil, fmt.Errorf("mcpembed: set base endpoint: %w", err)
	}
	if err := cfgMgr.SetSecure(secure); err != nil {
		return nil, fmt.Errorf("mcpembed: set secure: %w", err)
	}

	bundle := cli.BuildCatalogOpsDepsForHosted(cfgMgr)
	return func() *mcp.CatalogDepsBundle { return bundle }, nil
}
