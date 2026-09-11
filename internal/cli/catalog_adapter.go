// Package cli types for the shared catalog adapter pipeline.
package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/opmesh"
	clicatalog "go.lumeweb.com/pinner-cli/internal/clicatalog"
	catalogops "go.lumeweb.com/pinner/catalogops"
)

// CatalogRenderer renders a catalog op result for a domain. The nine existing
// render*Result functions use this signature and are passed into configs
// verbatim.
type CatalogRenderer func(ctx context.Context, c *cli.Command, op opmesh.Operation, result any) error

// CatalogInvokeContext carries per-invocation state shared by all pipeline
// hooks.
type CatalogInvokeContext struct {
	Ctx     context.Context
	C       *cli.Command
	Op      opmesh.Operation
	Input   map[string]any
	Output  Output
	Attribs map[string]any
	Confirm bool
	DryRun  bool
}

// CatalogAdapterConfig expresses all per-domain behavior as hooks. Every
// field is optional; the shared pipeline supplies a safe default for each
// unset field (see the pipeline's resolve* helpers). Renderer is required.
type CatalogAdapterConfig struct {
	Renderer CatalogRenderer

	HonorAuthTokenOverride bool
	ResolvePositional      func(ic *CatalogInvokeContext) error
	MutateInput            func(ic *CatalogInvokeContext) error
	ResolveIDs             func(ic *CatalogInvokeContext) error
	DestructiveGate        func(ic *CatalogInvokeContext) (handled bool, err error)
	ExtraConfirm           func(ic *CatalogInvokeContext) error
	UpdateGuard            func(ic *CatalogInvokeContext) error
	PreNormalizeWatch      func(ic *CatalogInvokeContext) (handled bool, err error)
	PostNormalizeWatch     func(ic *CatalogInvokeContext, normalized map[string]any) (handled bool, err error)
	OnExecuteError         func(ic *CatalogInvokeContext, err error) error
	PostExecute            func(ic *CatalogInvokeContext, result any) error
}

// catalogActionAdapter is the single shared execution pipeline for all
// catalog-driven CLI commands. It expresses each per-domain difference through
// CatalogAdapterConfig hooks; NormalizeOperationInput is a fixed step so CLI
// and MCP/Catalog.Invoke stay consistent.
func catalogActionAdapter(op opmesh.Operation, cfg CatalogAdapterConfig) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		ic := &CatalogInvokeContext{
			Ctx:     ctx,
			C:       c,
			Op:      op,
			Input:   clicatalog.FlagsToInput(c, op),
			Output:  setupOutput(c),
			Attribs: map[string]any{},
			Confirm: c.Bool(FlagForce) || c.Bool(FlagConfirm),
			DryRun:  c.Bool(FlagDryRun),
		}
		if cfg.HonorAuthTokenOverride {
			if tok := c.String(FlagAuthToken); tok != "" {
				ic.Input[catalogops.AuthTokenInputKey] = tok
			}
		}
		if err := ic.resolvePositional(cfg); err != nil {
			return err
		}
		if err := ic.mutateInput(cfg); err != nil {
			return err
		}
		if err := ic.resolveIDs(cfg); err != nil {
			return err
		}
		if op.Safety() == opmesh.SafetyDestructive {
			if handled, err := ic.destructiveGate(cfg); handled {
				return err
			}
		}
		if err := ic.extraConfirm(cfg); err != nil {
			return err
		}
		if err := ic.updateGuard(cfg); err != nil {
			return err
		}
		if handled, err := ic.preNormalizeWatch(cfg); handled {
			return err
		}
		normalized, err := opmesh.NormalizeOperationInput(op, ic.Input)
		if err != nil {
			return err
		}
		if handled, err := ic.postNormalizeWatch(cfg, normalized); handled {
			return err
		}
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()
		result, err := op.Handler().Execute(dctx, normalized)
		if err != nil {
			return ic.onExecuteError(cfg, err)
		}
		if err := ic.postExecute(cfg, result); err != nil {
			return err
		}
		return cfg.Renderer(ctx, c, op, result)
	}
}

