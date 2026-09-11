package mcp

// Regression tests for the completed per-server surface. Each test pins one
// behavior against OBSERVABLE output — the HTTP boundary, the wire, the
// completed per-server catalog — never an internal flag mirrored back on
// itself.
//
//   - Security (hosted credential fail-closed): TestCredentialMiddlewareFailClosed,
//     TestCompiledHandlerFailClosed, and the end-to-end hosted gate
//     TestHostedNoFallbackCredentialDispatch.
//   - Capability/registration parity on restricted surfaces:
//     TestCapabilitiesReportMatchesEligibleRegistration.
//   - Derived copy from finalized per-server facts:
//     TestGuideStepsAllExistInCompletedSurface,
//     TestOpenAppInventoryMatchesInstalledApps, and
//     TestInstructionsDerivePrimaryFlowsFromCompletedCatalog.
//   - Duplicate curated provisions rejected:
//     TestDuplicateDirectProvisionsRejected.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	opmesh "go.lumeweb.com/opmesh"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	mctf "go.lumeweb.com/mcpplane/transfer"

	hnd "go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	ieo "go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	vaultpkg "go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// --- Security: hosted credential resolution fails closed -------------------

// errResolver is a CredentialResolver that always fails (the hosted IdP/Portal
// backend cannot resolve the caller).
type errResolver struct{ err error }

func (r errResolver) TokenForRequest(context.Context) (string, error) {
	return "", r.err
}

// blankResolver is a CredentialResolver that resolves "successfully" to an
// empty token (an anonymous/unknown principal).
type blankResolver struct{}

func (blankResolver) TokenForRequest(context.Context) (string, error) { return "", nil }

// TestCredentialMiddlewareFailClosed proves the hosted HTTP boundary fails
// closed: with a resolver configured, a resolver error or a blank token yields
// HTTP 401 and the MCP handler is NEVER invoked — even though the deployment
// has a config token its downstream services would otherwise fall back to.
func TestCredentialMiddlewareFailClosed(t *testing.T) {
	const configToken = "shared-config-default-token"

	cases := map[string]struct {
		resolver CredentialResolver
	}{
		"not-authenticated error": {errResolver{err: ErrNotAuthenticated}},
		"resolver failure":        {errResolver{err: context.DeadlineExceeded}},
		"blank token":             {blankResolver{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reached := false
			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				reached = true
			})

			handler := credentialMiddleware(tc.resolver, next)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"%s: a configured resolver that yields no usable token must reject the request", name)
			require.False(t, reached,
				"%s: the request must fail closed — the MCP handler (and with it any config-token-backed dispatch) must never run", name)
		})
	}

	// The non-hosted path (no resolver) is a pass-through: the CLI/local
	// services keep their historical config-token fallback (the middleware
	// injects nothing and rejects nothing).
	reached := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })
	credentialMiddleware(nil, next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	require.True(t, reached, "CLI/local path must pass through unchanged")

	// A successful host resolution still proceeds and stores the credential.
	reached = false
	var got string
	next = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		reached = true
		got = CredentialFromContext(r.Context())
	})
	credentialMiddleware(testCredResolver{tok: configToken}, next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	require.True(t, reached)
	require.Equal(t, configToken, got)
}

