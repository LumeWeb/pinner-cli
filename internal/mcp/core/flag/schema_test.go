package flag

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestFlagsToSchema_AllSupportedFlagFamilies(t *testing.T) {
	schema, err := FlagsToSchema([]cli.Flag{
		&cli.StringFlag{Name: "name", Usage: "Name", Value: "default"},
		EnumStringFlag("mode", "Mode", true, "fast", "fast", "safe"),
		&cli.BoolFlag{Name: "enabled", Usage: "Enabled"},
		&cli.StringSliceFlag{Name: "tags", Usage: "Tags"},
		&cli.DurationFlag{Name: "timeout", Usage: "Timeout", Value: 5 * time.Second},
		&cli.IntFlag{Name: "count", Usage: "Count", Value: 3},
		&cli.Float32Flag{Name: "ratio", Usage: "Ratio", Value: 1.5},
		&cli.Uint64Flag{Name: "limit", Usage: "Limit", Value: 7},
	}, "files to process")
	require.NoError(t, err)

	doc := decodeSchema(t, schema)
	properties := doc["properties"].(map[string]any)

	assert.Equal(t, map[string]any{
		"type":        "string",
		"description": "Name",
		"default":     "default",
	}, properties["name"])
	assert.Equal(t, map[string]any{
		"type":        "string",
		"description": "Mode",
		"default":     "fast",
		"enum":        []any{"fast", "safe"},
	}, properties["mode"])
	assert.Equal(t, map[string]any{
		"type":        "boolean",
		"description": "Enabled",
		"default":     false,
	}, properties["enabled"])
	assert.Equal(t, map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": "Tags",
	}, properties["tags"])
	assert.Equal(t, map[string]any{
		"type":        "string",
		"description": "Timeout (duration, e.g. 5m, 1h30m)",
		"default":     "5s",
	}, properties["timeout"])
	assert.Equal(t, map[string]any{
		"type":        "number",
		"description": "Count",
		"default":     float64(3),
	}, properties["count"])
	assert.Equal(t, map[string]any{
		"type":        "number",
		"description": "Ratio",
		"default":     1.5,
	}, properties["ratio"])
	assert.Equal(t, map[string]any{
		"type":        "number",
		"description": "Limit",
		"default":     float64(7),
	}, properties["limit"])
	assert.Equal(t, []any{"mode"}, doc["required"])
	assert.Equal(t, map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": "Positional arguments: files to process",
	}, properties["_args"])
}

func TestFlagsToSchema_OmitsHiddenAndSpecialFlags(t *testing.T) {
	schema, err := FlagsToSchema([]cli.Flag{
		&cli.StringFlag{Name: "hidden", Hidden: true},
		&cli.BoolFlag{Name: "help"},
		&cli.BoolFlag{Name: "version"},
	}, "")
	require.NoError(t, err)

	properties := decodeSchema(t, schema)["properties"].(map[string]any)
	assert.Empty(t, properties)
}

func TestFlagsToSchema_OmitsZeroDefaults(t *testing.T) {
	schema, err := FlagsToSchema([]cli.Flag{
		&cli.StringFlag{Name: "string"},
		&cli.DurationFlag{Name: "duration"},
		&cli.IntFlag{Name: "count"},
	}, "")
	require.NoError(t, err)

	properties := decodeSchema(t, schema)["properties"].(map[string]any)
	assert.NotContains(t, properties["string"], "default")
	assert.NotContains(t, properties["duration"], "default")
	assert.NotContains(t, properties["count"], "default")
}

func TestFlagsToSchema_RejectsUnsupportedFlag(t *testing.T) {
	_, err := FlagsToSchema([]cli.Flag{unsupportedSchemaFlag{}}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported flag type")
}

type unsupportedSchemaFlag struct{}

func (unsupportedSchemaFlag) String() string           { return "unsupported" }
func (unsupportedSchemaFlag) Get() any                 { return nil }
func (unsupportedSchemaFlag) PreParse() error          { return nil }
func (unsupportedSchemaFlag) PostParse() error         { return nil }
func (unsupportedSchemaFlag) Set(string, string) error { return nil }
func (unsupportedSchemaFlag) Names() []string          { return []string{"unsupported"} }
func (unsupportedSchemaFlag) IsSet() bool              { return false }

func decodeSchema(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	return schema
}
