package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"

	"github.com/samber/lo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// startupProfile returns the static profile for the startup transport, used to
// resolve DescFunc-only MCPTarget fallbacks when the compiled surface is built
// (no per-request profile is available there). The startup transport is
// derived from the same flags that SetTransportFlags records (co-located stdio,
// OpenAI tunnel, or plain HTTP).
func startupProfile() hostenv.PlatformProfile {
	p := hostenv.ProfileForTransport(transfer.UploadFileTransport(transportFlagsVar.coLocated, transportFlagsVar.tunnelOpenAI))
	// The server surface is a construction-time property recorded by
	// buildCatalog; carry it on the startup profile so profile-aware tool
	// description/schema resolution (which reads the profile's surface) agrees
	// with what was actually registered.
	p.Surface = activeSurface()
	return p
}

// This file is the bridge between the operation catalog (the compiler-backed
// source of truth for MCP tool descriptions/schemas) and the legacy ToolCatalog
// that drives the official MCP server's progressive-disclosure meta-tools
// (search_tools, describe_tool, invoke_tool).
//
// The compiled catalog yields ToolDescriptors whose Description/InputSchema
// come from the catalogops MCPTargets fallback and typed arg metadata, so CLI
// help prose, global flag bags, and empty "required" arrays never leak into
// the model surface. Each compiled operation is surfaced as a ToolEntry whose
// Handler routes through catalog.Catalog.Invoke the dispatch gate so
// Interaction, Visibility, Safety, and required-arg enforcement hold.

// compiledHandler wraps the operation catalog's Invoke gate for a single
// compiled operation and returns its result as a ToolResult. It is the Handler
// installed on the ToolEntry for every operation compiled from the catalog.
//
// A hosted (Portal-embedded) server supplies resolveToken so each per-request
// invocation resolves the authenticated principal's Portal API token and
// threads it through the reserved auth-token input override. catalogops
// service construction honors that override (authTokenFromInput) before the
// config default, so a hosted server authenticates every request as the
// calling user rather than sharing a single config credential. Nil (CLI/local
// path) means no injection — services fall back to their config token.
//
// The credential is preferred from the request context first: the HTTP
// middleware (credentialMiddleware) resolves it once per request, so the
// handler does not re-resolve per tool. On the stdio path there is no
// middleware, so the handler falls back to resolving now via resolveToken.
func compiledHandler(cat catalog.Catalog, name string, resolveToken func(ctx context.Context) (string, error)) model.PinnerToolHandler {
	return func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
		tok := CredentialFromContext(ctx)
		if tok == "" && resolveToken != nil {
			if t, err := resolveToken(ctx); err == nil && t != "" {
				tok = t
				ctx = WithCredential(ctx, tok)
			}
		}
		args := req.Arguments
		if tok != "" {
			if args == nil {
				args = map[string]any{}
			}
			args[catalog.ReservedAuthTokenKey] = tok
		}
		return DispatchCatalogOp(ctx, cat, catalog.ActorModel, name, args, name)
	}
}

// catalogDescriptorToEntry converts a compiler-produced catalog.ToolDescriptor
// into a ToolEntry backed by the operation catalog's Invoke gate. It maps the
// catalog Safety classification onto the MCP entry's ReadOnly/Destructive
// semantics so tool metadata is truthful for the model surface:
//
//	SafetyRead        -> ReadOnly=true
//	SafetyDestructive -> Destructive=true
//	SafetyMutate      -> neither
//
// DirectVisible is left to markCurated (the curated product surface), matching
// how every other tool is promoted to tools/list.
func catalogDescriptorToEntry(d catalog.ToolDescriptor, cat catalog.Catalog, resolveToken func(ctx context.Context) (string, error)) *model.ToolEntry {
	entry := model.ToolEntryFromDescriptor(model.ToolDescriptor{
		Name:         d.Name,
		Title:        d.Title,
		Description:  d.Description,
		Category:     model.ToolCategory(d.Category),
		InputSchema:  d.InputSchema,
		OutputSchema: outputSchemaForCompiled(d.Safety, d.Interaction),
		ReadOnly:     d.Safety == catalog.SafetyRead,
		Destructive:  d.Safety == catalog.SafetyDestructive,
		MCPTargets:   toModelTargets(d.MCPTargets),
		Handler:      compiledHandler(cat, d.Name, resolveToken),
	})
	return entry
}

