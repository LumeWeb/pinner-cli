package clicatalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestCLICompilerCommandIdentity checks the declared Command fields derived from
// the Operation (name, usage from Summary, description) on the leaf builder
// (NewCLILeaf -> commandFor) that every production domain routes through.
func TestCLICompilerCommandIdentity(t *testing.T) {
	op := compiledOp(t, "vault_create")
	if op.Name != "vault_create" {
		t.Fatalf("cmd.Name = %q, want vault_create", op.Name)
	}
	if op.Usage != "create a vault" {
		t.Fatalf("cmd.Usage = %q, want from Summary", op.Usage)
	}
	if op.Description != "long vault create description" {
		t.Fatalf("cmd.Description = %q, want from Description", op.Description)
	}
	// Human-only op should carry the interactive note in its usage.
	human := compiledOp(t, "account.login")
	if human.Usage != "log in interactively (requires interactive human input)" {
		t.Fatalf("human-only cmd.Usage = %q", human.Usage)
	}
}

// TestCLICompilerFlagMapping asserts each ArgType becomes the matching urfave flag.
func TestCLICompilerFlagMapping(t *testing.T) {
	cmd := compiledOp(t, "vault_create")

	want := map[string]struct {
		wantType string // %T of the flag
		required bool
	}{
		"name":   {"*cli.StringFlag", true},
		"ttl":    {"*cli.IntFlag", false},
		"rate":   {"*cli.Float64Flag", false},
		"grace":  {"*cli.DurationFlag", false},
		"tags":   {"*cli.StringSliceFlag", false},
		"public": {"*cli.BoolFlag", false},
	}
	if len(cmd.Flags) != len(want) {
		t.Fatalf("len(cmd.Flags) = %d, want %d", len(cmd.Flags), len(want))
	}
	byName := map[string]cli.Flag{}
	for _, f := range cmd.Flags {
		if n := f.Names(); len(n) > 0 {
			byName[n[0]] = f
		}
	}
	for name, exp := range want {
		f, ok := byName[name]
		if !ok {
			t.Errorf("missing flag %q", name)
			continue
		}
		if got := flagKind(f); got != exp.wantType {
			t.Errorf("flag %q type = %s, want %s", name, got, exp.wantType)
		}
		if got := requiredFlag(f); got != exp.required {
			t.Errorf("flag %q required = %v, want %v", name, got, exp.required)
		}
	}
}

// TestCLICompilerRejectsForceArgOnDestructive pins the fix: a destructive
// operation must not declare an arg named "force", which would collide with the
// synthetic --force confirm gate (urfave 'flag redefined'). The leaf builder
// (NewCLILeaf -> commandFor) must return a descriptive error instead.
func TestCLICompilerRejectsForceArgOnDestructive(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "vault.nuke", Title: "Nuke", Summary: "destroy",
		Category: "vault", Safety: SafetyDestructive,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Args:    []OperationArg{{Name: "force", Type: ArgTypeBool}},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	op := findOp(t, c, "vault.nuke")
	if _, err := NewCLILeaf(op, op.Name(), "", nil); err == nil {
		t.Fatal("NewCLILeaf of destructive op with 'force' arg should error")
	}
}

// TestCLICompilerPositionalOnlyNoFlag asserts an arg marked PositionalOnly
// (its value is supplied by the command's positional argument) is NOT emitted
// as a urfave --flag, while a sibling non-positional arg still is. This is the
// mechanism that removes the redundant `--zone string` flag from the DNS
// zone/record ops, which already expose <domain> positionally.
func TestCLICompilerPositionalOnlyNoFlag(t *testing.T) {
	// The PositionalOnly carve-out is frontend metadata keyed by the stable
	// operation ID in module catalogmeta (dns_records_create's "zone" arg is
	// PositionalOnly), so this test pins the merge against the real
	// module metadata rather than an inline field.
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "dns_records_create", Title: "Create", Summary: "create a record",
		Description: "create a dns record", Category: "dns", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Positional: "<domain>",
		Args: []OperationArg{
			{Name: "zone", Type: ArgTypeString, Required: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: ArgTypeString, Help: "Record name (or @ for apex)"},
			{Name: "type", Type: ArgTypeString, Required: true, Help: "Record type"},
		},
		Handler: markerHandler{marker: "ran:dns_records_create"},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cmd := leafFor(t, c, "dns_records_create")

	// The PositionalOnly "zone" arg must appear in no flag; name and type must.
	got := map[string]bool{}
	for _, f := range cmd.Flags {
		if n := f.Names(); len(n) > 0 {
			got[n[0]] = true
		}
	}
	for _, wantFlag := range []string{"name", "type"} {
		if !got[wantFlag] {
			t.Errorf("expected flag --%s to be emitted, got flags %v", wantFlag, got)
		}
	}
	if got["zone"] {
		t.Errorf("PositionalOnly arg 'zone' must NOT be emitted as a flag, got flags %v", got)
	}
}

