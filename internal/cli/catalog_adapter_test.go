package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/opmesh"
	catalogops "go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/config"
	configmocks "go.lumeweb.com/pinner/core/config/mocks"
)

// ---------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------

// stubHandler is an opmesh.Handler that records every normalized input the
// pipeline passes to Execute, plus whether the derived context carried a
// deadline, so tests can assert the pipeline fed it the NORMALIZED map (not the
// pre-normalize ic.Input) with applyDefaultTimeout applied.
type stubHandler struct {
	result      any
	err         error
	mu          sync.Mutex
	inputs      []map[string]any
	deadlineSet bool
}

func (s *stubHandler) Execute(ctx context.Context, input map[string]any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, input)
	if _, ok := ctx.Deadline(); ok {
		s.deadlineSet = true
	}
	return s.result, s.err
}

func (s *stubHandler) invocationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inputs)
}

func (s *stubHandler) firstInput() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inputs) == 0 {
		return nil
	}
	return s.inputs[0]
}

// newStubOp builds an opmesh.Operation via opmesh.NewOperation. The only reason
// it is a helper (not a bare NewOperation call at the call site) is to avoid
// repeating the boilerplate descriptor fields across every test.
func newStubOp(handler opmesh.Handler, safety opmesh.Safety, args []opmesh.OperationArg, positional string) opmesh.Operation {
	return opmesh.NewOperation(opmesh.OperationSpec{
		Name:        "demo_add",
		Title:       "Demo Add",
		Summary:     "demo add operation",
		Description: "demo add operation used by catalog adapter tests",
		Args:        args,
		Positional:  positional,
		Safety:      safety,
		Interaction: opmesh.InteractionAgentSafe,
		Visibility:  opmesh.VisibilityModel,
		Category:    "demo",
		Handler:     handler,
	})
}

// demoMutateOp is the canonical non-destructive op used by the default-path
// tests: a required string, a defaulted string, a selection group, and a
// positional binding to the required string arg.
func demoMutateOp(handler opmesh.Handler) opmesh.Operation {
	return newStubOp(handler, opmesh.SafetyMutate, []opmesh.OperationArg{
		{Name: "website", Type: opmesh.ArgTypeString, Required: true, Help: "the site"},
		{Name: "region", Type: opmesh.ArgTypeString, Default: "us-east-1"},
		{Name: "cids", Type: opmesh.ArgTypeStringSlice, SelectionGroup: "scope"},
		{Name: "all", Type: opmesh.ArgTypeBool, SelectionGroup: "scope"},
	}, "[<website>]")
}

// positionalOp declares a right-aligned two-slot positional usage so
// right-align, surplus, and double-supply behaviors can be pinned.
func positionalOp(handler opmesh.Handler) opmesh.Operation {
	return newStubOp(handler, opmesh.SafetyMutate, []opmesh.OperationArg{
		{Name: "website", Type: opmesh.ArgTypeString, Default: "default-site"},
		{Name: "region", Type: opmesh.ArgTypeString, Default: "default-region"},
	}, "[<website>] <region>")
}

// opFlag maps an operation arg to its urfave flag for the pipeline command.
func opFlag(a opmesh.OperationArg) cli.Flag {
	switch a.Type {
	case opmesh.ArgTypeBool:
		return &cli.BoolFlag{Name: a.Name}
	case opmesh.ArgTypeStringSlice:
		return &cli.StringSliceFlag{Name: a.Name}
	case opmesh.ArgTypeInt:
		return &cli.IntFlag{Name: a.Name}
	case opmesh.ArgTypeFloat:
		return &cli.Float64Flag{Name: a.Name}
	case opmesh.ArgTypeDuration:
		return &cli.DurationFlag{Name: a.Name}
	default:
		return &cli.StringFlag{Name: a.Name}
	}
}

