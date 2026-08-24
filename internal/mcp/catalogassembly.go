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

// AssembleCatalogOps builds a single operation catalog covering the whole
// catalogops surface: auth, vault-setup, vault, pins, websites, dns, ipns,
// api-keys, and account operations. Each domain's operations are derived from
// the corresponding CatalogDepsBundle field via the catalogops provider
// functions. A nil deps field is fine: catalogops degrades such a domain to
// operations that fail with a clear "service unavailable" error at execution
// time, so registration never fails purely because a dependency is missing.
//
// A nil bundle is a wiring bug and is rejected here.
//
// Note on the return type: catalog.NewCatalog returns the catalog.Catalog
// interface (its concrete backing type, catalogImpl, is unexported), so there
// is no exported *catalog.Catalog struct to return. The catalog is therefore
// returned as the catalog.Catalog interface, which exposes Add for
// registration and Search/Get/Describe/Invoke for consumption.
func AssembleCatalogOps(deps *CatalogDepsBundle) (catalog.Catalog, error) {
	if deps == nil {
		return nil, fmt.Errorf("catalog assembly: nil catalog deps bundle")
	}

	cat := catalog.NewCatalog()

	domains := []struct {
		name string
		ops  []catalog.Operation
	}{
		{"auth", catalogops.AuthOperations(deps.Auth)},
		{"account", catalogops.AccountOperations(deps.Account)},
		{"vault-setup", catalogops.VaultSetupOperations(deps.VaultSetup)},
		{"vault", catalogops.VaultOperations(deps.Vault)},
		{"pins", catalogops.PinsOperations(deps.Pins)},
		{"websites", catalogops.WebsitesOperations(deps.Websites)},
		{"dns", catalogops.DNSOperations(deps.DNS)},
		{"ipns", catalogops.IPNSOperations(deps.IPNS)},
		{"api-keys", catalogops.APIKeysOperations(deps.APIKeys)},
		{"operations", catalogops.OperationsOperations(deps.Operations)},
		{"admin", catalogops.AdminOperations(deps.Admin)},
	}

	for _, d := range domains {
		if err := assembleCatalogOps(cat, d.ops); err != nil {
			return nil, fmt.Errorf("catalog assembly: register %s domain: %w", d.name, err)
		}
	}

	return cat, nil
}