// TestCompiledHandlerFailClosed proves the direct dispatch/tool path (and the
// typed-invoke entry.Handler route, which calls compiledHandler directly) also
// fails closed when a per-request resolver IS configured but resolves an
// error or a blank token: the operation is never invoked, so the catalog ops'
// config-token fallback can never execute under default credentials.
func TestCompiledHandlerFailClosed(t *testing.T) {
	cases := map[string]func(context.Context) (string, error){
		"resolver error": func(context.Context) (string, error) { return "", ErrNotAuthenticated },
		"blank token":    func(context.Context) (string, error) { return "", nil },
	}
	for name, resolveToken := range cases {
		t.Run(name, func(t *testing.T) {
			cat := &captureCatalog{}
			h := compiledHandler(cat, "pins_list", resolveToken)
			res, err := h(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
			require.NoError(t, err, "the hand-off is a result, not a transport error")
			require.NotNil(t, res, "the refusal must surface as a credential hand-off")

			require.Nil(t, cat.input,
				"%s: the operation must NEVER be dispatched — the catalog ops' services would fall back to the config default token", name)
			sc, ok := res.StructuredContent.(map[string]any)
			require.True(t, ok, "structured content must carry the needs_human hand-off shape")
			require.Equal(t, model.ReasonCredentialEntry, sc["reason"],
				"%s: no usable resolver credential must produce a credential_entry hand-off", name)

			// Positive control on the same seam: with a working resolver the
			// operation dispatches WITH the injected token.
			okCat := &captureCatalog{}
			okHandler := compiledHandler(okCat, "pins_list", func(context.Context) (string, error) { return "jwt-for-caller", nil })
			_, err = okHandler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
			require.NoError(t, err)
			require.Equal(t, "jwt-for-caller", okCat.input[opmesh.ReservedAuthTokenKey],
				"a resolved credential must still be injected and dispatch must still run")
		})
	}
}

// hostedCredentialErrorBundle is a hosted-style CatalogDepsBundle whose
// per-request resolver ALWAYS fails — the production shape of a
// Portal-embedded assembly whose OAuth backend could not resolve the caller.
func hostedCredentialErrorBundle() func() *CatalogDepsBundle {
	return func() *CatalogDepsBundle {
		b := fullTestBundle()
		b.CredentialResolver = errResolver{err: ErrNotAuthenticated}
		return b
	}
}

// TestHostedNoFallbackCredentialDispatch is the end-to-end no-operation gate:
// a REAL hosted assembly whose resolver fails, driven through its compiled
// tool handler, must return a credential_entry hand-off and never execute an
// operation — the (empty-domain) services behind it can never run under the
// config fallback.
func TestHostedNoFallbackCredentialDispatch(t *testing.T) {
	restoreConstructionGuards(t)

	_, catalog, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: hostedCredentialErrorBundle(),
		Options: []MCPServerOption{
			WithUploadTaskManager(mctf.NewUploadTaskManager(stubUploadExec, 0)),
			WithIPFSDownload(transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil })),
		},
	})
	require.NoError(t, err, "hosted assembly must build")

	entry, ok := catalog.Get("auth_status")
	require.True(t, ok, "auth_status must be registered on the hosted compiled surface")
	res, err := entry.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "the refusal must carry the structured needs_human hand-off shape")
	require.Equal(t, model.ReasonCredentialEntry, sc["reason"],
		"a hosted operation must never dispatch when the resolver yields no identity")
}

// --- Capability/registration parity on restricted surfaces -----------------

// wiredTransferFixtures builds every transfer handler wired, so any advertised
// capability difference must come from the SURFACE, not from missing handlers.
func wiredTransferFixtures(t *testing.T) (customToolDeps, *mcpServerOptions) {
	t.Helper()
	tasks := mctf.NewUploadTaskManager(stubUploadExec, 0)
	curlUpload := mctf.NewHTTPUpload(tasks, ieo.EffectiveRelayMaxBytes(0))
	t.Cleanup(func() { curlUpload.Stop(context.Background()) })
	vaultPut := vaultpkg.VaultPutHandler(stubVaultPutExec)
	deps := customToolDeps{
		curlUpload:   curlUpload,
		vaultUpload:  transfer.NewVaultHTTPUpload(vaultPut, ieo.EffectiveRelayMaxBytes(0)),
		downloadDrop: mctf.NewHTTPDownload(),
		coLocated:    false,
		tunnelOpenAI: false,
	}
	opts := &mcpServerOptions{
		vaultPutHandler: vaultPut,
		ipfsDownload:    transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil }),
		vaultGet:        transfer.VaultGetHandler(func(context.Context, string, string, io.Writer) error { return nil }),
		relayURLUpload:  mctf.RelayURLUploadHandler(stubUploadExec),
		dataURIUpload:   mctf.DataURIUploadHandler(stubUploadExec),
	}
	return deps, opts
}