// newPipelineCommand builds a cli.Command with flags matching the op args (plus
// the shared force/confirm/all/dry-run/auth-token flags where absent) and wires
// catalogActionAdapter(op, cfg) as its Action. Driving it through cmd.Run parses
// flags and positional args exactly as the real command path does.
func newPipelineCommand(op opmesh.Operation, cfg CatalogAdapterConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(op.Args())+5)
	seen := make(map[string]bool, len(op.Args())+5)
	for _, a := range op.Args() {
		seen[a.Name] = true
		flags = append(flags, opFlag(a))
	}
	for name, f := range map[string]cli.Flag{
		FlagForce:     &cli.BoolFlag{Name: FlagForce},
		FlagConfirm:   &cli.BoolFlag{Name: FlagConfirm},
		FlagAll:       &cli.BoolFlag{Name: FlagAll},
		FlagDryRun:    &cli.BoolFlag{Name: FlagDryRun},
		FlagAuthToken: &cli.StringFlag{Name: FlagAuthToken},
	} {
		if !seen[name] {
			flags = append(flags, f)
		}
	}
	return &cli.Command{
		Name:   "testcmd",
		Flags:  flags,
		Action: catalogActionAdapter(op, cfg),
	}
}

// runPipeline runs the shared pipeline command with the given CLI args
// (excluding the program name, which Run strips as argv[0]).
func runPipeline(cmd *cli.Command, args ...string) error {
	return cmd.Run(context.Background(), append([]string{"testcmd"}, args...))
}

// recordingOutput is an Output that delegates to the real formatter but also
// captures whatever the pipeline/gates print, so tests can assert on written
// hint text.
type recordingOutput struct {
	Output
	buf    bytes.Buffer
	prints []string
}

func newRecordingOutput() *recordingOutput {
	return &recordingOutput{Output: newTestOutput()}
}

func (r *recordingOutput) record(s string) {
	r.buf.WriteString(s)
	r.prints = append(r.prints, s)
}

func (r *recordingOutput) Print(message string)              { r.record(message) }
func (r *recordingOutput) Printf(format string, args ...any) { r.record(fmt.Sprintf(format, args...)) }
func (r *recordingOutput) Printfln(format string, args ...any) {
	r.record(fmt.Sprintf(format+"\n", args...))
}

// newGateContext builds a CatalogInvokeContext wired to a command carrying the
// shared force/confirm/all/dry-run flags (values preset via cmd.Set) plus a
// recording output, for driving the gate factories directly.
func newGateContext(force, confirm, all, dryRun bool) (*CatalogInvokeContext, *recordingOutput) {
	cmd := &cli.Command{Flags: []cli.Flag{
		&cli.BoolFlag{Name: FlagForce},
		&cli.BoolFlag{Name: FlagConfirm},
		&cli.BoolFlag{Name: FlagAll},
		&cli.BoolFlag{Name: FlagDryRun},
	}}
	if force {
		_ = cmd.Set(FlagForce, "true")
	}
	if confirm {
		_ = cmd.Set(FlagConfirm, "true")
	}
	if all {
		_ = cmd.Set(FlagAll, "true")
	}
	if dryRun {
		_ = cmd.Set(FlagDryRun, "true")
	}
	rec := newRecordingOutput()
	ic := &CatalogInvokeContext{
		C:       cmd,
		Input:   map[string]any{},
		Output:  rec,
		Confirm: cmd.Bool(FlagForce) || cmd.Bool(FlagConfirm),
		DryRun:  cmd.Bool(FlagDryRun),
	}
	return ic, rec
}

// ===========================================================================
// Default-path tests
// ===========================================================================

func TestCatalogAdapterFlagsToInputMapping(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	err := runPipeline(cmd, "--website", "example.com", "--cids", "a", "--cids", "b")
	require.NoError(t, err)

	input := handler.firstInput()
	require.NotNil(t, input)
	assert.Equal(t, "example.com", input["website"])
	// Declared default is filled by normalize.
	assert.Equal(t, "us-east-1", input["region"])
	// Zero-shape for an omitted slice/bool.
	assert.Equal(t, []string{"a", "b"}, input["cids"])
	assert.Equal(t, false, input["all"])
}

