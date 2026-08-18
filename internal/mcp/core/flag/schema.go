package flag

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"
)

// FlagsToSchema converts urfave/cli flags into a Pinner-neutral JSON Schema
// object for a tool's input. The schema preserves the established wire format
// used by progressive disclosure (describe_tool).
//
// The returned schema is a JSON object of the form:
//
//	{"type":"object","properties":{...},"required":[...]}
//
// where "properties" is always present (possibly "{}") and "required" is
// omitted when empty. Each property carries a "type" plus optional
// "description"/"default"/"enum" fields. An optional argsUsage string adds
// an "_args" array property so MCP clients can pass positional CLI arguments.
func FlagsToSchema(flags []cli.Flag, argsUsage string) (json.RawMessage, error) {
	builder := newSchemaBuilder()
	for _, flag := range flags {
		if err := builder.addFlag(flag); err != nil {
			return nil, err
		}
	}
	builder.addArgs(argsUsage)
	return builder.marshal()
}

type schemaBuilder struct {
	properties map[string]any
	required   []string
}

func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{properties: make(map[string]any)}
}

func (b *schemaBuilder) addFlag(flag cli.Flag) error {
	switch f := flag.(type) {
	case *cli.StringFlag:
		b.addString(f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *sensitiveStringFlag:
		// A credential-bearing string flag; schema-wise it is a plain string.
		// Declaring it via SensitiveStringFlag keeps the schema identical to a
		// StringFlag while the adapter can redact its value from the arg trace.
		if f.StringFlag != nil {
			b.addString(f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		}
	case *enumStringFlag:
		b.addEnum(f.Name, f.Usage, f.Value, f.enum, f.Required, f.Hidden)
	case *cli.BoolFlag:
		if f.Name != "help" && f.Name != "version" {
			b.addProperty(f.Name, "boolean", f.Usage, f.Value, true, f.Required, f.Hidden)
		}
	case *cli.StringSliceFlag:
		b.addStringSlice(f.Name, f.Usage, f.Required, f.Hidden)
	case *cli.DurationFlag:
		b.addDuration(f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.FloatFlag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Float32Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.IntFlag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Int8Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Int16Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Int32Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Int64Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.UintFlag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Uint8Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Uint16Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Uint32Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	case *cli.Uint64Flag:
		addNumberProperty(b, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
	default:
		return fmt.Errorf("unsupported flag type: %T", flag)
	}
	return nil
}

func (b *schemaBuilder) addString(name, usage, value string, required, hidden bool) {
	if value == "" {
		b.addProperty(name, "string", usage, nil, false, required, hidden)
		return
	}
	b.addProperty(name, "string", usage, value, true, required, hidden)
}

func (b *schemaBuilder) addEnum(name, usage, value string, enum []string, required, hidden bool) {
	property := b.property(name, "string", usage, required, hidden)
	if property == nil {
		return
	}
	if value != "" {
		property["default"] = value
	}
	if len(enum) > 0 {
		property["enum"] = enum
	}
}

func (b *schemaBuilder) addStringSlice(name, usage string, required, hidden bool) {
	if hidden {
		return
	}
	property := b.property(name, "array", usage, required, hidden)
	property["items"] = map[string]any{"type": "string"}
}

func (b *schemaBuilder) addDuration(name, usage string, value time.Duration, required, hidden bool) {
	description := fmt.Sprintf("%s (duration, e.g. 5m, 1h30m)", usage)
	if value == 0 {
		b.addProperty(name, "string", description, nil, false, required, hidden)
		return
	}
	b.addProperty(name, "string", description, value.String(), true, required, hidden)
}

func addNumberProperty[T int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64](b *schemaBuilder, name, usage string, value T, required, hidden bool) {
	b.addProperty(name, "number", usage, float64(value), value != 0, required, hidden)
}

func (b *schemaBuilder) addProperty(name, typ, description string, value any, hasDefault, required, hidden bool) {
	b.property(name, typ, description, required, hidden)
	if hasDefault && !hidden {
		b.properties[name].(map[string]any)["default"] = value
	}
}

func (b *schemaBuilder) property(name, typ, description string, required, hidden bool) map[string]any {
	if hidden {
		return nil
	}
	property := map[string]any{
		"type":        typ,
		"description": description,
	}
	b.properties[name] = property
	if required {
		b.required = append(b.required, name)
	}
	return property
}

func (b *schemaBuilder) addArgs(argsUsage string) {
	if argsUsage == "" {
		return
	}
	b.properties["_args"] = map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": "Positional arguments: " + argsUsage,
	}
}

func (b *schemaBuilder) marshal() (json.RawMessage, error) {
	obj := map[string]any{
		"type":       "object",
		"properties": b.properties,
	}
	if len(b.required) > 0 {
		obj["required"] = b.required
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}
