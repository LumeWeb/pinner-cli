package mcp

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/opmesh"
	pinnermcp "go.lumeweb.com/pinner/mcp"
)

// registerDevTools provisions the dev_* introspection tools onto the CLI
// catalog for a --dev-tools launch. The module owns the dev-tools surface —
// its descriptors, handlers, and copy are defined there behind Config.DevTools
// — so this shim sources the descriptors from a module assembly rather than
// re-implementing them locally. The raw wire snapshot the tools introspect is
// CLI runtime behavior and stays behind SetDevTools.
func registerDevTools(catalog *ToolCatalog) error {
	if catalog == nil {
		return nil
	}
	// The standalone assembly carries an empty operation catalog: only the
	// direct-only presentation is built from Config, which is exactly the part
	// this shim consumes. The dev surface is derived DIFFERENTIALLY — the
	// presentation assembled with DevTools less the one assembled without it —
	// so the shim registers exactly what the module appends for the
	// DevTools switch, with no locally hardcoded tool names to keep in sync.
	base, err := pinnermcp.Assemble(pinnermcp.Config{Catalog: opmesh.NewCatalog()})
	if err != nil {
		return err
	}
	dev, err := pinnermcp.Assemble(pinnermcp.Config{
		Catalog:  opmesh.NewCatalog(),
		DevTools: true,
	})
	if err != nil {
		return err
	}
	undev := make(map[string]bool, len(base.Direct))
	for _, d := range base.Direct {
		undev[d.Name] = true
	}
	for _, d := range dev.Direct {
		if !undev[d.Name] {
			catalog.Add(model.ToolEntryFromDescriptor(d))
		}
	}
	return nil
}
