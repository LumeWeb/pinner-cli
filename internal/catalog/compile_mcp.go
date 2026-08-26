package catalog

import (
	"fmt"
)

// NewMCPCompiler returns a Compiler[ToolDescriptor] that maps a Catalog to
// []ToolDescriptor for the *model* surface: it yields every model-visible
// operation (VisibilityModel and VisibilityBoth), resolves the description
// from MCPTargets fallback, and EXCLUDES app-only tools so they stay out of
// agent discovery/search_tools.
func NewMCPCompiler() Compiler[ToolDescriptor] { return newMCPCompiler(VisibilityModel) }

// NewMCPAppCompiler returns a Compiler[ToolDescriptor] that maps a Catalog to
// []ToolDescriptor for the *app* surface: it yields app-only and both-visible
// operations, and uses the plain human Description. App-only helpers that a
// model agent must not see are emitted here. Expose this surface to the
// hosting application, not to the model.
func NewMCPAppCompiler() Compiler[ToolDescriptor] { return newMCPCompiler(VisibilityAppOnly) }

// newMCPCompiler builds an MCP compiler targeting the given visibility surface.
//
// The target selects which Search visibility the descriptor set is drawn from,
// following Catalog.Search's visibility semantics (a model surface search
// excludes app-only ops; an app surface search includes them). Description
// is resolved from MCPTargets fallback via descriptorFor.
func newMCPCompiler(target Visibility) Compiler[ToolDescriptor] {
	return &mcpCompiler{target: target}
}

// mcpCompiler maps a Catalog's operations, restricted to a visibility surface,
// into []ToolDescriptor. Target records which surface it produces:
// VisibilityModel for the agent-facing tool set, VisibilityAppOnly for the
// app-facing set. Its zero value is VisibilityModel, matching the default.
// It satisfies Compiler[ToolDescriptor].
type mcpCompiler struct {
	target Visibility
}

// Compile converts the catalog's operations visible to the target surface into
// []ToolDescriptor.
func (m *mcpCompiler) Compile(cat Catalog) ([]ToolDescriptor, error) {
	if cat == nil {
		return nil, fmt.Errorf("catalog: cannot compile a nil catalog")
	}
	ops := cat.Search("", "", m.target)
	tools := make([]ToolDescriptor, 0, len(ops))
	for _, op := range ops {
		tools = append(tools, m.toolFor(op))
	}
	return tools, nil
}

// toolFor builds a single ToolDescriptor from an Operation, selecting the
// description audience by the target surface. It reuses the catalog's shared
// descriptorFor builder, mapping the surface to the equivalent discovery actor
// so Describe and the MCP compiler can never disagree on shape or audience.
func (m *mcpCompiler) toolFor(op Operation) ToolDescriptor {
	actor := ActorHuman
	if m.target == VisibilityModel {
		actor = ActorModel
	}
	return descriptorFor(op, actor)
}
