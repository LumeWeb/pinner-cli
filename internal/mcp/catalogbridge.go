package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/samber/lo"
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner/catalogmcp"
	"go.lumeweb.com/pinner/catalogmeta"
)

// startupProfile returns the static profile for the startup transport, used to
// resolve DescFunc-only MCPTarget fallbacks when the compiled surface is built
// (no per-request profile is available there). The startup transport is
// derived from the same flags that SetTransportFlags records (co-located stdio,
// OpenAI tunnel, or plain HTTP).
func startupProfile() hostenv.PlatformProfile {
	p := hostenv.ProfileForTransport(transfer.UploadFileTransport(transportFlagsVar.coLocated, transportFlagsVar.tunnelOpenAI))
	// The server surface and deployment mode are construction-time properties
	// recorded by buildCatalog; carry them on the startup profile so
	// profile-aware tool description/schema resolution (which reads the
	// profile's surface) agrees with what was actually registered.
	p.DomainScope = activeDomainScope()
	p.Hosted = activeHosted()
	return p
}

// This file is the bridge between the operation catalog (the compiler-backed
// source of truth for MCP tool descriptions/schemas) and the legacy ToolCatalog
// that drives the official MCP server's progressive-disclosure meta-tools
// (search_tools, describe_tool, invoke_read_tool/invoke_write_tool/invoke_destructive_tool).
//
// The compiled catalog yields ToolDescriptors whose Description/InputSchema
// come from the module's catalogmcp compiler (MCP-target fallback resolution
// plus typed arg metadata with catalogmeta AgentHelp re-application), so CLI
// help prose, global flag bags, and empty "required" arrays never leak into
// the model surface. Each compiled operation is surfaced as a ToolEntry whose
// Handler routes through opmesh.Catalog.Invoke the dispatch gate so
// Interaction, Visibility, Safety, and required-arg enforcement hold.

// compileProfileFor adapts the local hostenv profile into the module compiler's
// feature-carrier contract. The module's description DSL (catalogmcp) gates
// segments on a mcpforge.FeatureSet carried by an MCPProfile; it cannot know
// hostenv. ProfileFromHas probes every feature the module's descriptions gate
// on, so adapting through it cannot drop a segment — exactly the lossless
// bridge the module documents for pinner-cli's Has-style PlatformProfile.
func compileProfileFor(prof hostenv.PlatformProfile) catalogmcp.MCPProfile {
	return catalogmcp.ProfileFromHas(func(f string) bool {
		return prof.Has(hostenv.Feature(f))
	})
}

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
//
// FAIL CLOSED on the hosted path: when a per-request resolver is configured
// (the hosted assembly seeds the bundle's CredentialResolver) a resolver
// error or a blank resolved token returns a structured credential_entry
// needs_human hand-off and the operation is NEVER dispatched. The catalog
// ops' services fall back to the config default token when the reserved
// auth-token override is absent, so dispatching without a resolved identity
// would execute a request under the shared deployment credential. The
// middleware already fails closed at the HTTP boundary; this gate defends
// the direct-dispatch/tool path (and typed-invoke entry.Handler routes, which
// call this handler directly) the same way. The CLI/local path (nil
// resolver) is unchanged: an empty context credential simply means no
// override is injected and services use their config token.
func compiledHandler(cat opmesh.Catalog, name string, resolveToken func(ctx context.Context) (string, error)) model.ToolHandler {
	return func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
		tok := CredentialFromContext(ctx)
		if tok == "" && resolveToken != nil {
			t, err := resolveToken(ctx)
			if err != nil || t == "" {
				return model.NeedsHumanResult(model.NeedsHuman{
					Reason: model.ReasonCredentialEntry,
					Detail: name + " requires an authenticated request, but no Portal credential could be resolved for the current identity. Re-authenticate the MCP session (a hosted deployment resolves identity per request via Portal OAuth). No operation was executed under default credentials.",
				}), nil
			}
			tok = t
			ctx = WithCredential(ctx, tok)
		}
		args := req.Arguments
		if tok != "" {
			if args == nil {
				args = map[string]any{}
			}
			args[opmesh.ReservedAuthTokenKey] = tok
		}
		return DispatchCatalogOp(ctx, cat, opmesh.ActorModel, name, args, name)
	}
}

// catalogDescriptorToEntry converts a compiler-produced opmesh.ToolDescriptor
// into a ToolEntry backed by the operation catalog's Invoke gate. It maps the
// catalog Safety classification onto the MCP entry's ReadOnly/Destructive
// semantics so tool metadata is truthful for the model surface:
//
//	SafetyRead        -> ReadOnly=true
//	SafetyDestructive -> Destructive=true
//	SafetyMutate      -> neither
//
// The open-world hint derives from Safety: mutating/destructive operations
// change publicly visible internet state (pins, websites, DNS), while reads
// change nothing external, so openWorldHint stays false for any SafetyRead
// operation. There is deliberately NO per-tool hint override: the entry
// metadata must match the operation's own classification so the typed invoke
// dispatchers route it into the dispatcher its real behavior demands. In
// particular auth_status is a pure read (its underlying operation reads
// session state from the configured service; the agent can never trigger an
// out-of-band message by checking status), so it keeps the SafetyRead ->
// readOnlyHint=true mapping and is classified into invoke_read_tool.
//
// DirectVisible is left to stampDirectTools (the direct product surface), matching
// how every other tool is promoted to tools/list.
func catalogDescriptorToEntry(d opmesh.ToolDescriptor, cat opmesh.Catalog, resolveToken func(ctx context.Context) (string, error)) *model.ToolEntry {
	readOnly := d.Safety == opmesh.SafetyRead
	destructive := d.Safety == opmesh.SafetyDestructive
	openWorld := !readOnly
	entry := model.ToolEntryFromDescriptor(model.ToolDescriptor{
		Name:          d.Name,
		Title:         d.Title,
		Description:   d.Description,
		Category:      model.ToolCategory(d.Category),
		InputSchema:   d.InputSchema,
		OutputSchema:  outputSchemaForCompiled(d.Safety, d.Interaction),
		ReadOnly:      readOnly,
		Destructive:   destructive,
		OpenWorldHint: openWorld,
		MCPTargets:    toModelTargets(catalogmcp.TargetsOf(d.Name)),
		Handler:       compiledHandler(cat, d.Name, resolveToken),
	})
	// Propagate the operation's Interaction classification onto the entry so
	// the search/describe/invoke surface and the safety carve-out (agentDirectSafe)
	// agree with the operation catalog. ToolEntryFromDescriptor stamps a default
	// model.InteractionAgentSafe, but a human-only operation (prompts
	// interactively, no agent-safe form) must be classified model.InteractionInteractive
	// so agents are steered away: invoke dispatchers hand off and search_tools
	// hides it. Without this, the operator-declared Interaction would be silently
	// dropped and the interaction-based safety branch would be unreachable for
	// the compiled surface.
	entry.Interaction = modelInteractionFromOpmesh(d.Interaction)
	return entry
}

