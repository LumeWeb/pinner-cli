package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	require.Contains(t, got, "curated set of common Pinner tools")
	require.Contains(t, got, "progressive disclosure")
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

// TestSensitiveStringFlagSchemaAndNames verifies that a flag built with
// SensitiveStringFlag is (a) emitted into the schema as a plain string and
// (b) reported by sensitiveFlagNames so the adapter can redact its value,
// while a plain string flag is neither.
func TestSensitiveStringFlagSchemaAndNames(t *testing.T) {
	flags := []cli.Flag{
		SensitiveStringFlag(&cli.StringFlag{Name: "password", Usage: "Password", Aliases: []string{"p"}}),
		&cli.StringFlag{Name: "email", Usage: "Email"},
	}

	schema, err := flagsToSchema(flags, "")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	props := doc["properties"].(map[string]any)
	passwordSchema, ok := props["password"].(map[string]any)
	require.True(t, ok, "password flag must appear in schema")
	assert.Equal(t, "string", passwordSchema["type"])

	names := sensitiveFlagNames(flags)
	assert.Equal(t, []string{"password"}, names, "only the sensitive flag name is reported")
}

// TestSensitiveFlagNamesOmitsNonSensitive verifies sensitiveFlagNames ignores
// flags that do not implement SensitiveProvider.
func TestSensitiveFlagNamesOmitsNonSensitive(t *testing.T) {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "email", Usage: "Email"},
		&cli.BoolFlag{Name: "force", Usage: "Force"},
	}
	assert.Empty(t, sensitiveFlagNames(flags))
}

// TestSensitiveFlagRedactedFromArgTrace verifies the schema-derived redaction
// end to end: a tool whose command declares a SensitiveStringFlag must have
// its value masked (****) in the "invoking in-process" trace instead of the
// previously hardcoded flag-name list.
func TestSensitiveFlagRedactedFromArgTrace(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{
				Name: "auth",
				Commands: []*cli.Command{
					{
						Name: "login",
						Flags: []cli.Flag{
							SensitiveStringFlag(&cli.StringFlag{
								Name:    "password",
								Aliases: []string{"p"},
								Usage:   "Password",
							}),
							&cli.StringFlag{Name: "email", Usage: "Email"},
						},
						Action: func(context.Context, *cli.Command) error { return nil },
					},
				},
			},
		},
	}

	catalog, err := buildCatalog(root, true, nil, nil, nil)
	require.NoError(t, err)

	entry, ok := catalog.Get("pinner_auth_login")
	require.True(t, ok)
	require.Equal(t, []string{"password"}, entry.SensitiveFlags,
		"sensitive flag must be derived onto the catalog entry")

	// Swap the package logger for a capture buffer so we can assert on the
	// arg-trace line without polluting stderr.
	var buf bytes.Buffer
	oldLog := log
	log = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.InfoLevel,
	))
	t.Cleanup(func() { log = oldLog })

	_, err = entry.Handler(context.Background(), ToolRequest{
		Name: "pinner_auth_login",
		Arguments: map[string]any{
			"email":    "user@example.com",
			"password": "supersecret123",
		},
	})
	require.NoError(t, err)

	trace := buf.String()
	assert.Contains(t, trace, "--password")
	assert.Contains(t, trace, "****")
	assert.NotContains(t, trace, "supersecret123", "password value must not be logged verbatim")
	assert.Contains(t, trace, "user@example.com", "non-sensitive value is not redacted")
}

// TestRootSensitiveFlagRedactedFromSubcommand verifies that a root-level
// global sensitive flag (e.g. the global --auth-token) is unioned onto every
// subcommand's SensitiveFlags, so an agent passing it to any tool has its
// value redacted from the arg trace. This is a regression guard: root/global
// flags were previously dropped, leaking the live auth token verbatim.
func TestRootSensitiveFlagRedactedFromSubcommand(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Flags: []cli.Flag{
			SensitiveStringFlag(&cli.StringFlag{Name: "auth-token", Usage: "Auth token"}),
			&cli.StringFlag{Name: "email", Usage: "Email"},
		},
		Commands: []*cli.Command{
			{Name: "status", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}

	catalog, err := buildCatalog(root, true, nil, nil, nil)
	require.NoError(t, err)

	status, ok := catalog.Get("pinner_status")
	require.True(t, ok)
	require.Contains(t, status.SensitiveFlags, "auth-token",
		"root-level sensitive flag must be unioned onto the subcommand entry")

	// Render and assert the token is redacted from the arg trace.
	var buf bytes.Buffer
	oldLog := log
	log = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.InfoLevel,
	))
	t.Cleanup(func() { log = oldLog })

	_, err = status.Handler(context.Background(), ToolRequest{
		Name: "pinner_status",
		Arguments: map[string]any{
			"auth-token": "LIVE-AUTH-TOKEN-123",
			"email":      "user@example.com",
		},
	})
	require.NoError(t, err)

	trace := buf.String()
	assert.Contains(t, trace, "--auth-token")
	assert.Contains(t, trace, "****")
	assert.NotContains(t, trace, "LIVE-AUTH-TOKEN-123", "root auth token must be redacted")
	assert.Contains(t, trace, "user@example.com", "non-sensitive value is not redacted")
}

