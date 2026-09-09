// Package toolforge shim: the schema builder (go.lumeweb.com/mcpforge)
// re-exposed over Pinner's hostenv feature vocabulary. The wire-compatible
// PropOpt constructors below keep the old call-site shapes
// (toolforge.When(hostenv.FeatX)) compiling unchanged; the feature value is
// re-wrapped onto mcpforge's type at the boundary (see carrier.go).
package toolforge

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
	mcpforge "go.lumeweb.com/mcpforge"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// SchemaBuilder composes a tool's input JSON schema from feature-gated
// property contributions and compiles it against a hostenv.FeatureSet
// (mcpforge.SchemaBuilder re-exposed; Build takes the feature set directly
// — a published schema depends only on features, no platform predicates).
type SchemaBuilder struct {
	b *mcpforge.SchemaBuilder
}

// PropOpt configures a single contributed property.
type PropOpt = mcpforge.PropOpt

// When includes the property only when the resolved feature set has f.
func When(f hostenv.Feature) PropOpt {
	return mcpforge.When(forgeFeature(f))
}

// Unless excludes the property when the resolved feature set has f.
func Unless(f hostenv.Feature) PropOpt {
	return mcpforge.Unless(forgeFeature(f))
}

// Transform rewrites the materialized property schema using the resolved
// feature set (e.g. to narrow a nested enum). It runs after feature gating.
// The CLI signature takes a hostenv.FeatureSet; the mcpforge callback
// re-wraps the set back into the CLI vocabulary.
func Transform(fn func(*jsonschema.Schema, hostenv.FeatureSet)) PropOpt {
	return mcpforge.Transform(func(s *jsonschema.Schema, fs mcpforge.FeatureSet) {
		fn(s, fromForgeFeatureSet(fs))
	})
}

// Description sets the property's schema description.
func Description(d string) PropOpt {
	return mcpforge.Description(d)
}

// Enum sets a fixed enum on the property. For enums that vary by profile,
// prefer Transform.
func Enum(values ...any) PropOpt {
	return mcpforge.Enum(values...)
}

// Schema starts a new tool input schema builder.
func Schema() *SchemaBuilder {
	return &SchemaBuilder{b: mcpforge.Schema()}
}

// Description sets the top-level schema description.
func (b *SchemaBuilder) Description(d string) *SchemaBuilder {
	b.b.Description(d)
	return b
}

// Required marks top-level properties as required.
func (b *SchemaBuilder) Required(names ...string) *SchemaBuilder {
	b.b.Required(names...)
	return b
}

// Property adds an arbitrary property from a pre-built *jsonschema.Schema
// (typically a leaf you constructed inline or a reflected stable object).
// Use StringProperty/BoolProperty for simple leaves.
func (b *SchemaBuilder) Property(name string, schema *jsonschema.Schema, opts ...PropOpt) *SchemaBuilder {
	b.b.Property(name, schema, opts...)
	return b
}

// StringProperty adds a string leaf property described in prose.
func (b *SchemaBuilder) StringProperty(name, desc string, opts ...PropOpt) *SchemaBuilder {
	b.b.StringProperty(name, desc, opts...)
	return b
}

// BoolProperty adds a boolean leaf property described in prose.
func (b *SchemaBuilder) BoolProperty(name, desc string, opts ...PropOpt) *SchemaBuilder {
	b.b.BoolProperty(name, desc, opts...)
	return b
}

// Build materializes the schema for the given feature set.
func (b *SchemaBuilder) Build(fs hostenv.FeatureSet) *jsonschema.Schema {
	return b.b.Build(toForgeFeatureSet(fs))
}

// RawJSON renders the compiled schema as a JSON document for a descriptor's
// InputSchema. A schema that cannot marshal degrades to an empty object schema
// so a bad contribution can never crash tool registration.
func (b *SchemaBuilder) RawJSON(fs hostenv.FeatureSet) json.RawMessage {
	return b.b.RawJSON(toForgeFeatureSet(fs))
}