func TestCatalogAdapterAuthTokenOverrideOn(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	cfg := CatalogAdapterConfig{Renderer: noopRenderer, HonorAuthTokenOverride: true}
	cmd := newPipelineCommand(op, cfg)

	err := runPipeline(cmd, "--website", "example.com", "--auth-token", "override-tok")
	require.NoError(t, err)

	input := handler.firstInput()
	require.NotNil(t, input)
	// Reserved auth key survives NormalizeOperationInput.
	assert.Equal(t, "override-tok", input[catalogops.AuthTokenInputKey])
}

func TestCatalogAdapterAuthTokenOverrideOff(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	// HonorAuthTokenOverride defaults to false -> the reserved key is never added.
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	err := runPipeline(cmd, "--website", "example.com", "--auth-token", "override-tok")
	require.NoError(t, err)

	input := handler.firstInput()
	require.NotNil(t, input)
	_, ok := input[catalogops.AuthTokenInputKey]
	assert.False(t, ok, "auth token override must not leak when HonorAuthTokenOverride is false")
}

func TestCatalogAdapterPositionalRightAlign(t *testing.T) {
	// One positional rights-aligns into the last slot (region).
	{
		handler := &stubHandler{result: "rendered"}
		op := positionalOp(handler)
		cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})
		err := runPipeline(cmd, "west")
		require.NoError(t, err)
		assert.Equal(t, "default-site", handler.firstInput()["website"])
		assert.Equal(t, "west", handler.firstInput()["region"])
	}

	// Two positionals fill both slots in order.
	{
		handler := &stubHandler{result: "rendered"}
		op := positionalOp(handler)
		cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})
		err := runPipeline(cmd, "mysite", "south")
		require.NoError(t, err)
		assert.Equal(t, "mysite", handler.firstInput()["website"])
		assert.Equal(t, "south", handler.firstInput()["region"])
	}
}

func TestCatalogAdapterPositionalSurplusRejected(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := positionalOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	err := runPipeline(cmd, "a", "b", "c")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected extra argument")
	assert.Zero(t, handler.invocationCount(), "handler must not run on surplus positional input")
}

func TestCatalogAdapterPositionalDoubleSupplyRejected(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := positionalOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	// website supplied both as --flag and as a positional -> ambiguous.
	err := runPipeline(cmd, "--website", "flagval", "mysite", "reg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provided both as a flag and as a positional argument")
	assert.Zero(t, handler.invocationCount(), "handler must not run on ambiguous double-supply")
}