// TestCLICompilerAgentOnlyNoFlag asserts an arg marked AgentOnly (exposed on
// the agent/MCP surface only) is NOT emitted as a urfave --flag, while a
// sibling non-agent arg still is. This lets an operation carry an agent-only
// control (e.g. a type discriminator the CLI derives automatically) without
// surfacing a CLI flag for it.
func TestCLICompilerAgentOnlyNoFlag(t *testing.T) {
	c := NewCatalog()
	// The AgentOnly carve-out is frontend metadata keyed by the stable
	// operation ID in module catalogmeta (websites_create's platform/label
	// args are AgentOnly), so this test pins against the real module metadata.
	if err := c.Add(NewOperation(OperationSpec{
		Name: "websites_create", Title: "Create", Summary: "create a site",
		Description: "create a website", Category: "websites", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Positional: "<domain>",
		Args: []OperationArg{
			{Name: "website", Type: ArgTypeString, Help: "Custom domain"},
			{Name: "cid", Type: ArgTypeString, Required: true, Help: "IPFS CID"},
			{Name: "platform", Type: ArgTypeBool, Help: "Claim a platform subdomain"},
			{Name: "label", Type: ArgTypeString, Help: "Subdomain label"},
		},
		Handler: markerHandler{marker: "ran:websites_create"},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cmd := leafFor(t, c, "websites_create")

	// Only the non-agent args (website, cid) are emitted as flags; the
	// AgentOnly platform/label args must not appear.
	got := map[string]bool{}
	for _, f := range cmd.Flags {
		if n := f.Names(); len(n) > 0 {
			got[n[0]] = true
		}
	}
	for _, wantFlag := range []string{"website", "cid"} {
		if !got[wantFlag] {
			t.Errorf("expected flag --%s to be emitted, got flags %v", wantFlag, got)
		}
	}
	for _, none := range []string{"platform", "label"} {
		if got[none] {
			t.Errorf("AgentOnly arg '%s' must NOT be emitted as a flag, got flags %v", none, got)
		}
	}
}

// buildCompileSample returns a catalog with a Read, Mutate, Destructive, and
// HumanOnly operation. The mutate op ("vault_create") carries one flag of every
// ArgType to exercise flag mapping.
func buildCompileSample(t *testing.T) Catalog {
	t.Helper()
	c := NewCatalog()

	ok := func(op Operation) {
		if err := c.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}

	ok(NewOperation(OperationSpec{
		Name: "vault.list", Title: "List Vaults", Summary: "list vaults",
		Description: "long list description", Category: "vault",
		Safety: SafetyRead, Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Handler: markerHandler{marker: "ran:vault.list"},
	}))

	ok(NewOperation(OperationSpec{
		Name: "vault_create", Title: "Create Vault", Summary: "create a vault",
		Description: "long vault create description", Positional: "NAME",
		Category: "vault", Safety: SafetyMutate, Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true, Help: "vault name"},
			{Name: "ttl", Type: ArgTypeInt, Help: "seconds"},
			{Name: "rate", Type: ArgTypeFloat, Help: "rate"},
			{Name: "grace", Type: ArgTypeDuration, Help: "grace period"},
			{Name: "tags", Type: ArgTypeStringSlice, Help: "tags"},
			{Name: "public", Type: ArgTypeBool, Help: "expose publicly"},
		},
		Handler: markerHandler{marker: "ran:vault.create"},
	}))

	ok(NewOperation(OperationSpec{
		Name: "vault.delete", Title: "Delete Vault", Summary: "delete a vault",
		Description: "long delete description", Category: "vault",
		Safety: SafetyDestructive, Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true, Help: "vault name"},
		},
		Handler: markerHandler{marker: "ran:vault.delete"},
	}))

	ok(NewOperation(OperationSpec{
		Name: "account.login", Title: "Login", Summary: "log in interactively",
		Description: "long login description", Category: "auth",
		Safety: SafetyMutate, Interaction: InteractionHumanOnly, Visibility: VisibilityBoth,
		Handler: markerHandler{marker: "ran:account.login"},
	}))

	return c
}