// TestCapabilitiesReportMatchesEligibleRegistration is the restricted-surface
// parity matrix: for surfaces with fully-wired handlers, the capabilities
// report must exactly equal the tools the assembly ELIGIBLY registered
// (recomputed in the test from the surface truth against the same
// handler-wiring inputs the collection consumed). A wired handler behind a
// disabled surface family must be reported false.
func TestCapabilitiesReportMatchesEligibleRegistration(t *testing.T) {
	surfaces := map[string]DomainScope{
		"full":          FullDomainScope,
		"hosted":        HostedDomainScope,
		"noVault":       hostenv.DomainScope{Account: true, Upload: true, Pins: true, Websites: true, DNS: true, IPNS: true, ENS: true, Operations: true},
		"vaultNoUpload": hostenv.DomainScope{Vault: true, Account: true},
		"accountOnly":   hostenv.DomainScope{Account: true},
	}
	require.False(t, HostedDomainScope.VaultOn(), "the hosted fixture must disable the vault family")

	deps, opts := wiredTransferFixtures(t)
	feats := effectiveFeaturesFor(deps)

	for name, surface := range surfaces {
		avail := computeTransferAvailability(deps, opts, surface, feats)

		// Independent recomputation of the surface-eligible registration from
		// only the surface flags and handler availability (what registration
		// actually gates on), never from computeTransferAvailability itself.
		want := map[string]bool{
			"upload_file":    surface.UploadOn() && uploadFileAvailable(deps.coLocated, false, deps.curlUpload != nil, false, deps.tunnelOpenAI),
			"vault_put_file": surface.VaultOn() && vaultPutFileAvailable(deps.coLocated, false, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI),
			"upload_url":     surface.UploadOn() && opts.relayURLUpload != nil && feats.Has(hostenv.FeatSourceURL),
			"upload_data":    surface.UploadOn() && opts.dataURIUpload != nil && feats.Has(hostenv.FeatSourceData),
			"download_file":  surface.UploadOn() && opts.ipfsDownload != nil,
			"vault_get_file": surface.VaultOn() && opts.vaultGet != nil,
		}
		got := map[string]bool{
			"upload_file":    avail.uploadFile,
			"vault_put_file": avail.vaultPutFile,
			"upload_url":     avail.uploadURL,
			"upload_data":    avail.uploadData,
			"download_file":  avail.downloadFile,
			"vault_get_file": avail.vaultGetFile,
		}
		require.Equal(t, want, got, "%s: availability must equal surface-eligible registration", name)

		// The capabilities REPORT derives exclusively from this calculation:
		// run the descriptor handler and compare its booleans.
		desc := NewCapabilitiesDescriptor(
			deps.coLocated, deps.tunnelOpenAI,
			avail.uploadFile, avail.vaultPutFile,
			avail.downloadFile, avail.vaultGetFile,
			deps.downloadDrop != nil,
			avail.uploadURL, avail.uploadData, avail.uploadData,
			0,
		)
		res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
		require.NoError(t, err)
		report := res.StructuredContent.(CapabilityReport)
		reportByName := map[string]bool{
			"upload_file":    report.UploadFile,
			"vault_put_file": report.VaultPutFile,
			"upload_url":     false,
			"upload_data":    false,
			"download_file":  report.DownloadFile,
			"vault_get_file": report.VaultGetFile,
		}
		for _, tc := range report.UploadTools {
			switch tc {
			case UploadToolURL:
				reportByName["upload_url"] = true
			case UploadToolData:
				reportByName["upload_data"] = true
			}
		}
		require.Equal(t, want, reportByName, "%s: the capabilities report must equal the eligible registration", name)

		// upload_tools lists the REGISTERED relay tools only.
		if !want["upload_url"] {
			require.NotContains(t, report.UploadTools, UploadToolURL, "%s: an unregistered relay must never be advertised", name)
		}
		if !want["upload_data"] {
			require.NotContains(t, report.UploadTools, UploadToolData, "%s: an unregistered relay must never be advertised", name)
		}
		if want["upload_url"] && want["upload_data"] {
			require.Equal(t, []UploadToolCapability{UploadToolFile, UploadToolURL, UploadToolData}, report.UploadTools,
				"%s: the chooser-ordered registered tool list", name)
		}

		// RESTRICTION proof: on any surface without the vault family, no wired
		// vault handler may leak into the report at all.
		if !surface.VaultOn() {
			require.False(t, report.VaultPutFile, "%s: vault_put_file wiring must stay invisible without the vault surface", name)
			require.False(t, report.VaultGetFile, "%s: vault_get_file wiring must stay invisible without the vault surface", name)
		}
		if !surface.UploadOn() {
			require.False(t, report.UploadFile, "%s: upload_file wiring must stay invisible without the upload surface", name)
			require.False(t, report.DownloadFile, "%s: download_file wiring must stay invisible without the upload surface", name)
		}
	}
}

// --- Guide steps exist in the completed surface ----------------------------

// guideWalker collects every tool step the resolved guide names: flow.Steps
// plus every decision-branch step chain (recursively through nested next
// decisions). OOB markers ("<host PUT>") are excluded from the returned set.
func guideWalker(guide AgentGuide) map[string]bool {
	steps := map[string]bool{}
	var walkDecision func(d *GuideDecision)
	walkDecision = func(d *GuideDecision) {
		if d == nil {
			return
		}
		for _, br := range d.Branches {
			for _, s := range br.Steps {
				if !isOOBStepMarker(s) {
					steps[s] = true
				}
			}
			walkDecision(br.Next)
		}
	}
	for _, f := range guide.Flows {
		for _, s := range f.Steps {
			if !isOOBStepMarker(s) {
				steps[s] = true
			}
		}
		walkDecision(f.Decision)
	}
	return steps
}