// toModelTargets maps catalog-native per-profile presentation Targets onto the
// model's ToolTarget variants. Each catalog Target's opaque Require feature
// names are cast to hostenv.Feature and packed into a FeatureSet, so the MCP
// surface can resolve the best-matching variant per request via the detected
// platform profile.
func toModelTargets(targets []catalog.Target) []model.ToolTarget {
	if len(targets) == 0 {
		return nil
	}
	return lo.Map(targets, func(t catalog.Target, _ int) model.ToolTarget {
		require := lo.SliceToMap(t.Require, func(name string) (hostenv.Feature, bool) {
			return hostenv.Feature(name), true
		})
		mt := model.ToolTarget{
			Require:     require,
			Visible:     t.Visible,
			Description: t.Description,
		}
		if t.DescFunc != nil {
			fn := t.DescFunc
			mt.DescFunc = func(p hostenv.PlatformProfile) string {
				return fn(p)
			}
		}
		return mt
	})
}

// outputSchemaForCompiled selects the output schema for a compiled operation
// from its Safety/Interaction classification, so the declared shape matches
// what the operation actually emits for a model actor (the MCP surface runs as
// ActorModel). DispatchCatalogOp maps the catalog gate's refusals onto the
// needs_human hand-off shape, so the effective StructuredContent range is:
//
//   - InteractionHumanOnly / InteractionNeedsHandoff: Catalog.Invoke always
//     refuses a model actor (ErrHumanRequired) before a handler runs, so these
//     tools return only the needs_human hand-off on the model path.
//
//   - SafetyDestructive: Catalog.Invoke refuses a model actor with
//     ErrConfirmRequired on first invocation (manual-confirm hand-off), then —
//     after human confirmation resumes — the op runs and returns the
//     {status:ok,value} success envelope. Both shapes are emitted, so a union
//     (anyOf) schema is declared.
//
//   - Otherwise (SafetyRead / SafetyMutate, agent-safe): the op runs directly
//     and returns only the {status:ok,value} success envelope.
func outputSchemaForCompiled(safety catalog.Safety, interaction catalog.Interaction) json.RawMessage {
	switch {
	case interaction == catalog.InteractionHumanOnly || interaction == catalog.InteractionNeedsHandoff:
		return catalogNeedsHumanOutputSchema
	case safety == catalog.SafetyDestructive:
		return catalogOutputUnionSchema
	default:
		return catalogOutputSchema
	}
}

// populateCatalogSurface compiles every model-visible operation from cat and
// registers it in tc as a ToolEntry whose Handler dispatches through the
// catalog's Invoke gate. It returns the set of compiled operation names so the
// legacy argv tool-handler can route those invocations to the catalog instead
// of the CLI command tree. Names that already exist in tc are replaced, so a
// hybrid deployment (compiled ops for covered domains, legacy tools for the
// rest) stays coherent. Tools are discoverable via search_tools/describe_tool;
// tools/list prominence is decided by markCurated.
func populateCatalogSurface(tc *ToolCatalog, cat catalog.Catalog) (map[string]bool, error) {
	if tc == nil {
		return nil, fmt.Errorf("populateCatalogSurface: nil tool catalog")
	}
	if cat == nil {
		return nil, fmt.Errorf("populateCatalogSurface: nil operation catalog")
	}
	// Resolve the static descriptor against the startup/transport profile so a
	// DescFunc-only MCPTarget fallback (FallbackFunc, e.g. websites_create's
	// DSL-composed description) survives onto the static/non-profile surface
	// instead of collapsing to the short CLI description. Per-request
	// describe_tool/search_tools still re-resolves against the live profile.
	descs, err := catalog.NewMCPCompilerForProfile(startupProfile()).Compile(cat)
	if err != nil {
		return nil, fmt.Errorf("populateCatalogSurface: compile operation catalog: %w", err)
	}
	// A hosted server's per-request credential resolver (on the catalog deps
	// bundle) is captured here so every compiled op authenticates as the
	// calling user; nil on the CLI/local path (config-token fallback).
	var resolveToken func(ctx context.Context) (string, error)
	if tc.CatalogDeps != nil {
		if bundle := tc.CatalogDeps(); bundle != nil && bundle.CredentialResolver != nil {
			resolveToken = bundle.CredentialResolver.TokenForRequest
		}
	}
	compiled := make(map[string]bool, len(descs))
	for _, d := range descs {
		if d.Name == "" {
			continue
		}
		// EnvCLIOnly operations (e.g. the plain SDK-call account credential ops
		// account_update_email / account_update_password) are valid only on the
		// urfave CLI frontend: they pass credentials through the LLM channel
		// and duplicate the OOB tools (account_password_update /
		// account_email_change) that hand off to a browser form. They remain
		// available to the CLI frontend through the operation catalog; only the
		// MCP surface omits them. This is declared on the operation, not by a
		// hard-coded name list here.
		if op, ok := cat.Get(d.Name); ok && op.Environment() == catalog.EnvCLIOnly {
			continue
		}
		tc.Add(catalogDescriptorToEntry(d, cat, resolveToken))
		compiled[d.Name] = true
	}
	return compiled, nil
}