// compiledOp builds a leaf for the named operation in the sample catalog via
// the production leaf builder (NewCLILeaf) and returns the *cli.Command.
func compiledOp(t *testing.T, name string) *cli.Command {
	t.Helper()
	return leafFor(t, buildCompileSample(t), name)
}

// leafFor builds a *cli.Command leaf for the named operation in cat through
// the production leaf builder path (NewCLILeaf -> commandFor). It is the
// direct test consumer for the shape/flag construction that all catalog
// domains share.
func leafFor(t *testing.T, c Catalog, name string) *cli.Command {
	t.Helper()
	op := findOp(t, c, name)
	cmd, err := NewCLILeaf(op, name, "", nil)
	if err != nil {
		t.Fatalf("NewCLILeaf(%s): %v", name, err)
	}
	return cmd
}

// findOp returns the operation in cat whose Name() matches name.
func findOp(t *testing.T, c Catalog, name string) Operation {
	t.Helper()
	for _, op := range c.Search("", "", VisibilityBoth) {
		if op.Name() == name {
			return op
		}
	}
	t.Fatalf("operation %q not found", name)
	return nil
}

// flagKind names the concrete urfave flag type via type assertion. Because v3
// aliases each flag to a generic FlagBase, use the exported alias types rather
// than %T (which would print the underlying generic instantiation).
func flagKind(f cli.Flag) string {
	switch f.(type) {
	case *cli.StringFlag:
		return "*cli.StringFlag"
	case *cli.BoolFlag:
		return "*cli.BoolFlag"
	case *cli.IntFlag:
		return "*cli.IntFlag"
	case *cli.Float64Flag:
		return "*cli.Float64Flag"
	case *cli.DurationFlag:
		return "*cli.DurationFlag"
	case *cli.StringSliceFlag:
		return "*cli.StringSliceFlag"
	default:
		return fmt.Sprintf("%T", f)
	}
}

// requiredFlag reports whether the urfave flag is marked required.
type requireder interface{ IsRequired() bool }

func requiredFlag(f cli.Flag) bool {
	if r, ok := f.(requireder); ok {
		return r.IsRequired()
	}
	return false
}

// TestFlagsToInput pins the shared flag->input-map construction used by every
// CLI wiring adapter (websites, vault, dns, ipns, account, apikeys, operations):
// it must place a value for each declared arg from a urfave-parsed command,
// mirroring FlagValue, and return nil when the op declares no args.
func TestFlagsToInput(t *testing.T) {
	op := NewOperation(OperationSpec{
		Name: "test_thing",
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString},
			{Name: "count", Type: ArgTypeInt},
			{Name: "enabled", Type: ArgTypeNullableBool},
		},
	})

	cmd := &cli.Command{
		Name: "test_thing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name"},
			&cli.IntFlag{Name: "count"},
			&cli.BoolFlag{Name: "enabled"},
		},
		Action: func(ctx context.Context, c *cli.Command) error { return nil },
	}
	if err := cmd.Run(context.Background(), []string{"test_thing", "--name", "x", "--count", "3"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	input := FlagsToInput(cmd, op)
	if input["name"] != "x" {
		t.Errorf("name = %v, want x", input["name"])
	}
	if input["count"] != 3 {
		t.Errorf("count = %v, want 3", input["count"])
	}
	// enabled not supplied: nullable-bool surfaces nil (absent).
	if input["enabled"] != nil {
		t.Errorf("enabled = %v, want nil (absent)", input["enabled"])
	}

	// An op with no args returns nil.
	if got := FlagsToInput(cmd, NewOperation(OperationSpec{Name: "no_args"})); got != nil {
		t.Errorf("no-arg op returned %v, want nil", got)
	}
}