// completedSurfaceMembers lists every tool a completed assembly actually made
// invocable, derived through the CANONICAL membership predicates rather than a
// hand-rolled union: tool membership comes from catalogToolAvailable (the one
// completed-surface predicate — catalog entries plus DirectCustom direct-only
// registrations), and the installed app launchers come from the finalized
// surface. The test therefore cannot drift from production's membership rules.
func completedSurfaceMembers(catalog *ToolCatalog) map[string]bool {
	available := catalogToolAvailable(catalog)
	members := map[string]bool{}
	for _, e := range catalog.Entries() {
		if available(e.Name) {
			members[e.Name] = true
		}
	}
	for _, n := range catalog.DirectCustom {
		if available(n) {
			members[n] = true
		}
	}
	for _, app := range catalog.FinalizedTooling().InstalledApps() {
		members[app] = true
	}
	return members
}

// resolveGuideForAssembly invokes the completed catalog's agent_guide handler
// with a generic stdio profile and returns the resolved guide.
func resolveGuideForAssembly(t *testing.T, catalog *ToolCatalog) AgentGuide {
	t.Helper()
	entry, ok := catalog.Get("agent_guide")
	require.True(t, ok, "agent_guide must be registered on the completed assembly")
	res, err := entry.Handler(context.Background(), model.ToolRequest{})
	require.NoError(t, err)
	guide, ok := res.StructuredContent.(AgentGuide)
	require.True(t, ok, "the guide handler must return the structured AgentGuide payload")
	return guide
}

// TestGuideStepsAllExistInCompletedSurface pins the derived-guide invariant on
// the REAL production assemblies (full/local and hosted): every tool step the
// resolved agent_guide names — flow chains AND decision branches — exists on
// the completed per-server surface. A hosted/minimal assembly must therefore
// not recommend the absent OOB or transfer tools its surface never registered.
func TestGuideStepsAllExistInCompletedSurface(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		inv := buildInventoryServer(t, nil, nil)
		guide := resolveGuideForAssembly(t, inv.catalog)
		members := completedSurfaceMembers(inv.catalog)
		for step := range guideWalker(guide) {
			require.Truef(t, members[step], "full guide names step %q that the completed surface never registered", step)
		}
	})

	t.Run("hosted", func(t *testing.T) {
		restoreConstructionGuards(t)
		_, catalog, _, err := BuildHostedServer(HostedServerConfig{
			CatalogDeps: hostedCredentialErrorBundle(),
			Options: []MCPServerOption{
				WithUploadTaskManager(mctf.NewUploadTaskManager(stubUploadExec, 0)),
				WithIPFSDownload(transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil })),
			},
		})
		require.NoError(t, err)

		guide := resolveGuideForAssembly(t, catalog)
		members := completedSurfaceMembers(catalog)
		for step := range guideWalker(guide) {
			require.Truef(t, members[step], "hosted guide names step %q that the completed surface never registered", step)
		}

		// The hosted assembly has no OOB coordinator (Portal OAuth handles
		// identity) and no vault surface: the resolved guide must not recommend
		// them, whatever flows/prose exist on the full server.
		require.NotContains(t, members, "auth_sso")
		require.NotContains(t, members, "auth_resume")
		require.False(t, members["vault_put_file"] || members["vault_status"],
			"hosted fixture must have no vault surface members")
	})
}

// --- open_app inventory derives from installed apps -------------------------

