package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// assembleCatalogOps registers every operation produced by one catalogops
// domain provider into cat. It stops at the first registration error so a
// genuinely malformed operation (duplicate name, invalid arg metadata) is
// surfaced rather than silently dropped, matching how the CLI wiring layer
// treats catalog construction.
func assembleCatalogOps(cat catalog.Catalog, ops []catalog.Operation) error {
	for _, op := range ops {
		if err := cat.Add(op); err != nil {
			return err
		}
	}
	return nil
}

// AssembleCatalogOps builds a single operation catalog covering the
// catalogops surface: auth, account, vault-setup, vault, pins, websites, dns,
// ipns, ens, api-keys, operations, and admin operations. Each domain's
// operations are derived from the corresponding CatalogDepsBundle field via
// the catalogops provider functions. A nil deps field is fine: catalogops
// degrades such a domain to operations that fail with a clear "service
// unavailable" error at execution time, so registration never fails purely
// because a dependency is missing.
//
// surface controls which domains are registered. A domain whose surface flag
// is disabled is simply not added to the catalog, so a restricted surface
// (e.g. hosted mode, which excludes the Sia vault and portal admin) never
// advertises — or can invoke — those operations. The zero surface is the full
// surface.
//
// A nil bundle is a wiring bug and is rejected here.
//
// Note on the return type: catalog.NewCatalog returns the catalog.Catalog
// interface (its concrete backing type, catalogImpl, is unexported), so there
// is no exported *catalog.Catalog struct to return. The catalog is therefore
// returned as the catalog.Catalog interface, which exposes Add for
// registration and Search/Get/Describe/Invoke for consumption.
func AssembleCatalogOps(deps *CatalogDepsBundle, surface Surface) (catalog.Catalog, error) {
	if deps == nil {
		return nil, fmt.Errorf("catalog assembly: nil catalog deps bundle")
	}

	cat := catalog.NewCatalog()

	// Map each catalogops domain to its surface flag. A disabled domain's
	// operations are never produced, so they are absent from search/describe/
	// invoke and their hand-off/setup handlers are never reachable.
	domains := []struct {
		name    string
		enabled bool
		ops     []catalog.Operation
	}{
		{"auth", surface.AccountOn(), catalogops.AuthOperations(deps.Auth)},
		{"account", surface.AccountOn(), catalogops.AccountOperations(deps.Account)},
		{"api-keys", surface.AccountOn(), catalogops.APIKeysOperations(deps.APIKeys)},
		{"vault-setup", surface.VaultOn(), catalogops.VaultSetupOperations(deps.VaultSetup)},
		{"vault", surface.VaultOn(), catalogops.VaultOperations(deps.Vault)},
		{"pins", surface.PinsOn(), catalogops.PinsOperations(deps.Pins)},
		{"websites", surface.WebsitesOn(), catalogops.WebsitesOperations(deps.Websites)},
		{"dns", surface.DNSOn(), catalogops.DNSOperations(deps.DNS)},
		{"ipns", surface.IPNSOn(), catalogops.IPNSOperations(deps.IPNS)},
		{"ens", surface.ENSOn(), catalogops.ENSOperations(deps.ENS)},
		{"operations", surface.OperationsOn(), catalogops.OperationsOperations(deps.Operations)},
		{"admin", surface.AdminOn(), catalogops.AdminOperations(deps.Admin)},
	}

	for _, d := range domains {
		if !d.enabled {
			continue
		}
		if err := assembleCatalogOps(cat, d.ops); err != nil {
			return nil, fmt.Errorf("catalog assembly: register %s domain: %w", d.name, err)
		}
	}

	return cat, nil
}
