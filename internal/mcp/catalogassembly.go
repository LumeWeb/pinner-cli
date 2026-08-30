package mcp

import (
	"fmt"

	"github.com/samber/lo"

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
// hosted declares whether this is a hosted (Portal-embedded) assembly. It is
// passed explicitly from the server construction path (set only by
// BuildHostedServer) rather than inferred from surface equality or the
// presence of a CredentialResolver, which are orthogonal to deployment
// context. When hosted, operations whose Environment is EnvCLIOnly or
// EnvLocalOnly (e.g. auth_login/auth_logout, which mutate shared local config
// a stateless hosted server does not have) are excluded.
//
// A nil bundle is a wiring bug and is rejected here.
//
// Note on the return type: catalog.NewCatalog returns the catalog.Catalog
// interface (its concrete backing type, catalogImpl, is unexported), so there
// is no exported *catalog.Catalog struct to return. The catalog is therefore
// returned as the catalog.Catalog interface, which exposes Add for
// registration and Search/Get/Describe/Invoke for consumption.
func AssembleCatalogOps(deps *CatalogDepsBundle, surface Surface, hosted bool) (catalog.Catalog, error) {
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

	// An operation's Environment restricts which surfaces may register it.
	// Hosted mode is a Portal-embedded assembly whose catalog must never
	// advertise CLI-local complexity (EnvLocalOnly, EnvCLIOnly) — e.g.
	// auth_login/auth_logout, which mutate shared local config that a stateless
	// hosted server does not have. The hosted flag is declared explicitly by
	// the construction path (only BuildHostedServer sets it) rather than
	// inferred from surface equality or CredentialResolver presence, so a local
	// stdio server that disables Vault/Admin is never misclassified as hosted.
	// The CLI/local path registers everything except hosted-only ops.
	for _, d := range domains {
		if !d.enabled {
			continue
		}
		ops := filterOpsForEnvironment(d.ops, hosted)
		if err := assembleCatalogOps(cat, ops); err != nil {
			return nil, fmt.Errorf("catalog assembly: register %s domain: %w", d.name, err)
		}
	}

	return cat, nil
}

// filterOpsForEnvironment drops operations that are not valid on the active
// surface. In hosted mode the EnvLocalOnly and EnvCLIOnly operations are
// excluded (they mutate or depend on shared local config / are CLI frontend
// only). In CLI/local mode every operation is kept except EnvHostedOnly, which
// none are declared to be today.
func filterOpsForEnvironment(ops []catalog.Operation, hosted bool) []catalog.Operation {
	return lo.Filter(ops, func(op catalog.Operation, _ int) bool {
		env := op.Environment()
		if hosted && (env == catalog.EnvCLIOnly || env == catalog.EnvLocalOnly) {
			return false
		}
		if !hosted && env == catalog.EnvHostedOnly {
			return false
		}
		return true
	})
}