// TestOpenAppInventoryMatchesInstalledApps pins that open_app's advertised
// inventory is the FINALIZED installed app state: the resolved description and
// the available list name exactly the app views the assembly installed — a
// restricted (hosted) assembly never advertises the absent vault/OOB apps,
// and the fully wired local assembly lists every app it actually installed.
func TestOpenAppInventoryMatchesInstalledApps(t *testing.T) {
	t.Run("hosted_restricted", func(t *testing.T) {
		restoreConstructionGuards(t)
		_, catalog, _, err := BuildHostedServer(HostedServerConfig{
			CatalogDeps: hostedCredentialErrorBundle(),
			Options: []MCPServerOption{
				WithUploadTaskManager(mctf.NewUploadTaskManager(stubUploadExec, 0)),
				WithIPFSDownload(transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil })),
			},
		})
		require.NoError(t, err)

		names := openAppAppNames(catalog)
		installed := catalog.FinalizedTooling().InstalledApps()
		require.NotEmpty(t, names, "a fully wired hosted assembly installs its reachable app views")
		require.Equal(t, len(installed), len(names),
			"the advertised inventory must be exactly the installed app views (bare names)")
		for _, launcher := range installed {
			require.Contains(t, names, stringsTrimOpenPrefix(launcher), "every installed app must be advertised")
		}
		require.NotContains(t, names, "vault_browser", "hosted assembly must not advertise the absent vault app")
		require.NotContains(t, names, "vault_create", "hosted assembly must not advertise the absent vault app")
		require.NotContains(t, names, "sso_signin", "hosted assembly must not advertise the absent OOB app")

		// The description copy names the same derived inventory.
		desc := openAppDescriptionFor(hostenv.ProfileStdioMCPApps, catalog)
		require.NotContains(t, desc, "vault_browser", "hosted description must not hard-code absent apps")
		require.Contains(t, desc, names[0], "hosted description must name an actually installed app")
	})

	t.Run("full_overrides", func(t *testing.T) {
		inv := buildInventoryServer(t, nil, nil)
		names := openAppAppNames(inv.catalog)
		installed := inv.catalog.FinalizedTooling().InstalledApps()
		require.Equal(t, len(installed), len(names),
			"the advertised inventory must be exactly the installed app views (bare names)")
		for _, launcher := range installed {
			require.Contains(t, names, stringsTrimOpenPrefix(launcher), "every installed app must be advertised")
		}
		// The fully wired local assembly advertises every manager app.
		for _, want := range []string{"vault_browser", "sso_signin", "upload_manager", "vault_manager", "pin_list", "account"} {
			require.Contains(t, names, want, "fully wired assembly must advertise %s", want)
		}
	})
}

// stringsTrimOpenPrefix strips the open_ launcher prefix.
func stringsTrimOpenPrefix(name string) string {
	if len(name) > 5 && name[:5] == "open_" {
		return name[5:]
	}
	return name
}

// --- Instructions derive primary flows from the completed catalog -----------

// TestInstructionsDerivePrimaryFlowsFromCompletedCatalog pins that the
// initialize instructions' primary tool chains and the out-of-band sign-in
// clause derive from the COMPLETED per-server membership, never a hard-coded
// inventory: a hosted-style catalog (no OOB entries, no vault) advertises
// neither the OOB clause nor vault flows; a fully populated catalog carries
// pins_rm exactly as the pins flow defines it and states the OOB pair with its
// truthful visibility split (auth_sso directly listed, auth_resume
// search/progressive-only).
func TestInstructionsDerivePrimaryFlowsFromCompletedCatalog(t *testing.T) {
	// Hosted-style catalog: account surface only, no OOB entries registered.
	hosted := NewToolCatalog()
	hosted.DomainScope = HostedDomainScope
	for _, n := range []string{"auth_status", "pins_add", "pins_list", "pins_status", "pins_rm", "websites_create"} {
		hosted.Add(&model.ToolEntry{Name: n, Description: n + " description"})
	}
	hostedInst := hosted.Instructions()
	require.NotContains(t, hostedInst, "auth_sso", "a hosted-style catalog must not claim the absent OOB tools")
	require.NotContains(t, hostedInst, "auth_resume", "a hosted-style catalog must not claim the absent OOB tools")
	require.NotContains(t, hostedInst, "vault_create", "a hosted-style catalog must not claim vault flows")
	// The auth chain derives from the flow, filtered to registered members.
	require.Contains(t, hostedInst, "- auth:     auth_status", "the auth flow line must survive with only auth_status")

	// Fully populated catalog (the CLI/local compiled surface): primary flows
	// derive with their full chains — auth_status repeats, pins includes rm.
	full := NewToolCatalog()
	for _, n := range []string{
		"auth_status", "auth_sso", "auth_resume",
		"vault_create", "vault_create_resume", "vault_status",
		"vault_restore", "vault_restore_resume",
		"pins_add", "pins_list", "pins_status", "pins_rm",
		"websites_create", "websites_get",
	} {
		full.Add(&model.ToolEntry{Name: n, Description: n + " description", DirectVisible: true})
	}
	fullInst := full.Instructions()
	require.Contains(t, fullInst, "auth_status -> auth_sso -> auth_resume",
		"the auth chain derives from the primary flow (the repeated verify step dedupes to one mention)")
	require.Contains(t, fullInst, "vault_create -> vault_create_resume -> vault_status")
	require.Contains(t, fullInst, "vault_restore -> vault_restore_resume")
	require.Contains(t, fullInst, "pins_add / pins_list / pins_status / pins_rm",
		"the pins line derives from the flow and must include pins_rm exactly as the flow does")
	require.Contains(t, fullInst, "agent-facing out-of-band sign-in tools (auth_sso directly listed; auth_resume progressively discoverable via search)",
		"the registered OOB tools stay named with the truthful visibility split (auth_sso is DirectVisible; auth_resume is search-only)")
	require.NotContains(t, fullInst, "not directly listed",
		"the OOB clause must not claim the directly listed auth_sso lacks a tools/list entry")
}

