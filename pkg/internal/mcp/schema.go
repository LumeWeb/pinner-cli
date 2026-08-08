package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v3"
)

// flagsToSchema converts urfave/cli flags into a Pinner-neutral JSON Schema
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
// an "_args" array property so MCP
// clients can pass positional CLI arguments.
func flagsToSchema(flags []cli.Flag, argsUsage string) (json.RawMessage, error) {
	properties := map[string]any{}
	var required []string

	for _, flag := range flags {
		switch f := flag.(type) {
		case *cli.StringFlag:
			if f.Hidden {
				continue
			}
			prop := map[string]any{
				"type":        "string",
				"description": f.Usage,
			}
			if f.Value != "" {
				prop["default"] = f.Value
			}
			if f.Required {
				required = append(required, f.Name)
			}
			properties[f.Name] = prop

		case *enumStringFlag:
			if f.Hidden {
				continue
			}
			prop := map[string]any{
				"type":        "string",
				"description": f.Usage,
			}
			if f.Value != "" {
				prop["default"] = f.Value
			}
			if len(f.enum) > 0 {
				prop["enum"] = f.enum
			}
			if f.Required {
				required = append(required, f.Name)
			}
			properties[f.Name] = prop

		case *cli.BoolFlag:
			if f.Name == "help" || f.Name == "version" || f.Hidden {
				continue
			}
			prop := map[string]any{
				"type":        "boolean",
				"description": f.Usage,
				"default":     f.Value,
			}
			if f.Required {
				required = append(required, f.Name)
			}
			properties[f.Name] = prop

		case *cli.StringSliceFlag:
			if f.Hidden {
				continue
			}
			prop := map[string]any{
				"type":        "string",
				"description": f.Usage + "\n(comma-separated for multiple values)",
			}
			if f.Required {
				required = append(required, f.Name)
			}
			properties[f.Name] = prop

		case *cli.DurationFlag:
			if f.Hidden {
				continue
			}
			prop := map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("%s (duration, e.g. 5m, 1h30m)", f.Usage),
			}
			if f.Value != 0 {
				prop["default"] = f.Value.String()
			}
			if f.Required {
				required = append(required, f.Name)
			}
			properties[f.Name] = prop

		// Numeric flags. FloatFlag and Float64Flag are type aliases in
		// urfave/cli v3 (both are FlagBase[float64, ...]), so only one
		// case appears. Same for IntFlag/Int64Flag on 64-bit platforms
		// but they are distinct types at the type-system level.
		case *cli.FloatFlag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Float32Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)

		case *cli.IntFlag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Int8Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Int16Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Int32Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Int64Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)

		case *cli.UintFlag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Uint8Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Uint16Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Uint32Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)
		case *cli.Uint64Flag:
			addNumberProperty(properties, &required, f.Name, f.Usage, f.Value, f.Required, f.Hidden)

		default:
			return nil, fmt.Errorf("unsupported flag type: %T", flag)
		}
	}

	// If the command has positional args, add an _args property to the
	// schema so MCP clients know they can pass positionals via the _args
	// array field.
	if argsUsage != "" {
		properties["_args"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Positional arguments: " + argsUsage,
		}
	}

	obj := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		obj["required"] = required
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// addNumberProperty adds a numeric-typed property to a schema.
func addNumberProperty[T int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64](properties map[string]any, required *[]string, name, usage string, value T, isRequired, hidden bool) {
	if hidden {
		return
	}
	prop := map[string]any{
		"type":        "number",
		"description": usage,
		"default":     float64(value),
	}
	if isRequired {
		*required = append(*required, name)
	}
	properties[name] = prop
}
