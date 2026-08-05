package cli

import (
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// delegationDriver renders context-specific delegation UX for a namespace.
// Each driver owns the publishing semantics for its namespace (e.g. HNS
// wallet/resource vs ICANN registrar) so those assumptions do not leak into a
// shared renderer.
type delegationDriver interface {
	// Render prints the delegation bundle for a domain response. managed is
	// true when the website has DNS hosting enabled (the platform is
	// authoritative for the zone), so the driver does not instruct the user
	// to configure their own authoritative DNS server.
	Render(output Output, result *ipfs.DomainResponse, managed bool)
}

// delegationRegistry resolves the driver responsible for a namespace and holds
// a neutral fallback for namespaces without a dedicated driver.
type delegationRegistry struct {
	fallback delegationDriver
	drivers  map[ipfs.DomainNamespace]delegationDriver
}

// newDelegationRegistry builds a registry from the given namespace-to-driver
// mappings and a fallback used for any other namespace.
func newDelegationRegistry(fallback delegationDriver, drivers map[ipfs.DomainNamespace]delegationDriver) *delegationRegistry {
	return &delegationRegistry{
		fallback: fallback,
		drivers:  drivers,
	}
}

// Render routes a domain response to the driver registered for its namespace,
// falling back to the generic driver for unrecognized namespaces.
func (r *delegationRegistry) Render(output Output, result *ipfs.DomainResponse, managed bool) {
	if r == nil {
		return
	}
	driver, ok := r.drivers[ipfs.DomainNamespaceOf(result)]
	if !ok {
		driver = r.fallback
	}
	driver.Render(output, result, managed)
}

// defaultDelegationDriver is the registry wired to the built-in drivers.
var defaultDelegationDriver = func() *delegationRegistry {
	return newDelegationRegistry(
		&genericDelegationDriver{},
		map[ipfs.DomainNamespace]delegationDriver{
			ipfs.DomainNamespaceICANN: &icannDelegationDriver{},
			ipfs.DomainNamespaceHNS:   &hnsDelegationDriver{},
		},
	)
}()
