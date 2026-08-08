package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestHumanTitle(t *testing.T) {
	cases := []struct {
		loc  []string
		want string
	}{
		{[]string{"pinner", "upload"}, "Upload"},
		{[]string{"pinner", "websites", "domains", "create"}, "Websites Domains Create"},
		{[]string{"pinner", "admin", "billing", "subscribers", "list-users"}, "Admin Billing Subscribers List-Users"},
		{[]string{"pinner"}, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, humanTitle(c.loc), "humanTitle(%v)", c.loc)
	}
}

func TestReadOnlyAndDestructiveClassification(t *testing.T) {
	assert.True(t, isReadOnlyName([]string{"pinner", "list"}))
	assert.True(t, isReadOnlyName([]string{"pinner", "status"}))
	assert.False(t, isReadOnlyName([]string{"pinner", "upload"}))
	assert.False(t, isReadOnlyName([]string{"pinner", "rm"}))

	assert.True(t, isDestructiveName([]string{"pinner", "unpin"}))
	assert.True(t, isDestructiveName([]string{"pinner", "vault", "rm"}))
	assert.True(t, isDestructiveName([]string{"pinner", "admin", "billing", "credits", "purge"}))
	assert.True(t, isDestructiveName([]string{"pinner", "admin", "billing", "subscribers", "cancel"}))
	assert.False(t, isDestructiveName([]string{"pinner", "list"}))
}

func TestBuildInstructionsEmbedsCount(t *testing.T) {
	got := buildInstructions(42)
	require.Contains(t, got, "42 tools")
	require.Contains(t, got, "core, admin, and wizard")
}

// TestCommandAnnotationsRegistered verifies that RegisterFromCommand sets the
// title, read-only, and destructive hints on catalog entries for a realistic
// command tree.
func TestCommandAnnotationsRegistered(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{
				Name:        "upload",
				Description: "Upload a file to IPFS",
				Flags:       []cli.Flag{&cli.StringFlag{Name: "path", Required: true}},
				Action:      func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name:        "unpin",
				Description: "Remove a pinned CID",
				Flags:       []cli.Flag{&cli.BoolFlag{Name: "force"}},
				Action:      func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name: "websites",
				Commands: []*cli.Command{
					{
						Name: "domains",
						Commands: []*cli.Command{
							{
								Name:        "create",
								Description: "Create a website domain",
								Action:      func(context.Context, *cli.Command) error { return nil },
							},
						},
					},
				},
			},
		},
	}

	catalog := NewToolCatalog()
	handler := PinnerToolHandler(func(context.Context, ToolRequest) (ToolResult, error) { return ToolResult{}, nil })
	err := catalog.RegisterFromCommand(root, true, nil, handler)
	require.NoError(t, err)

	upload, ok := catalog.Get("pinner_upload")
	require.True(t, ok)
	assert.Equal(t, "Upload", upload.Title)
	assert.False(t, upload.ReadOnly)
	assert.False(t, upload.Destructive)

	unpin, ok := catalog.Get("pinner_unpin")
	require.True(t, ok)
	assert.Equal(t, "Unpin", unpin.Title)
	assert.True(t, unpin.Destructive)
	assert.False(t, unpin.ReadOnly)

	create, ok := catalog.Get("pinner_websites_domains_create")
	require.True(t, ok)
	assert.Equal(t, "Websites Domains Create", create.Title)
}

// TestDescribeToolCarriesAnnotations verifies that describe_tool output
// includes the annotation fields.
func TestDescribeToolCarriesAnnotations(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{
				Name:        "status",
				Description: "Show account status",
				Action:      func(context.Context, *cli.Command) error { return nil },
			},
			{
				Name: "vault",
				Commands: []*cli.Command{
					{
						Name:        "rm",
						Description: "Remove a vault file",
						Action:      func(context.Context, *cli.Command) error { return nil },
					},
				},
			},
		},
	}

	catalog := NewToolCatalog()
	err := catalog.RegisterFromCommand(root, true, nil,
		func(context.Context, ToolRequest) (ToolResult, error) { return ToolResult{}, nil })
	require.NoError(t, err)

	detail, err := catalog.Describe("pinner_status")
	require.NoError(t, err)
	assert.True(t, detail.ReadOnly)
	assert.False(t, detail.Destructive)
	assert.Equal(t, "Status", detail.Title)

	detail, err = catalog.Describe("pinner_vault_rm")
	require.NoError(t, err)
	assert.False(t, detail.ReadOnly)
	assert.True(t, detail.Destructive)
	assert.Equal(t, "Vault Rm", detail.Title)
	assert.True(t, strings.HasPrefix(detail.Description, "Remove a vault file"))
}

// TestEnumStringFlagEmitsEnum verifies that a flag built with EnumStringFlag
// gets its enum emitted into the MCP input schema, while a plain string flag
// does not.
func TestEnumStringFlagEmitsEnum(t *testing.T) {
	schema, err := flagsToSchema([]cli.Flag{EnumStringFlag("mode", "Cancel mode", false, "end_of_billing_period", "immediate", "end_of_billing_period")}, "")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	props := doc["properties"].(map[string]any)
	mode := props["mode"].(map[string]any)
	assert.Equal(t, []any{"immediate", "end_of_billing_period"}, mode["enum"])

	schema, err = flagsToSchema([]cli.Flag{&cli.StringFlag{Name: "path", Usage: "Path"}}, "")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(schema, &doc))
	props = doc["properties"].(map[string]any)
	_, hasEnum := props["path"].(map[string]any)["enum"]
	assert.False(t, hasEnum)
}

func TestStringSliceFlagEmitsArraySchema(t *testing.T) {
	schema, err := flagsToSchema([]cli.Flag{&cli.StringSliceFlag{Name: "tags", Usage: "Tags"}}, "")
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	property := doc["properties"].(map[string]any)["tags"].(map[string]any)
	assert.Equal(t, "array", property["type"])
	assert.Equal(t, map[string]any{"type": "string"}, property["items"])
}