// TestUnionSensitiveFlagsDedupes verifies unionSensitiveFlags preserves order
// and drops duplicate names shared across the root and a subcommand.
func TestUnionSensitiveFlagsDedupes(t *testing.T) {
	got := unionSensitiveFlags([]string{"password", "key"}, []string{"key", "auth-token"})
	assert.Equal(t, []string{"password", "key", "auth-token"}, got)
}

// TestClassifyInteraction verifies the deterministic interaction classification
// for both stdin-input and interactive command paths.
func TestClassifyInteraction(t *testing.T) {
	cases := []struct {
		loc  []string
		want Interaction
	}{
		{[]string{"pinner", "upload"}, InteractionAgentSafe}, // upload guarded by isStdinPipe
		{[]string{"pinner", "pin"}, InteractionAgentSafe},
		{[]string{"pinner", "vault", "create"}, InteractionAgentSafe},
		{[]string{"pinner", "vault", "restore"}, InteractionStdinInput}, // --seed-stdin io.ReadAll
		{[]string{"pinner", "setup"}, InteractionInteractive},
		{[]string{"pinner", "list"}, InteractionAgentSafe},
		{[]string{"pinner", "admin", "billing", "subscribers", "list-users"}, InteractionAgentSafe},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, classifyInteraction(c.loc), "classifyInteraction(%v)", c.loc)
	}
}

// TestInteractionRegisteredOnCommandTree verifies RegisterFromCommand sets the
// Interaction field on catalog entries from the command path.
func TestInteractionRegisteredOnCommandTree(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{Name: "setup", Action: func(context.Context, *cli.Command) error { return nil }},
			{
				Name: "vault",
				Commands: []*cli.Command{
					{Name: "restore", Action: func(context.Context, *cli.Command) error { return nil }},
					{Name: "create", Action: func(context.Context, *cli.Command) error { return nil }},
				},
			},
			{Name: "list", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	catalog := NewToolCatalog()
	err := catalog.RegisterFromCommand(root, true, nil,
		func(context.Context, ToolRequest) (ToolResult, error) { return ToolResult{}, nil })
	require.NoError(t, err)

	restore, _ := catalog.Get("pinner_vault_restore")
	require.NotNil(t, restore)
	assert.Equal(t, InteractionStdinInput, restore.Interaction)

	setup, _ := catalog.Get("pinner_setup")
	require.NotNil(t, setup)
	assert.Equal(t, InteractionInteractive, setup.Interaction)

	create, _ := catalog.Get("pinner_vault_create")
	require.NotNil(t, create)
	assert.Equal(t, InteractionAgentSafe, create.Interaction)
}

// TestSearchHidesInteractiveTools verifies interactive (human-only) tools are
// omitted from search_tools while stdin_input and agent_safe tools stay
// discoverable with their interaction signal.
func TestSearchHidesInteractiveTools(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{Name: "setup", Description: "Run the setup wizard", Action: func(context.Context, *cli.Command) error { return nil }},
			{
				Name: "vault",
				Commands: []*cli.Command{
					{Name: "restore", Description: "Restore a vault", Action: func(context.Context, *cli.Command) error { return nil }},
				},
			},
			{Name: "status", Description: "Show status", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	catalog := NewToolCatalog()
	err := catalog.RegisterFromCommand(root, true, nil,
		func(context.Context, ToolRequest) (ToolResult, error) { return ToolResult{}, nil })
	require.NoError(t, err)

	summaries := catalog.Search("", "")
	var names []string
	var restoreSummary *ToolSummary
	for i := range summaries {
		names = append(names, summaries[i].Name)
		if summaries[i].Name == "pinner_vault_restore" {
			restoreSummary = &summaries[i]
		}
	}
	assert.NotContains(t, names, "pinner_setup", "interactive tool must be hidden from search_tools")
	assert.Contains(t, names, "pinner_vault_restore")
	assert.Contains(t, names, "pinner_status")
	require.NotNil(t, restoreSummary, "restore must remain discoverable")
	assert.Equal(t, InteractionStdinInput, restoreSummary.Interaction)

	// Describe must still surface the interaction label for a discoverable tool.
	detail, err := catalog.Describe("pinner_vault_restore")
	require.NoError(t, err)
	assert.Equal(t, InteractionStdinInput, detail.Interaction)
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

// TestVaultRestoreInteractionStaysStdinInputThroughBuildCatalog asserts that
// buildCatalog never reclassifies pinner_vault_restore away from stdin_input,
// even when an OOB restore coordinator is wired. The Interaction enum drives
// the invoke_tool stdin gate (sdk_official.go), which switches on
// entry.Interaction: if it became agent_safe, a --seed-stdin invocation would
// fall through the switch and run io.ReadAll(os.Stdin) — desyncing the stdio
// MCP transport. The non-stdin OOB hand-off is already permitted by the
// bypassGate, so the signal must stay stdin_input.
func TestVaultRestoreInteractionStaysStdinInputThroughBuildCatalog(t *testing.T) {
	root := &cli.Command{
		Name: "pinner",
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{Name: "restore", Action: func(context.Context, *cli.Command) error { return nil }},
				},
			},
		},
	}

	// With an OOB restore coordinator wired, restore must STILL be stdin_input.
	oobRestore := NewOOBRestore(nil, time.Minute)
	t.Cleanup(func() { oobRestore.Stop(context.Background()) })
	catalog, err := buildCatalog(root, true, nil, nil, oobRestore)
	require.NoError(t, err)
	restore, ok := catalog.Get("pinner_vault_restore")
	require.True(t, ok)
	assert.Equal(t, InteractionStdinInput, restore.Interaction,
		"buildCatalog must keep vault restore stdin_input so the --seed-stdin gate holds")
}

