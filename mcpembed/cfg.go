package mcpembed

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp"
)

// resolveCfgMgr returns the live config manager carried by the hosted catalog
// deps bundle, or nil when the bundle has none. It is the seam through which a
// hosted embed resolves the Portal API endpoint and per-request credential used
// to build its IPFS transfer executors.
func resolveCfgMgr(bundle *mcp.CatalogDepsBundle) (config.Manager, error) {
	if bundle == nil || bundle.CfgMgr == nil {
		return nil, nil
	}
	return bundle.CfgMgr(), nil
}