func TestCatalogAdapterNormalizeRequiredMissing(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	// website is Required and has no Default -> must be supplied.
	err := runPipeline(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing required argument "website"`)
	assert.Zero(t, handler.invocationCount(), "handler must not run when a required arg is missing")
}

func TestCatalogAdapterNormalizeDefaultFilled(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", handler.firstInput()["region"])
}

func TestCatalogAdapterNormalizeSelectionGroupEnforced(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	// cids (slice) and all (bool) are mutually exclusive in group "scope".
	err := runPipeline(cmd, "--website", "example.com", "--cids", "a", "--all")
	require.Error(t, err)
	assert.ErrorIs(t, err, opmesh.ErrSelector)
	assert.Zero(t, handler.invocationCount(), "handler must not run on a selection-group violation")
}

func TestCatalogAdapterTimeoutApplied(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)

	prev := configManagerFactory
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{DefaultTimeout: 2 * time.Second}).Maybe()
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	defer func() { configManagerFactory = prev }()

	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)

	require.True(t, handler.deadlineSet, "applyDefaultTimeout must set a deadline on the Execute context")
}

func TestCatalogAdapterExecuteReceivesNormalizedInput(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)
	// Registering a PostNormalizeWatch lets us capture the normalized map and
	// prove Execute receives exactly it (defaults filled, separate from ic.Input).
	var normalized map[string]any
	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		PostNormalizeWatch: func(_ *CatalogInvokeContext, n map[string]any) (bool, error) {
			normalized = n
			return false, nil
		},
	}
	cmd := newPipelineCommand(op, cfg)

	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)

	executed := handler.firstInput()
	require.NotNil(t, executed)
	assert.Equal(t, normalized, executed, "Execute must receive the normalized input map")
}

func TestCatalogAdapterRendererReceivesResult(t *testing.T) {
	handler := &stubHandler{result: "final-result"}
	op := demoMutateOp(handler)

	var got any
	cfg := CatalogAdapterConfig{
		Renderer: func(_ context.Context, _ *cli.Command, _ opmesh.Operation, result any) error {
			got = result
			return nil
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)
	assert.Equal(t, "final-result", got)
}

func TestCatalogAdapterDestructiveGateDefault(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	// Destructive ops that pass through the default gate must declare a confirm
	// arg: the gate writes input["confirm"], which NormalizeOperationInput
	// otherwise rejects as an unrecognized argument.
	op := newStubOp(handler, opmesh.SafetyDestructive, []opmesh.OperationArg{
		{Name: "confirm", Type: opmesh.ArgTypeBool},
		{Name: "website", Type: opmesh.ArgTypeString, Required: true},
	}, "[<website>]")

	// No --force -> default GateForceReject refuses with the leaf-name message.
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})
	err := runPipeline(cmd, "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add: pass --force to confirm this destructive operation")
	assert.Zero(t, handler.invocationCount())

	// --force -> proceeds.
	cmd = newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})
	err = runPipeline(cmd, "--force", "example.com")
	require.NoError(t, err)
	assert.Equal(t, 1, handler.invocationCount())
}

// ===========================================================================
// Slot-mechanics tests
// ===========================================================================

func TestCatalogAdapterHookOrder(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := newStubOp(handler, opmesh.SafetyDestructive, []opmesh.OperationArg{
		{Name: "website", Type: opmesh.ArgTypeString, Required: true},
	}, "[<website>]")

	var order []string
	appendHook := func(name string) { order = append(order, name) }

	cfg := CatalogAdapterConfig{
		Renderer: func(_ context.Context, _ *cli.Command, _ opmesh.Operation, _ any) error {
			appendHook("Render")
			return nil
		},
		HonorAuthTokenOverride: true,
		ResolvePositional: func(ic *CatalogInvokeContext) error {
			appendHook("ResolvePositional")
			return opmesh.MapPositionalArgs(ic.Op.Args(), ic.Op.Positional(), ic.C.Args().Slice(), ic.Input)
		},
		MutateInput: func(ic *CatalogInvokeContext) error { appendHook("MutateInput"); return nil },
		ResolveIDs:  func(ic *CatalogInvokeContext) error { appendHook("ResolveIDs"); return nil },
		DestructiveGate: func(ic *CatalogInvokeContext) (bool, error) {
			appendHook("DestructiveGate")
			return false, nil
		},
		ExtraConfirm: func(ic *CatalogInvokeContext) error { appendHook("ExtraConfirm"); return nil },
		UpdateGuard:  func(ic *CatalogInvokeContext) error { appendHook("UpdateGuard"); return nil },
		PreNormalizeWatch: func(ic *CatalogInvokeContext) (bool, error) {
			appendHook("PreNormalizeWatch")
			return false, nil
		},
		PostNormalizeWatch: func(ic *CatalogInvokeContext, _ map[string]any) (bool, error) {
			appendHook("PostNormalizeWatch")
			return false, nil
		},
		OnExecuteError: func(ic *CatalogInvokeContext, err error) error {
			appendHook("OnExecuteError")
			return err
		},
		PostExecute: func(ic *CatalogInvokeContext, _ any) error {
			appendHook("PostExecute")
			return nil
		},
	}

	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--force", "example.com")
	require.NoError(t, err)

	expected := []string{
		"ResolvePositional", "MutateInput", "ResolveIDs", "DestructiveGate",
		"ExtraConfirm", "UpdateGuard", "PreNormalizeWatch", "PostNormalizeWatch",
		"PostExecute", "Render",
	}
	assert.Equal(t, expected, order, "hooks must run in the defined order on the success path")
}

func TestCatalogAdapterPreNormalizeWatchShortCircuit(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)

	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		PreNormalizeWatch: func(ic *CatalogInvokeContext) (bool, error) {
			return true, nil
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)
	assert.Zero(t, handler.invocationCount(), "pre-normalize watch that handles must skip normalize and execute")
}

func TestCatalogAdapterPostNormalizeWatchShortCircuit(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)

	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		PostNormalizeWatch: func(ic *CatalogInvokeContext, _ map[string]any) (bool, error) {
			return true, nil
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)
	assert.Zero(t, handler.invocationCount(), "post-normalize watch that handles must skip execute")
}

func TestCatalogAdapterPreNormalizeWatchErrorPropagates(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)

	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		PreNormalizeWatch: func(ic *CatalogInvokeContext) (bool, error) {
			return true, errors.New("watch refused")
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.EqualError(t, err, "watch refused")
	assert.Zero(t, handler.invocationCount())
}

func TestCatalogAdapterDestructiveGateTriState(t *testing.T) {
	t.Run("proceed", func(t *testing.T) {
		handler := &stubHandler{result: "rendered"}
		op := newStubOp(handler, opmesh.SafetyDestructive, []opmesh.OperationArg{
			{Name: "website", Type: opmesh.ArgTypeString, Required: true},
		}, "")
		cfg := CatalogAdapterConfig{
			Renderer:        noopRenderer,
			DestructiveGate: func(ic *CatalogInvokeContext) (bool, error) { return false, nil },
		}
		err := runPipeline(newPipelineCommand(op, cfg), "--website", "x")
		require.NoError(t, err)
		assert.Equal(t, 1, handler.invocationCount())
	})

	t.Run("abort-success", func(t *testing.T) {
		handler := &stubHandler{result: "rendered"}
		op := newStubOp(handler, opmesh.SafetyDestructive, nil, "")
		cfg := CatalogAdapterConfig{
			Renderer:        noopRenderer,
			DestructiveGate: func(ic *CatalogInvokeContext) (bool, error) { return true, nil },
		}
		err := runPipeline(newPipelineCommand(op, cfg))
		require.NoError(t, err, "handled=true with nil err must exit 0")
		assert.Zero(t, handler.invocationCount())
	})

	t.Run("abort-error", func(t *testing.T) {
		handler := &stubHandler{result: "rendered"}
		op := newStubOp(handler, opmesh.SafetyDestructive, nil, "")
		cfg := CatalogAdapterConfig{
			Renderer: noopRenderer,
			DestructiveGate: func(ic *CatalogInvokeContext) (bool, error) {
				return true, errors.New("refused: pass --force")
			},
		}
		err := runPipeline(newPipelineCommand(op, cfg))
		require.EqualError(t, err, "refused: pass --force")
		assert.Zero(t, handler.invocationCount())
	})
}

func TestCatalogAdapterOnExecuteErrorDefaultIdentity(t *testing.T) {
	handler := &stubHandler{err: errors.New("boom")}
	op := demoMutateOp(handler)
	cmd := newPipelineCommand(op, CatalogAdapterConfig{Renderer: noopRenderer})

	err := runPipeline(cmd, "--website", "example.com")
	require.EqualError(t, err, "boom", "default OnExecuteError must be identity")
}

func TestCatalogAdapterOnExecuteErrorWraps(t *testing.T) {
	handler := &stubHandler{err: errors.New("boom")}
	op := demoMutateOp(handler)
	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		OnExecuteError: func(ic *CatalogInvokeContext, err error) error {
			return fmt.Errorf("demo add failed: %w", err)
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo add failed: boom")
}

func TestCatalogAdapterPostExecuteReceivesResult(t *testing.T) {
	handler := &stubHandler{result: "typed-result"}
	op := demoMutateOp(handler)

	var postGot any
	var postRanBeforeRender bool
	cfg := CatalogAdapterConfig{
		Renderer: func(_ context.Context, _ *cli.Command, _ opmesh.Operation, _ any) error {
			postRanBeforeRender = true
			return nil
		},
		PostExecute: func(_ *CatalogInvokeContext, result any) error {
			postGot = result
			return nil
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)
	assert.Equal(t, "typed-result", postGot)
	assert.True(t, postRanBeforeRender, "PostExecute must run before the renderer")
}

func TestCatalogAdapterNormalizedIsSeparateMap(t *testing.T) {
	handler := &stubHandler{result: "rendered"}
	op := demoMutateOp(handler)

	var preInput map[string]any
	var postNormalized map[string]any
	cfg := CatalogAdapterConfig{
		Renderer: noopRenderer,
		PreNormalizeWatch: func(ic *CatalogInvokeContext) (bool, error) {
			preInput = ic.Input
			return false, nil
		},
		PostNormalizeWatch: func(ic *CatalogInvokeContext, normalized map[string]any) (bool, error) {
			postNormalized = normalized
			// Mutate the normalized map to prove ic.Input is unaffected.
			normalized["website"] = "mutated-in-normalize"
			return false, nil
		},
	}
	cmd := newPipelineCommand(op, cfg)
	err := runPipeline(cmd, "--website", "example.com")
	require.NoError(t, err)

	// normalized is a separate map object from the pre-normalize ic.Input.
	require.NotEqual(t,
		reflect.ValueOf(preInput).Pointer(),
		reflect.ValueOf(postNormalized).Pointer(),
		"normalized map must be a distinct allocation")
	// ic.Input had the raw flag value; mutating normalized must not touch it.
	assert.Equal(t, "example.com", preInput["website"], "mutating normalized must not touch the pre-normalize input")
	// The handler receives the (mutated) normalized map.
	assert.Equal(t, "mutated-in-normalize", handler.firstInput()["website"])
}

// ===========================================================================
// Gate-factory table tests
// ===========================================================================

func TestGateForceReject(t *testing.T) {
	ic, _ := newGateContext(false, false, false, false) // not confirmed
	msg := func(*CatalogInvokeContext) string { return "op: pass --force to confirm this destructive operation" }
	alwaysReject := func(*CatalogInvokeContext) bool { return true }
	neverReject := func(*CatalogInvokeContext) bool { return false }

	g := GateForceReject(alwaysReject, msg)

	t.Run("rejects when unconfirmed and predicate holds", func(t *testing.T) {
		ic.Input = map[string]any{}
		handled, err := g(ic)
		assert.True(t, handled)
		require.EqualError(t, err, "op: pass --force to confirm this destructive operation")
		assert.Equal(t, false, ic.Input["confirm"], "confirm must be written for the caller")
	})

	t.Run("fall-through when predicate does not hold", func(t *testing.T) {
		g2 := GateForceReject(neverReject, msg)
		ic.Input = map[string]any{}
		handled, err := g2(ic)
		assert.False(t, handled)
		require.NoError(t, err)
	})

	t.Run("proceeds when confirmed", func(t *testing.T) {
		ic2, _ := newGateContext(true, false, false, false) // --force
		ic2.Input = map[string]any{}
		handled, err := GateForceReject(alwaysReject, msg)(ic2)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Equal(t, true, ic2.Input["confirm"])
	})
}

func TestGateInteractivePrompt(t *testing.T) {
	accepted := func(*CatalogInvokeContext) (bool, error) { return true, nil }
	declined := func(*CatalogInvokeContext) (bool, error) { return false, nil }
	promptErr := func(*CatalogInvokeContext) (bool, error) { return false, errors.New("boom") }
	// Modeled on promptPlatformDomainDelete's non-interactive branch: a
	// --force-directive error in non-interactive contexts.
	nonInteractive := func(*CatalogInvokeContext) (bool, error) {
		return false, errors.New("admin-platform-domains delete: pass --force to confirm this destructive operation")
	}
	msg := func(*CatalogInvokeContext) string { return "deletion aborted" }

	t.Run("accepted prompt proceeds with confirm", func(t *testing.T) {
		ic, _ := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateInteractivePrompt(accepted, msg)(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.True(t, ic.Confirm)
		assert.Equal(t, true, ic.Input["confirm"])
	})

	t.Run("declined prompt aborts", func(t *testing.T) {
		ic, _ := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateInteractivePrompt(declined, msg)(ic)
		assert.True(t, handled)
		require.EqualError(t, err, "deletion aborted")
	})

	t.Run("prompt error propagates as handled", func(t *testing.T) {
		ic, _ := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateInteractivePrompt(promptErr, msg)(ic)
		assert.True(t, handled)
		require.EqualError(t, err, "boom")
	})

	t.Run("non-interactive branch surfaces --force directive", func(t *testing.T) {
		ic, _ := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateInteractivePrompt(nonInteractive, msg)(ic)
		assert.True(t, handled)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pass --force to confirm this destructive operation")
	})

	t.Run("pre-confirmed skips prompt", func(t *testing.T) {
		ic, _ := newGateContext(true, false, false, false) // --force
		ic.Input = map[string]any{}
		handled, err := GateInteractivePrompt(declined, msg)(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Equal(t, true, ic.Input["confirm"])
	})
}

func TestGateHintSilent(t *testing.T) {
	hintWhen := func(*CatalogInvokeContext) bool { return true }
	noHint := func(*CatalogInvokeContext) bool { return false }
	hint := func(ic *CatalogInvokeContext, out Output) string { return "note: nothing removed" }

	t.Run("prints hint and exits 0 when unconfirmed destructive", func(t *testing.T) {
		ic, rec := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateHintSilent(hintWhen, hint)(ic)
		assert.True(t, handled)
		require.NoError(t, err)
		assert.Equal(t, false, ic.Input["confirm"])
		assert.Contains(t, rec.buf.String(), "note: nothing removed")
	})

	t.Run("writes all=true for --all", func(t *testing.T) {
		ic, _ := newGateContext(false, false, true, false) // --all
		ic.Input = map[string]any{}
		handled, err := GateHintSilent(hintWhen, hint)(ic)
		assert.True(t, handled)
		require.NoError(t, err)
		assert.Equal(t, true, ic.Input["all"])
	})

	t.Run("falls through when confirmed", func(t *testing.T) {
		ic, rec := newGateContext(true, false, false, false) // --force
		ic.Input = map[string]any{}
		handled, err := GateHintSilent(hintWhen, hint)(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Empty(t, rec.buf.String(), "no hint when confirmed")
	})

	t.Run("falls through on dry-run", func(t *testing.T) {
		ic, rec := newGateContext(false, false, false, true) // --dry-run
		ic.Input = map[string]any{}
		handled, err := GateHintSilent(hintWhen, hint)(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Empty(t, rec.buf.String(), "no hint on dry-run")
	})

	t.Run("falls through when hint predicate false", func(t *testing.T) {
		ic, _ := newGateContext(false, false, false, false)
		ic.Input = map[string]any{}
		handled, err := GateHintSilent(noHint, hint)(ic)
		assert.False(t, handled)
		require.NoError(t, err)
	})
}

func TestGatePassthroughForce(t *testing.T) {
	g := GatePassthroughForce()

	t.Run("writes confirm from --force only", func(t *testing.T) {
		ic, _ := newGateContext(true, false, false, false) // --force, no --confirm alias
		ic.Input = map[string]any{}
		handled, err := g(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Equal(t, true, ic.Input["confirm"])
	})

	t.Run("no force -> confirm false, never refuses", func(t *testing.T) {
		ic, _ := newGateContext(false, true, false, false) // --confirm alias does NOT count
		ic.Input = map[string]any{}
		handled, err := g(ic)
		assert.False(t, handled)
		require.NoError(t, err)
		assert.Equal(t, false, ic.Input["confirm"], "GatePassthroughForce must not honor the --confirm alias")
	})
}

func TestGateNone(t *testing.T) {
	ic, _ := newGateContext(false, false, false, false)
	ic.Input = map[string]any{}
	handled, err := GateNone()(ic)
	assert.False(t, handled)
	require.NoError(t, err)
	assert.Len(t, ic.Input, 0, "GateNone must not write confirm or any other key")
}

// noopRenderer is a Renderer that returns nil without doing anything, used for
// default-path tests that do not care about rendering.
func noopRenderer(context.Context, *cli.Command, opmesh.Operation, any) error { return nil }
