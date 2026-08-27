package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"

	"github.com/samber/lo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// This file is the bridge between the operation catalog (the compiler-backed
// source of truth for MCP tool descriptions/schemas) and the legacy ToolCatalog
// that drives the official MCP server's progressive-disclosure meta-tools
// (search_tools, describe_tool, invoke_tool).
//
// The compiled catalog yields ToolDescriptors whose Description/InputSchema
// come from the catalogops MCPTargets fallback and typed arg metadata, so CLI
// help prose, global flag bags, and empty "required" arrays never leak into
// the model surface. Each compiled operation is surfaced as a ToolEntry whose
// Handler routes through catalog.Catalog.Invoke the dispatch gate so
// Interaction, Visibility, Safety, and required-arg enforcement hold.

// compiledHandler wraps the operation catalog's Invoke gate for a single
// compiled operation and returns its result as a ToolResult. It is the Handler
// installed on the ToolEntry for every operation compiled from the catalog.
func compiledHandler(cat catalog.Catalog, name string) model.PinnerToolHandler {
	return func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
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
func catalogDescriptorToEntry(d catalog.ToolDescriptor, cat catalog.Catalog) *model.ToolEntry {
	entry := model.ToolEntryFromDescriptor(model.ToolDescriptor{
		Name:         d.Name,
		Title:        d.Title,
		Description:  d.Description,
		Category:     model.ToolCategory(d.Category),
		InputSchema:  d.InputSchema,
		OutputSchema: outputSchemaForCompiled(d.Safety, d.Interaction),
		ReadOnly:     d.Safety == catalog.SafetyRead,
		Destructive:  d.Safety == catalog.SafetyDestructive,
		MCPTargets:   toModelTargets(d.MCPTargets),
		Handler:      compiledHandler(cat, d.Name),
	})
	return entry
}

// toModelTargets maps catalog-native per-profile presentation Targets onto the
// model's ToolTarget variants. Each catalog Target's opaque Require feature
// names are cast to hostenv.Feature and packed into a FeatureSet, so the MCP
// surface can resolve the best-matching variant per request via the detected
// platform profile.
func toModelTargets(targets []catalog.Target) []model.ToolTarget {
	if len(targets) == 0 {
		return nil
	}
	return lo.Map(targets, func(t catalog.Target, _ int) model.ToolTarget {
		require := lo.SliceToMap(t.Require, func(name string) (hostenv.Feature, bool) {
			return hostenv.Feature(name), true
		})
		mt := model.ToolTarget{
			Require:     require,
			Visible:     t.Visible,
			Description: t.Description,
		}
		if t.DescFunc != nil {
			fn := t.DescFunc
			mt.DescFunc = func(p hostenv.PlatformProfile) string {
				return fn(p)
			}
		}
		return mt
	})
}

// outputSchemaForCompiled selects the output schema for a compiled operation
// from its Safety/Interaction classification, so the declared shape matches
// what the operation actually emits for a model actor (the MCP surface runs as
// ActorModel). DispatchCatalogOp maps the catalog gate's refusals onto the
// needs_human hand-off shape, so the effective StructuredContent range is:
//
//   - InteractionHumanOnly / InteractionNeedsHandoff: Catalog.Invoke always
//     refuses a model actor (ErrHumanRequired) before a handler runs, so these
//     tools return only the needs_human hand-off on the model path.
//
//   - SafetyDestructive: Catalog.Invoke refuses a model actor with
//     ErrConfirmRequired on first invocation (manual-confirm hand-off), then —
//     after human confirmation resumes — the op runs and returns the
//     {status:ok,value} success envelope. Both shapes are emitted, so a union
//     (anyOf) schema is declared.
//
//   - Otherwise (SafetyRead / SafetyMutate, agent-safe): the op runs directly
//     and returns only the {status:ok,value} success envelope.
func outputSchemaForCompiled(safety catalog.Safety, interaction catalog.Interaction) json.RawMessage {
	switch {
	case interaction == catalog.InteractionHumanOnly || interaction == catalog.InteractionNeedsHandoff:
		return catalogNeedsHumanOutputSchema
	case safety == catalog.SafetyDestructive:
		return catalogOutputUnionSchema
	default:
		return catalogOutputSchema
	}
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