// resolvePositional binds positional args to op inputs. Default: the
// canonical opmesh.MapPositionalArgs (right-aligned, surplus and
// double-supply rejected).
func (ic *CatalogInvokeContext) resolvePositional(cfg CatalogAdapterConfig) error {
	if cfg.ResolvePositional != nil {
		return cfg.ResolvePositional(ic)
	}
	return opmesh.MapPositionalArgs(ic.Op.Args(), ic.Op.Positional(), ic.C.Args().Slice(), ic.Input)
}

// mutateInput applies op-specific input surgery. Default: none.
func (ic *CatalogInvokeContext) mutateInput(cfg CatalogAdapterConfig) error {
	if cfg.MutateInput != nil {
		return cfg.MutateInput(ic)
	}
	return nil
}

// resolveIDs resolves symbolic IDs to numeric ones before execution (admin
// platform-domains / social-providers). Default: none.
func (ic *CatalogInvokeContext) resolveIDs(cfg CatalogAdapterConfig) error {
	if cfg.ResolveIDs != nil {
		return cfg.ResolveIDs(ic)
	}
	return nil
}

// destructiveGate enforces --force/--confirm for SafetyDestructive ops.
// Default: reject without --force (safe, compiler-consistent wording).
func (ic *CatalogInvokeContext) destructiveGate(cfg CatalogAdapterConfig) (bool, error) {
	if cfg.DestructiveGate != nil {
		return cfg.DestructiveGate(ic)
	}
	return GateForceReject(
		func(*CatalogInvokeContext) bool { return true },
		func(ic *CatalogInvokeContext) string {
			return opLeafName(ic.Op) + ": pass --force to confirm this destructive operation"
		},
	)(ic)
}

// extraConfirm runs an additional op-specific confirmation prompt. Default: none.
func (ic *CatalogInvokeContext) extraConfirm(cfg CatalogAdapterConfig) error {
	if cfg.ExtraConfirm != nil {
		return cfg.ExtraConfirm(ic)
	}
	return nil
}

// updateGuard enforces the at-least-one-field rule for update ops. Default: none.
func (ic *CatalogInvokeContext) updateGuard(cfg CatalogAdapterConfig) error {
	if cfg.UpdateGuard != nil {
		return cfg.UpdateGuard(ic)
	}
	return nil
}

// preNormalizeWatch short-circuits before normalize for ops whose watch runs
// the service directly. Default: not handled.
func (ic *CatalogInvokeContext) preNormalizeWatch(cfg CatalogAdapterConfig) (bool, error) {
	if cfg.PreNormalizeWatch != nil {
		return cfg.PreNormalizeWatch(ic)
	}
	return false, nil
}

// postNormalizeWatch re-executes the handler on normalized input for ops with
// an output.Watch loop. Default: not handled.
func (ic *CatalogInvokeContext) postNormalizeWatch(cfg CatalogAdapterConfig, normalized map[string]any) (bool, error) {
	if cfg.PostNormalizeWatch != nil {
		return cfg.PostNormalizeWatch(ic, normalized)
	}
	return false, nil
}

// onExecuteError decorates an execute error. Default: identity.
func (ic *CatalogInvokeContext) onExecuteError(cfg CatalogAdapterConfig, err error) error {
	if cfg.OnExecuteError != nil {
		return cfg.OnExecuteError(ic, err)
	}
	return err
}

// postExecute runs a post-execute, pre-render side effect (e.g. account --open).
func (ic *CatalogInvokeContext) postExecute(cfg CatalogAdapterConfig, result any) error {
	if cfg.PostExecute != nil {
		return cfg.PostExecute(ic, result)
	}
	return nil
}