// --- Duplicate curated provisions -------------------------------------------

// TestDuplicateDirectProvisionsRejected pins the D finding: a duplicate
// curated provision must be REJECTED by the collection phase, never collapsed
// by the ToolCatalog map (which silently replaces same-name entries).
func TestDuplicateDirectProvisionsRejected(t *testing.T) {
	t.Run("two provisions same name", func(t *testing.T) {
		cat := NewToolCatalog()
		reg := newServerExtensionRegistry(nil, cat)
		reg.beforeDirectTools(func(c *ToolCatalog) error { c.Add(&model.ToolEntry{Name: "dup_tool"}); return nil })
		reg.beforeDirectTools(func(c *ToolCatalog) error { c.Add(&model.ToolEntry{Name: "dup_tool"}); return nil })
		err := reg.complete()
		require.ErrorContains(t, err, "duplicate extension name \"dup_tool\"",
			"duplicate beforeDirectTools provisions must fail the plan, not collapse via the catalog map")
	})

	t.Run("provision replacing a compiled entry", func(t *testing.T) {
		cat := NewToolCatalog()
		compiled := &model.ToolEntry{Name: "auth_status", Description: "compiled op"}
		cat.Add(compiled)
		reg := newServerExtensionRegistry(nil, cat)
		reg.beforeDirectTools(func(c *ToolCatalog) error {
			c.Add(&model.ToolEntry{Name: "auth_status", Description: "rogue replace"})
			return nil
		})
		err := reg.complete()
		require.ErrorContains(t, err, "replaced an existing catalog entry",
			"a provision must never silently overwrite a compiled operation")
		// The rejected plan never reaches the finished-surface record: the
		// provision's in-memory mutation is observable, but the plan fails so
		// no collection result or materialization can carry it to a server.
		require.Nil(t, cat.FinalizedTooling(), "a rejected plan must never record a finalized surface")
	})

	t.Run("distinct provisions pass", func(t *testing.T) {
		cat := NewToolCatalog()
		reg := newServerExtensionRegistry(nil, cat)
		reg.beforeDirectTools(func(c *ToolCatalog) error { c.Add(&model.ToolEntry{Name: "wizard_a"}); return nil })
		reg.beforeDirectTools(func(c *ToolCatalog) error { c.Add(&model.ToolEntry{Name: "dev_a", DirectVisible: true}); return nil })
		require.NoError(t, reg.complete(), "distinct provisions must still collect cleanly")
	})
}

// --- Hosted no-OOB/no-transfer assembly: derived copy carries no absent names

// hostedAbsentToolNames is the denylist for a hosted assembly built WITHOUT
// transfer wiring: none of these names (OOB sign-in pair + app helper,
// CLI auth mutations, every vault tool and flow name, every byte-transfer
// tool) may appear ANYWHERE in the derived agent guide or initialize
// instructions — flow steps, summary, rules, detail text, or nested decision
// branches. All of them are absent from this assembly's surface, so naming
// one would recommend an uncallable action.
func hostedAbsentToolNames() []string {
	return []string{
		// OOB sign-in / CLI auth-mutation surface (absent on hosted)
		"auth_sso", "auth_resume", "auth_sso_revoke", "auth_sso_status",
		"auth_login", "auth_logout",
		// Sia vault surface (absent on hosted)
		"vault_create", "vault_restore", "vault_status", "vault_create_resume",
		"vault_restore_resume", "vault_put_file", "vault_get_file",
		"vault_share", "vault_sync", "vault_verify", "vault_flush",
		"vault_flush_status", "vault_stat", "vault_send", "vault_profiles",
		"vault_share_accept", "vault_ls",
		// byte-transfer family (no transfer wiring in this fixture)
		"upload_file", "upload_url", "upload_data", "upload_status",
		"download_file",
	}
}