// modelInteractionFromOpmesh maps the opmesh-operation Interaction class onto
// the model's interaction vocabulary that the progressive search/describe/
// invoke surface steers on. A human-only operation becomes
// model.InteractionInteractive (steer agents away): the invoke dispatchers
// return a needs_human hand-off and search_tools hides it, matching the
// operation catalog's own refusal of a model actor for such ops. Everything
// else — agent-safe and the out-of-band needs-handoff (external browser/device,
// split into two calls) which the invoke gate serves as a needs_human redirect —
// stays model.InteractionAgentSafe, the default the model converter stamps.
func modelInteractionFromOpmesh(i opmesh.Interaction) model.Interaction {
	if i == opmesh.InteractionHumanOnly {
		return model.InteractionInteractive
	}
	return model.InteractionAgentSafe
}

// toModelTargets maps the module's MCP boundary presentation Targets onto the
// model's ToolTarget variants. Each catalogmcp Target's opaque Require feature
// names are cast to hostenv.Feature and packed into a FeatureSet, so the MCP
// surface can resolve the best-matching variant per request via the detected
// platform profile.
//
// DescFunc resolvers are adapted across the module boundary: the module DSL
// resolves features from a FeatureCarrier (MCPProfile), while the model layer
// passes the live hostenv.PlatformProfile. The adapter re-wraps the request
// profile via compileProfileFor so feature-gated description segments resolve
// losslessly at per-request time, not just at compile time.
func toModelTargets(targets []catalogmcp.Target) []model.ToolTarget {
	if len(targets) == 0 {
		return nil
	}
	return lo.Map(targets, func(t catalogmcp.Target, _ int) model.ToolTarget {
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
			mt.DescFunc = func(sp model.Profile) string {
				// Reconstruct the CLI profile view (DomainScope zero — feature/
				// transport gating only) before re-wrapping for the module DSL.
				return fn(compileProfileFor(hostenv.FromShared(sp)))
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
func outputSchemaForCompiled(safety opmesh.Safety, interaction opmesh.Interaction) json.RawMessage {
	switch {
	case interaction == opmesh.InteractionHumanOnly || interaction == opmesh.InteractionNeedsHandoff:
		return catalogNeedsHumanOutputSchema
	case safety == opmesh.SafetyDestructive:
		return catalogOutputUnionSchema
	default:
		return catalogOutputSchema
	}
}

// populateCatalogTools compiles every model-visible operation from cat and
// registers it in tc as a ToolEntry whose Handler dispatches through the
// catalog's Invoke gate. It returns the set of compiled operation names so the
// legacy argv tool-handler can route those invocations to the catalog instead
// of the CLI command tree. Names that already exist in tc are replaced, so a
// hybrid deployment (compiled ops for covered domains, legacy tools for the
// rest) stays coherent. Tools are discoverable via search_tools/describe_tool;
// tools/list prominence is decided by stampDirectTools.
func populateCatalogTools(tc *ToolCatalog, cat opmesh.Catalog) (map[string]bool, error) {
	if tc == nil {
		return nil, fmt.Errorf("populateCatalogTools: nil tool catalog")
	}
	if cat == nil {
		return nil, fmt.Errorf("populateCatalogTools: nil operation catalog")
	}
	// Compile against the module's MCP boundary compiler, adapted to the
	// startup/transport profile. The compiler resolves FallbackFunc targets
	// (e.g. websites_create's DSL-composed description, feature-gated per
	// profile) so the static/non-profile surface does not collapse to the
	// short base description instead. Per-request describe_tool/search_tools
	// still re-resolves against the live profile.
	descs, err := catalogmcp.NewCompilerForProfile(compileProfileFor(startupProfile())).Compile(cat)
	if err != nil {
		return nil, fmt.Errorf("populateCatalogTools: compile operation catalog: %w", err)
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
		// MCP surface omits them. The carve-out is frontend metadata
		// (catalogmeta.EnvironmentOf, keyed by the stable operation ID): the
		// opmesh core model carries no Environment field.
		if op, ok := cat.Get(d.Name); ok && catalogmeta.EnvironmentOf(op.Name()) == catalogmeta.EnvCLIOnly {
			continue
		}
		tc.Add(catalogDescriptorToEntry(d, cat, resolveToken))
		compiled[d.Name] = true
	}
	return compiled, nil
}