// TestSSOToolsDiscoverableInCatalog verifies the out-of-band sign-in tools are
// surfaced through progressive discovery, not just as direct (tools/list)
// descriptors. Previously a catalog search for "sso"/"oob"/"resume" returned
// zero results even though pinner_auth_sso and pinner_auth_resume existed as
// direct tools, so an agent that relies on search_tools could never find them.
func TestSSOToolsDiscoverableInCatalog(t *testing.T) {
	catalog := NewToolCatalog()
	// The descriptors carry the metadata; the handlers no-op on nil oob/handles
	// for discovery purposes.
	catalog.Add(toolEntryFromDescriptor(NewAuthSSODescriptor(nil, nil)))
	catalog.Add(toolEntryFromDescriptor(NewAuthResumeDescriptor(nil, nil)))

	for _, q := range []string{"sso", "oob", "resume", "sign-in", "out-of-band", "auth"} {
		summaries := catalog.Search(q, "")
		names := make([]string, 0, len(summaries))
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		assert.Contains(t, names, "pinner_auth_sso", "search %q must find pinner_auth_sso", q)
		assert.Contains(t, names, "pinner_auth_resume", "search %q must find pinner_auth_resume", q)
	}

	// Both are agent-safe (non-blocking) and present in the full listing.
	var ssoCount, resumeCount int
	for _, s := range catalog.Search("", "") {
		switch s.Name {
		case "pinner_auth_sso":
			ssoCount++
			assert.Equal(t, InteractionAgentSafe, s.Interaction)
		case "pinner_auth_resume":
			resumeCount++
			assert.Equal(t, InteractionAgentSafe, s.Interaction)
		}
	}
	assert.Equal(t, 1, ssoCount, "pinner_auth_sso must be listed exactly once")
	assert.Equal(t, 1, resumeCount, "pinner_auth_resume must be listed exactly once")

	// describe_tool / invoke_tool must also resolve them.
	d, err := catalog.Describe("pinner_auth_sso")
	require.NoError(t, err)
	assert.Equal(t, CategoryCore, d.Category)
	_, ok := catalog.Get("pinner_auth_resume")
	assert.True(t, ok, "pinner_auth_resume must be registered for describe/invoke")
}