// hostedNoTransferAssembly builds a real hosted assembly (BuildHostedServer)
// whose Options carry no transfer wiring and whose catalog deps carry the
// hosted (erroring) resolver: no upload/download/vault tool family registers.
func hostedNoTransferAssembly(t *testing.T) (*sdk.Server, *ToolCatalog) {
	t.Helper()
	restoreConstructionGuards(t)
	srv, catalog, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: hostedCredentialErrorBundle(),
	})
	require.NoError(t, err, "hosted no-transfer assembly must build")
	return srv, catalog
}

// TestHostedNoTransferGuideAndInstructionsNameNoAbsentTools is the derived-copy
// regression: a hosted/minimal assembly whose guide and instructions never
// mention a tool it did not register — not in step chains, not in the summary,
// not in rules, not in flow detail prose, and not in nested decision branches
// (the whole resolved guide is JSON-marshaled so every text field is covered).
func TestHostedNoTransferGuideAndInstructionsNameNoAbsentTools(t *testing.T) {
	srv, catalog := hostedNoTransferAssembly(t)

	guide := resolveGuideForAssembly(t, catalog)
	wire, err := json.Marshal(guide)
	require.NoError(t, err)
	guideText := string(wire)
	sess := connectOfficialClient(t, srv)
	instructions := sess.InitializeResult().Instructions

	for _, name := range hostedAbsentToolNames() {
		require.NotContainsf(t, guideText, name,
			"hosted guide must never name the unavailable tool %q (steps/summary/rules/detail/branches)", name)
		require.NotContainsf(t, instructions, name,
			"hosted instructions must never name the unavailable tool %q", name)
	}

	// The flows the hosted assembly CAN drive survive with their registered
	// content: auth (status only), pins, website publishing/update.
	names := map[string]bool{}
	for _, f := range guide.Flows {
		names[f.Name] = true
	}
	require.Contains(t, names, "auth", "hosted auth flow must survive (auth_status registered)")
	require.Contains(t, names, "pins", "hosted pins flow must survive")
	require.Contains(t, names, "update_website", "hosted update flow must survive")
	require.NotContains(t, names, "upload", "hosted no-transfer upload flow must be dropped")
	require.NotContains(t, names, "download", "hosted no-transfer download flow must be dropped")
	require.NotContains(t, names, "vault_upload", "hosted vault flow must be dropped")

	// The auth chain in the instructions reduces to auth_status only.
	require.Contains(t, instructions, "- auth:     auth_status",
		"the hosted auth flow line must keep only the registered step")
	require.Contains(t, instructions, "pins_add / pins_list / pins_status / pins_rm",
		"the hosted pins line must keep its registered chain including pins_rm")

	// The declared count equals the final indexed catalog (no post-surface
	// exception for open_app — it is indexed during collection).
	require.Equal(t, catalog.Len(), instructionToolCount(t, instructions),
		"hosted instruction count must equal the final indexed catalog")
}

