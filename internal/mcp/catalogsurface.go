package mcp

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// This file is the bridge between the operation catalog (the compiler-backed
// source of truth for MCP tool descriptions/schemas) and the legacy ToolCatalog
// that drives the official MCP server's progressive-disclosure meta-tools
// (search_tools, describe_tool, invoke_tool).
//
// The compiled catalog yields ToolDescriptors whose Description/InputSchema
// come from the catalogops AgentDescription and typed arg metadata, so CLI
// help prose, global flag bags, and empty "required" arrays never leak into
// the model surface. Each compiled operation is surfaced as a ToolEntry whose
// Handler routes through catalog.Catalog.Invoke the dispatch gate so
// Interaction, Visibility, Safety, and required-arg enforcement hold.

// compiledHandler wraps the operation catalog's Invoke gate for a single
// compiled operation and returns its result as a ToolResult. It is the Handler
// installed on the ToolEntry for every operation compiled from the catalog.
func compiledHandler(cat catalog.Catalog, name string) PinnerToolHandler {
	return func(ctx context.Context, req ToolRequest) (ToolResult, error) {
		return DispatchCatalogOp(ctx, cat, catalog.ActorModel, name, req.Arguments, name)
	}
}

// catalogDescriptorToEntry converts a compiler-produced catalog.ToolDescriptor
// into a ToolEntry backed by the operation catalog's Invoke gate. It maps the
// catalog Safety classification onto the MCP entry's ReadOnly/Destructive
// semantics so tool metadata is truthful for the model surface:
//
//	SafetyRead        -> ReadOnly=true
//	SafetyDestructive -> Destructive=true
//	SafetyMutate      -> neither
//
// DirectVisible is left to markCurated (the curated product surface), matching
// how every other tool is promoted to tools/list.
func catalogDescriptorToEntry(d catalog.ToolDescriptor, cat catalog.Catalog) *ToolEntry {
	entry := toolEntryFromDescriptor(ToolDescriptor{
		Name:        d.Name,
		Title:       d.Title,
		Description: d.Description,
		Category:    ToolCategory(d.Category),
		InputSchema: d.InputSchema,
		ReadOnly:    d.Safety == catalog.SafetyRead,
		Destructive: d.Safety == catalog.SafetyDestructive,
		Handler:     compiledHandler(cat, d.Name),
	})
	return entry
}

// populateCatalogSurface compiles every model-visible operation from cat and
// registers it in tc as a ToolEntry whose Handler dispatches through the
// catalog's Invoke gate. It returns the set of compiled operation names so the
// legacy argv tool-handler can route those invocations to the catalog instead
// of the CLI command tree. Names that already exist in tc are replaced, so a
// hybrid deployment (compiled ops for covered domains, legacy tools for the
// rest) stays coherent. Tools are discoverable via search_tools/describe_tool;
// tools/list prominence is decided by markCurated.
func populateCatalogSurface(tc *ToolCatalog, cat catalog.Catalog) (map[string]bool, error) {
	if tc == nil {
		return nil, fmt.Errorf("populateCatalogSurface: nil tool catalog")
	}
	if cat == nil {
		return nil, fmt.Errorf("populateCatalogSurface: nil operation catalog")
	}
	descs, err := catalog.NewMCPCompiler().Compile(cat)
	if err != nil {
		return nil, fmt.Errorf("populateCatalogSurface: compile operation catalog: %w", err)
	}
	compiled := make(map[string]bool, len(descs))
	for _, d := range descs {
		if d.Name == "" {
			continue
		}
		tc.Add(catalogDescriptorToEntry(d, cat))
		compiled[d.Name] = true
	}
	return compiled, nil
}