// opLeafName returns the last underscore-separated segment of an op name
// (e.g. "vault_rm" -> "rm"), used for the default destructive-gate message.
func opLeafName(op opmesh.Operation) string {
	name := op.Name()
	if i := strings.LastIndex(name, "_"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// firstPositional returns the first positional arg and whether one exists.
func firstPositional(ic *CatalogInvokeContext) (string, bool) {
	if ic.C == nil || ic.C.Args() == nil || ic.C.Args().Len() == 0 {
		return "", false
	}
	return ic.C.Args().First(), true
}

// posFirstToNamed binds the first positional arg to the named string arg when
// that arg is currently empty (the shared "first positional -> named arg"
// override family).
func posFirstToNamed(ic *CatalogInvokeContext, arg string) error {
	first, ok := firstPositional(ic)
	if !ok {
		return nil
	}
	if opmesh.StrArg(ic.Input, arg, "") == "" {
		ic.Input[arg] = first
	}
	return nil
}

// posFirstToType binds the first positional arg to the first declared arg
// matching pred when that arg is empty.
func posFirstToType(ic *CatalogInvokeContext, pred func(opmesh.OperationArg) bool) error {
	first, ok := firstPositional(ic)
	if !ok {
		return nil
	}
	for _, a := range ic.Op.Args() {
		if !pred(a) {
			continue
		}
		if opmesh.StrArg(ic.Input, a.Name, "") == "" {
			ic.Input[a.Name] = first
		}
		return nil
	}
	return nil
}

// GateForceReject writes confirm and rejects (handled=true, error) when the
// caller did not pass --force and the rejectWhen predicate holds.
func GateForceReject(rejectWhen func(ic *CatalogInvokeContext) bool, msg func(ic *CatalogInvokeContext) string) func(ic *CatalogInvokeContext) (bool, error) {
	return func(ic *CatalogInvokeContext) (bool, error) {
		ic.Input["confirm"] = ic.Confirm
		if !ic.Confirm && rejectWhen(ic) {
			return true, errors.New(msg(ic))
		}
		return false, nil
	}
}

// GateInteractivePrompt runs an interactive confirmation prompt when the caller
// did not pass --force/--confirm. The prompt is expected to produce a
// --force-directive error in non-interactive contexts (as the admin
// platform-domains prompt does); a declined prompt aborts ("deletion
// aborted"), an accepted prompt proceeds with confirm=true.
func GateInteractivePrompt(prompt func(ic *CatalogInvokeContext) (bool, error), msg func(ic *CatalogInvokeContext) string) func(ic *CatalogInvokeContext) (bool, error) {
	return func(ic *CatalogInvokeContext) (bool, error) {
		if !ic.Confirm {
			ok, err := prompt(ic)
			if err != nil {
				return true, err
			}
			if !ok {
				return true, errors.New(msg(ic))
			}
			ic.Confirm = true
		}
		ic.Input["confirm"] = ic.Confirm
		return false, nil
	}
}

// GateHintSilent writes confirm (and "all" when --all) and, for an unconfirmed
// destructive op that is not a dry-run, prints a hint to stdout and exits 0
// (the pins rm unconfirmed path). Otherwise it falls through.
func GateHintSilent(hintWhen func(ic *CatalogInvokeContext) bool, hint func(ic *CatalogInvokeContext, output Output) string) func(ic *CatalogInvokeContext) (bool, error) {
	return func(ic *CatalogInvokeContext) (bool, error) {
		ic.Input["confirm"] = ic.Confirm
		if ic.C.Bool(FlagAll) {
			ic.Input["all"] = true
		}
		if !ic.Confirm && !ic.DryRun && hintWhen(ic) {
			ic.Output.Printfln("%s", hint(ic, ic.Output))
			return true, nil
		}
		return false, nil
	}
}

// GatePassthroughForce writes confirm from --force only (no --confirm alias)
// and never refuses; the core service decides (apikeys delete).
func GatePassthroughForce() func(ic *CatalogInvokeContext) (bool, error) {
	return func(ic *CatalogInvokeContext) (bool, error) {
		ic.Input["confirm"] = ic.C.Bool(FlagForce)
		return false, nil
	}
}

// GateNone performs no confirmation write and never handles (ipns keys delete;
// the op's confirm default true + handler check carry the safety).
func GateNone() func(ic *CatalogInvokeContext) (bool, error) {
	return func(ic *CatalogInvokeContext) (bool, error) {
		return false, nil
	}
}
