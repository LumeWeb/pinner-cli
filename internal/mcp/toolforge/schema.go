package toolforge

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// SchemaBuilder composes a tool's input JSON schema from feature-gated property
// contributions and compiles it against a hostenv.FeatureSet. It is the schema
// twin of DescBuilder: DescBuilder varies tool prose by feature, SchemaBuilder
// varies the published schema (property presence, enums) by the same features.
//
// It operates on the typed *jsonschema.Schema object model — the same model the
// reflection path produces — so an adaptive tool declares one input shape and
// the builder adds/omits/narrows properties per profile. Stable nested objects
// (e.g. the reflected UploadSource or ChatGPTFileInput) are passed in whole via
// Property and only their enum/leaf shapes are adapted by Transform, so a
// profile that cannot supply a nested object simply omits the property at Build
// time rather than maintaining a reduced copy of it.
type SchemaBuilder struct {
	desc     string
	required []string
	props    []*schemaProp
}

// schemaProp is one contributed property. Feature gates and shape transforms
// are applied at Build time against the resolved feature set.
type schemaProp struct {
	name      string
	schema    *jsonschema.Schema
	when      hostenv.Feature
	unless    hostenv.Feature
	transform func(*jsonschema.Schema, hostenv.FeatureSet)
}

// PropOpt configures a single contributed property.
type PropOpt func(*schemaProp)

// When includes the property only when the resolved feature set has f.
func When(f hostenv.Feature) PropOpt {
	return func(p *schemaProp) { p.when = f }
}

// Unless excludes the property when the resolved feature set has f.
func Unless(f hostenv.Feature) PropOpt {
	return func(p *schemaProp) { p.unless = f }
}

// Transform rewrites the materialized property schema using the resolved
// feature set (e.g. to narrow a nested enum). It runs after feature gating.
func Transform(fn func(*jsonschema.Schema, hostenv.FeatureSet)) PropOpt {
	return func(p *schemaProp) { p.transform = fn }
}

// Enum sets a fixed enum on the property (used for static leaf enums such as
// archive_mode). For enums that vary by profile, prefer Transform.
func Enum(values ...any) PropOpt {
	return func(p *schemaProp) { p.schema.Enum = values }
}

// Schema starts a new tool input schema builder.
func Schema() *SchemaBuilder {
	return &SchemaBuilder{}
}

// Description sets the top-level schema description.
func (b *SchemaBuilder) Description(d string) *SchemaBuilder {
	b.desc = d
	return b
}

// Required marks top-level properties as required.
func (b *SchemaBuilder) Required(names ...string) *SchemaBuilder {
	b.required = append(b.required, names...)
	return b
}

// Property adds an arbitrary property from a pre-built *jsonschema.Schema
// (typically a leaf you constructed inline or a reflected stable object such as
// UploadSource / ChatGPTFileInput). Use StringProperty/BoolProperty for simple
// leaves.
func (b *SchemaBuilder) Property(name string, schema *jsonschema.Schema, opts ...PropOpt) *SchemaBuilder {
	return b.add(&schemaProp{name: name, schema: schema}, opts)
}

// StringProperty adds a string leaf property described in prose.
func (b *SchemaBuilder) StringProperty(name, desc string, opts ...PropOpt) *SchemaBuilder {
	return b.add(&schemaProp{name: name, schema: &jsonschema.Schema{Type: "string", Description: desc}}, opts)
}

// BoolProperty adds a boolean leaf property described in prose.
func (b *SchemaBuilder) BoolProperty(name, desc string, opts ...PropOpt) *SchemaBuilder {
	return b.add(&schemaProp{name: name, schema: &jsonschema.Schema{Type: "boolean", Description: desc}}, opts)
}

func (b *SchemaBuilder) add(p *schemaProp, opts []PropOpt) *SchemaBuilder {
	for _, o := range opts {
		o(p)
	}
	b.props = append(b.props, p)
	return b
}

// Build materializes the schema for the given feature set.
func (b *SchemaBuilder) Build(fs hostenv.FeatureSet) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "object",
		Description: b.desc,
		Properties:  orderedmap.New[string, *jsonschema.Schema](),
	}
	if len(b.required) > 0 {
		s.Required = b.required
	}
	for _, p := range b.props {
		if !p.visible(fs) {
			continue
		}
		s.Properties.Set(p.name, p.materialize(fs))
	}
	return s
}

// RawJSON renders the compiled schema as a JSON document for a descriptor's
// InputSchema. A schema that cannot marshal degrades to an empty object schema
// so a bad contribution can never crash tool registration.
func (b *SchemaBuilder) RawJSON(fs hostenv.FeatureSet) json.RawMessage {
	raw, err := json.Marshal(b.Build(fs))
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return raw
}

func (p *schemaProp) visible(fs hostenv.FeatureSet) bool {
	if p.when != "" && !fs.Has(p.when) {
		return false
	}
	if p.unless != "" && fs.Has(p.unless) {
		return false
	}
	return true
}

func (p *schemaProp) materialize(fs hostenv.FeatureSet) *jsonschema.Schema {
	s := *p.schema // shallow copy; object properties are distinct pointers below
	if p.transform != nil {
		p.transform(&s, fs)
	}
	return &s
}
