package mcp

import (
	"fmt"

	opmesh "go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner/assembly"
	"go.lumeweb.com/pinner/catalogops"
)

// AssembleCatalogOps is a thin adapter delegating to the module's
// assembly.AssembleCatalogOps. The dependency bundle, surface gating, and
// environment (hosted vs CLI/local) filtering all live there now — the local
// implementation was a byte-for-byte copy of the module seam (catalogops
// provider list, per-domain surface flags, EnvironmentOf via
// catalogmeta.EnvironmentOf) and is consumed directly at the re-integration
// seam so the CLI and the module cannot drift.
//
// The local CatalogDepsBundle is the CLI-side wiring shape (its field types
// come from the local operation definitions); it is converted
// field-by-field into the module assembly.CatalogDepsBundle here. The local
// and module bundle fields are field-identical (same names, same underlying
// function/service types), so the conversion loses nothing.
//
// surface controls which domains are registered (zero value = full surface).
// hosted excludes EnvLocalOnly/EnvCLIOnly operations (auth_login/logout mutates
// shared local config a stateless hosted server does not have); the CLI/local
// path registers everything except EnvHostedOnly ops. These semantics are the
// module's, unchanged.
//
// A nil bundle is a wiring bug and is rejected here.
func AssembleCatalogOps(deps *CatalogDepsBundle, surface DomainScope, hosted bool) (opmesh.Catalog, error) {
	if deps == nil {
		return nil, fmt.Errorf("catalog assembly: nil catalog deps bundle")
	}

	var resolver assembly.CredentialResolver
	if deps.CredentialResolver != nil {
		resolver = deps.CredentialResolver
	}

	moduleDeps := &assembly.CatalogDepsBundle{
		CfgMgr:             deps.CfgMgr,
		CredentialResolver: resolver,
		Auth:               catalogops.AuthDeps(deps.Auth),
		Account:            catalogops.AccountDeps(deps.Account),
		Vault:              catalogops.VaultDeps(deps.Vault),
		VaultSetup:         catalogops.VaultDeps(deps.VaultSetup),
		Pins:               catalogops.PinsDeps(deps.Pins),
		Websites:           catalogops.WebsitesDeps(deps.Websites),
		DNS:                catalogops.DNSDeps(deps.DNS),
		IPNS:               catalogops.IPNSDeps(deps.IPNS),
		ENS:                catalogops.ENSDeps(deps.ENS),
		APIKeys:            catalogops.APIKeysDeps(deps.APIKeys),
		Operations:         catalogops.OperationsDeps(deps.Operations),
		Admin:              catalogops.AdminDeps(deps.Admin),
	}

	return assembly.AssembleCatalogOps(moduleDeps, assembly.DomainScope(surface), hosted)
}