// TestHostedAssemblyRejectsStaleInstalledLauncher Pins the open_app contract at
// the ASSEMBLY level: a full local assembly first installs its app views into
// the process-global registry; the hosted assembly assembled afterwards shares
// that process — and must still resolve/advertise ONLY its own installed apps.
func TestHostedAssemblyRejectsStaleInstalledLauncher(t *testing.T) {
	full := buildInventoryServer(t, nil, nil) // pollutes the global app registry with vault/OOB apps

	restoreConstructionGuards(t)
	_, catalog := hostedNoTransferAssembly(t)

	hostedRecords := catalog.FinalizedTooling().AppRecords()
	recordByLauncher := map[string]InstalledAppView{}
	for _, r := range hostedRecords {
		recordByLauncher[r.Launcher] = r
	}
	require.NotContains(t, recordByLauncher, "open_vault_browser",
		"the hosted assembly must not capture an app view it never installed")
	require.NotNil(t, catalog.FinalizedTooling(), "hosted assembly must finalize its surface")
	require.True(t, len(hostedRecords) > 0, "hosted assembly installs its reachable app views (pin list)")

	// A stale full launcher (installed by the earlier full assembly) must not
	// resolve on the hosted server: handler-level, not just resolver-level.
	entry, ok := catalog.Get("open_app")
	require.True(t, ok, "hosted assembly must keep open_app catalog-indexed")
	res, err := entry.Handler(context.Background(), model.ToolRequest{
		Arguments: map[string]any{"app": "open_vault_browser"},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "open_vault_browser (never installed here) must be rejected")

	// The hosted open_app inventory/description name only the installed apps.
	bare := openAppAppNames(catalog)
	require.Contains(t, bare, "pin_list")
	require.NotContains(t, bare, "vault_browser")
	require.NotContains(t, bare, "sso_signin")
	desc := openAppDescriptionFor(hostenv.ProfileStdioMCPApps, catalog)
	require.Contains(t, desc, "pin_list")
	require.NotContains(t, desc, "vault_browser")

	// The full assembly's own surface is unaffected (same process, per-server
	// facts): its launcher still resolves there.
	name, uri, ok := resolveOpenApp(full.catalog, "open_vault_browser")
	require.True(t, ok, "the full assembly keeps its own installed app")
	require.Equal(t, "open_vault_browser", name)
	require.NotEmpty(t, uri)
	_ = full
}

// --- Guide branch prerequisite semantics -------------------------------------

// TestGuideBranchPrerequisiteDrop pins the branch filter: a decision branch
// whose ENTIRE tool chain is absent from the completed surface is dropped even
// when it carries a Next decision — a branch's own steps are the prerequisite
// of everything nested below it, and its detail text would still name absent
// tools. Sibling branches whose tools exist survive.
func TestGuideBranchPrerequisiteDrop(t *testing.T) {
	available := func(name string) bool { return name == "nested_real" || name == "sibling_real" }
	avail := &guideAvailability{toolAvailable: available}

	guide := toolforge.Guide().
		Flow(toolforge.Flow("upload", "Upload").
			Steps("capabilities").
			Decision(toolforge.Decision("outer?",
				toolforge.Branch("ghost branch").
					Steps("ghost_tool").
					Detail(toolforge.Static("ghost_tool prose")).
					Next(toolforge.Decision("nested?",
						toolforge.Branch("nested").Steps("nested_real"))),
				toolforge.Branch("sibling branch").
					Steps("sibling_real"),
			))).
		Resolve(hostenv.ProfileStdioGeneric)
	flow := guide.Flows[0]

	avail.filterGuideSteps(&flow)

	require.NotNil(t, flow.Decision)
	require.Len(t, flow.Decision.Branches, 1, "only the branch with present tools survives")
	require.Equal(t, "sibling branch", flow.Decision.Branches[0].When,
		"the ghost branch (steps absent, but carrying Next) must be dropped")
	require.Equal(t, []string{"sibling_real"}, flow.Decision.Branches[0].Steps)
}

// TestGuideFlowRequiredStepDrop pins the flow-prerequisite filter: a flow whose
// spec-declared REQUIRED step never registered on the completed surface is
// dropped whole — its prose can never survive to recommend the missing
// essential tool (the download flow names download_file in its detail; with
// that tool absent, the flow has nothing to say).
func TestGuideFlowRequiredStepDrop(t *testing.T) {
	available := func(name string) bool { return name != "download_file" }
	avail := &guideAvailability{toolAvailable: available}

	guide := buildAgentGuideFor(&hostenv.ProfileStdioGeneric, FullDomainScope, false, avail)
	for _, f := range guide.Flows {
		if f.Name == "download" {
			t.Fatalf("the download flow must be dropped when its required step download_file never registered")
		}
	}
	// A flow whose required steps ARE present keeps its slot.
	found := false
	for _, f := range guide.Flows {
		if f.Name == "pins" {
			found = true
		}
	}
	require.True(t, found, "flows with present prerequisites survive the required filter")
}

// TestAgentGuideDescriptionMatchesFlowInventory pins that the static
// agent_guide description's flow enumeration is DERIVED from the single
// flow-spec table: the description carries exactly the table's flows, in table
// order, with none missing and none invented.
func TestAgentGuideDescriptionMatchesFlowInventory(t *testing.T) {
	names := guideFlowNames()
	require.Equal(t, guideFlowSpecs, guideFlowSpecs) // table intact (compile anchor)
	wantList := "(" + strings.Join(names, ", ") + ")"
	require.Contains(t, agentGuideDescription, wantList,
		"the static description must enumerate exactly the flow-spec table, in declaration order")
	for _, n := range names {
		require.Containsf(t, agentGuideDescription, n,
			"the static description must name flow %q", n)
	}
}

// compile-time anchor: the legacy OOB/seed fixtures stay exercised through the
// full assembly suites; this file only needs the registry shim.
var _ = oobpkg.NewSeedDrop
var _ = hnd.NewHandoffRegistry
