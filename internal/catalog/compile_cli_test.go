package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

// TestCLICompilerCommandCount compiles a sample catalog spanning the safety and
// interaction spectrum and asserts one *cli.Command per operation.
func TestCLICompilerCommandCount(t *testing.T) {
	c := buildCompileSample(t)
	cmds, err := NewCLICompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(cmds) != 4 {
		t.Fatalf("len(cmds) = %d, want 4", len(cmds))
	}
}

// TestCLICompilerCommandIdentity checks the declared Command fields derived from
// the Operation (name, usage from Summary, description).
func TestCLICompilerCommandIdentity(t *testing.T) {
	op := compiledOp(t, "vault_create")
	if op.Name != "vault_create" {
		t.Fatalf("cmd.Name = %q, want vault.create", op.Name)
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

// TestCLICompilerDestructiveForceGate asserts a destructive op's Action refuses
// without --force and succeeds with it.
func TestCLICompilerDestructiveForceGate(t *testing.T) {
	cmd := compiledOp(t, "vault.delete")

	// Without --force the Action must refuse.
	err := cmd.Run(context.Background(), []string{"vault.delete", "--name", "x"})
	if err == nil {
		t.Fatal("destructive action should refuse without --force")
	}

	// With --force it reaches the Handler and succeeds.
	err = cmd.Run(context.Background(), []string{"vault.delete", "--name", "x", "--force"})
	if err != nil {
		t.Fatalf("destructive action with --force should succeed: %v", err)
	}
}

// TestCLICompilerNumericZeroNotEmpty pins the valueFor fix: an explicit zero
// (e.g. --ttl 0) on a numeric/duration arg is a legitimate value, not "empty".
// Earlier code treated v==0 as empty and wrongly refused a required numeric arg
// set to 0 with "required argument --ttl was empty".
func TestCLICompilerNumericZeroNotEmpty(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule", Title: "Schedule", Summary: "schedule a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			{Name: "ttl", Type: ArgTypeInt, Required: true},
			{Name: "grace", Type: ArgTypeDuration, Required: true},
		},
		Handler: markerHandler{marker: "ran:job.schedule"},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cmds, err := NewCLICompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var cmd *cli.Command
	for _, cc := range cmds {
		if cc.Name == "job.schedule" {
			cmd = cc
			break
		}
	}
	if cmd == nil {
		t.Fatal("job.schedule command not found")
	}
	// Using --ttl 0 --grace 0s must reach the handler, not be rejected as empty.
	if err := cmd.Run(context.Background(), []string{"job.schedule", "--name", "x", "--ttl", "0", "--grace", "0s"}); err != nil {
		t.Fatalf("action with zero numeric values should succeed: %v", err)
	}
}

// TestCLICompilerAppliesDefaults pins the round-4 fix: the CLI actionFor must
// apply declared defaults uniformly with the Invoke path (via
// normalizeInputDefaults), so a Handler sees the same keys on the CLI as on MCP.
// It also covers required+default-arg satisfaction: an arg that is both Required
// and carries a Default is satisfied by the default rather than rejected.
func TestCLICompilerAppliesDefaults(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule", Title: "Schedule", Summary: "schedule a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			// Required AND defaulted: the default satisfies requiredness.
			{Name: "ttl", Type: ArgTypeInt, Required: true, Default: "3600"},
			{Name: "grace", Type: ArgTypeDuration, Default: "30s"},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cmds, err := NewCLICompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var cmd *cli.Command
	for _, cc := range cmds {
		if cc.Name == "job.schedule" {
			cmd = cc
			break
		}
	}
	if cmd == nil {
		t.Fatal("job.schedule command not found")
	}
	// Provide only the required name arg; the ttl/grace defaults must be filled
	// by the shared normalizeInputDefaults sink (same as the Invoke path).
	if err := cmd.Run(context.Background(), []string{"job.schedule", "--name", "x"}); err != nil {
		t.Fatalf("action with defaulted args should succeed: %v", err)
	}
	if h.got["ttl"] != 3600 {
		t.Fatalf("default ttl not applied on CLI, got %#v", h.got["ttl"])
	}
	if d, ok := h.got["grace"].(time.Duration); !ok || d != 30*time.Second {
		t.Fatalf("default grace not applied on CLI, got %#v", h.got["grace"])
	}
}

// TestCLICompilerRejectsForceArgOnDestructive pins the round-11 fix: a
// destructive operation must not declare an arg named "force", which would
// collide with the synthetic --force confirm gate (urfave 'flag redefined').
// Compile must return a descriptive error instead.
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
	if _, err := NewCLICompiler().Compile(c); err == nil {
		t.Fatal("Compile of destructive op with 'force' arg should error")
	}
}

// TestCLICompilerPositionalOnlyNoFlag asserts an arg marked PositionalOnly
// (its value is supplied by the command's positional argument) is NOT emitted
// as a urfave --flag, while a sibling non-positional arg still is. This is the
// mechanism that removes the redundant `--zone string` flag from the DNS
// zone/record ops, which already expose <domain> positionally.
func TestCLICompilerPositionalOnlyNoFlag(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "dns.records.create", Title: "Create", Summary: "create a record",
		Description: "create a dns record", Category: "dns", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Positional: "<domain>",
		Args: []OperationArg{
			{Name: "zone", Type: ArgTypeString, Required: true, PositionalOnly: true, Help: "Domain name or numeric zone ID"},
			{Name: "name", Type: ArgTypeString, Help: "Record name (or @ for apex)"},
			{Name: "type", Type: ArgTypeString, Required: true, Help: "Record type"},
		},
		Handler: markerHandler{marker: "ran:dns.records.create"},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cmds, err := NewCLICompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	cmd := cmds[0]

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

// compiledOp compiles the sample catalog and returns the command named name.
func compiledOp(t *testing.T, name string) *cli.Command {
	t.Helper()
	cmds, err := NewCLICompiler().Compile(buildCompileSample(t))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, cmd := range cmds {
		if cmd.Name == name {
			return cmd
		}
	}
	t.Fatalf("command %q not found in compiled output", name)
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
