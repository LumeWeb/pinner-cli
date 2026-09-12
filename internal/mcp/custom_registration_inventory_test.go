package mcp

// Production-path MCP surface characterization.
//
// These tests drive the REAL assembly pipeline — BuildServer (buildCatalog ->
// populateCatalogTools -> stampDirectTools -> OfficialServerFromCatalog) and the
// hosted BuildHostedServer constructor — followed by the production
// registerCustomTools pipeline (custom_tools_register.go phases: index, app,
// curated, direct surface, post). They intentionally do NOT hand-construct
// mini-catalogs or re-declare the registration order, because the existing
// suites either build their own catalogs around the materialization seams
// (materialization_test.go) or mirror registerCustomTools' sequence by hand
// (upload_ipfs_app_test.go, upload_vault_app_test.go); neither characterizes
// the inventory the actual registration function produces.
//
// The dependency bundles below are fixture-level: real out-of-band and
// presigned-PUT coordinators built by their production constructors, stub
// executors. What is asserted is what the production assembly actually
// registered — the wire tools/list read through a live client session, the
// catalog's searchable/hidden membership through its real Search API, the
// per-server card and instructions — not internal booleans or constants
// compared against themselves.
//
// Not drivable here: the MCPCommand transport closure itself (stdio/HTTP
// flag parsing, the config-manager wizard factory, the serveHTTP mux and
// tunnel providers) and the hosted NewCatalogDeps -> New -> HTTP path. Both
// need the local/hosted composition harness and are
// covered here only by the closest production constructors (BuildServer +
// registerCustomTools / BuildHostedServer).

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/session"
	mctf "go.lumeweb.com/mcpplane/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	ieo "go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	downloadpkg "go.lumeweb.com/pinner-cli/internal/mcp/download"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
	uploadpkg "go.lumeweb.com/pinner-cli/internal/mcp/upload"
	vaultpkg "go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// stubUploadExec is a minimal-but-real UploadHandler (the shared shape behind
// the relay-URL and data-URI upload executors) so upload wiring mirrors
// production without a network.
func stubUploadExec(_ context.Context, r io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
	_, _ = io.Copy(io.Discard, r)
	return map[string]any{"cid": "QmStub"}, nil
}

// stubVaultPutExec is a minimal VaultPutHandler for the presigned vault
// upload coordinator.
func stubVaultPutExec(_ context.Context, _ io.Reader, _ int64, _ string, _ map[string]any) (any, error) {
	return map[string]any{"status": "staged"}, nil
}

// fullLocalDeps builds the customToolDeps bundle the CLI startup server uses
// when every optional backend is wired: OOB sign-in, presigned HTTP upload
// (mint) and vault-upload coordinators, IPFS download + vault-get executors,
// relay/data upload executors, the Create-a-Pin provider, wizard tools, and
// prompts. The coordinators use the same production constructors the CLI
// Action calls (mctf.NewUploadTaskManager / NewHTTPUpload / NewHTTPDownload,
// transfer.NewVaultHTTPUpload, oobpkg.NewOOBCreate / NewOOBRestore /
// NewSeedDrop, auth.NewOOBAccountChange), not test doubles of them (only the
// underlying runner/executors are stubs).
func fullLocalDeps(t *testing.T, srv *sdk.Server, catalog *ToolCatalog) customToolDeps {
	t.Helper()

	tasks := mctf.NewUploadTaskManager(stubUploadExec, 0)
	curlUpload := mctf.NewHTTPUpload(tasks, ieo.EffectiveRelayMaxBytes(0))
	t.Cleanup(func() { curlUpload.Stop(context.Background()) })

	vaultPut := vaultpkg.VaultPutHandler(stubVaultPutExec)
	vaultUpload := transfer.NewVaultHTTPUpload(vaultPut, ieo.EffectiveRelayMaxBytes(0))

	// Real out-of-band coordinators exactly as the CLI Action builds them.
	oobCreate, _, _ := buildCreateServer()
	oobRestore := oobpkg.NewOOBRestore(&fakeRestoreRunner{profile: "default"}, oobpkg.DefaultRestoreTTL)

	opts := &mcpServerOptions{
		uploadTasks:     tasks,
		vaultPutHandler: vaultPut,
		ipfsDownload:    transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil }),
		vaultGet:        transfer.VaultGetHandler(func(context.Context, string, string, io.Writer) error { return nil }),
		relayURLUpload:  mctf.RelayURLUploadHandler(stubUploadExec),
		dataURIUpload:   mctf.DataURIUploadHandler(stubUploadExec),
		pinnerPins: func() (apps.PinningProvider, error) {
			return &fakePins{status: "pinned"}, nil
		},
		prompts: true,
	}

	return customToolDeps{
		srv:              srv,
		catalog:          catalog,
		store:            session.NewSessionStore(),
		oob:              newHubOOBForTest(t),
		authHandles:      session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions),
		handoffReg:       handoff.NewHandoffRegistry(),
		seedDrop:         oobpkg.NewSeedDrop(oobpkg.DefaultSeedDropTTL),
		oobRestore:       oobRestore,
		oobCreate:        oobCreate,
		accountOOB:       auth.NewOOBAccountChange(stubAuthService{}, auth.DefaultAccountChangeTTL),
		accountWebAppURL: "https://account.example.com",
		curlUpload:       curlUpload,
		vaultUpload:      vaultUpload,
		opts:             opts,
		coLocated:        false,
		tunnelOpenAI:     false,
		hasWizard:        true,
	}
}

// assembledServer holds everything derivable from one production assembly at
// construction time: the live client session (whose initialize result froze
// the server's instructions), the per-server catalog, the per-server card
// captured the way MCPCommand captures the startup card, and the first
// materialized tools/list snapshot.
type assembledServer struct {
	session      *mcp.ClientSession
	catalog      *ToolCatalog
	card         *ServerCard
	wireNames    map[string]bool
	instructions string
}

// buildInventoryServer assembles a full-local server through the production
// BuildServer pipeline (with the optional listing policy from policy and the
// optional detected host profile) and the REAL registerCustomTools pipeline,
// then captures a live client session, the per-server catalog, the per-server
// card, and the wire tools/list. This mirrors the startup-server section of
// MCPCommand's Action minus transport/flag wiring (the part that needs the
// local composition harness; see the "not drivable" note above).
func buildInventoryServer(t *testing.T, policy *ListingPolicy, hostProfile *hostenv.PlatformProfile) *assembledServer {
	t.Helper()
	restoreConstructionGuards(t)

	srv, catalog, err := BuildServer(ServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Policy:      policy,
		SeedDrop:    oobpkg.NewSeedDrop(oobpkg.DefaultSeedDropTTL),
		OOBRestore:  oobpkg.NewOOBRestore(&fakeRestoreRunner{profile: "default"}, oobpkg.DefaultRestoreTTL),
		OOBCreate: func() *oobpkg.OOBCreate {
			c, _, _ := buildCreateServer()
			return c
		}(),
		HandoffReg:  handoff.NewHandoffRegistry(),
		AuthHandles: session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions),
		// The production one-pass path: extensions are COLLECTED before the
		// official server exists (so the initialize instructions and card
		// derive from the completed catalog) and materialized by BuildServer
		// in a single pass after construction.
		CollectExtensions: func(catalog *ToolCatalog) (*MaterializationPlan, error) {
			deps := fullLocalDeps(t, nil, catalog)
			deps.hostProfile = hostProfile
			return collectServerExtensions(deps)
		},
	})
	require.NoError(t, err, "production BuildServer + registerCustomTools assembly must succeed")

	// Capture the per-server card at construction time exactly as MCPCommand
	// does (NewServerCard(startupCat)), then connect a client so the
	// initialize result froze this server's instructions. tools/list is
	// materialized once, right after assembly.
	inv := &assembledServer{catalog: catalog}
	inv.card = NewServerCard(catalog)
	inv.session = connectOfficialClient(t, srv)
	inv.instructions = inv.session.InitializeResult().Instructions
	require.NotEmpty(t, inv.instructions, "assembled server must carry instructions")
	inv.wireNames, _ = sessionToolNames(t, inv.session)
	return inv
}

// sessionToolNames lists the direct tools on the wire for an existing client
// session, asserting the unique-name invariant every time.
func sessionToolNames(t *testing.T, cs *mcp.ClientSession) (map[string]bool, []*mcp.Tool) {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err, "ListTools")
	uniq := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		require.NotContainsf(t, uniq, tool.Name,
			"tools/list must not contain duplicate tool name %q", tool.Name)
		uniq[tool.Name] = true
	}
	return uniq, res.Tools
}

// searchVisible reports whether the catalog's real search API can surface
// name with a plain single-keyword query (the production search_tools
// behavior, not a flag mirror).
func searchVisible(t *testing.T, catalog *ToolCatalog, name string) bool {
	t.Helper()
	res := catalog.SearchFor(name, "", 0, nil)
	for _, s := range res {
		if s.Name == name {
			return true
		}
	}
	return false
}

// expectedDirectCustomNames enumerates the custom tools the fully-wired
// bundle's production registration makes DIRECTLY visible under the
// progressive strategy. Note the production reality that establishes this
// baseline: only auth_sso carries Descriptor.DirectVisible among the OOB
// sign-in/account/resume tools — the resume/revoke/account members are
// catalog-indexed (searchable/hand-off only) and stay off tools/list.
func expectedDirectCustomNames() []string {
	return []string{
		"capabilities", "agent_guide",
		"upload_file", "download_file", "vault_get_file", "vault_put_file",
		"auth_sso",
		// App-view helper descriptors (production behavior baseline): the app
		// installers register these app-only helpers on the server (not the
		// catalog), so they DO surface on the progressive tools/list.
		"pin_status",           // Create-a-Pin app helper
		"auth_sso_status",      // Sign-in OOB app helper
		"ipfs_upload_submit",   // IPFS upload app helper
		"ipfs_upload_status",   // IPFS upload app poller
		"vault_upload_submit",  // Vault upload app helper
		"vault_create_status",  // vault create OOB app poller
		"vault_restore_status", // vault restore OOB app poller
	}
}

// expectedSearchOnlyCustomNames enumerates the custom tools the fully-wired
// bundle's production registration indexes into the catalog but keeps OFF the
// progressive tools/list (reachable via search_tools / describe_tool /
// invoke dispatchers only). Includes the OOB resume/revoke/account tools, the
// async upload management tools, and the onboarding-only pins op.
func expectedSearchOnlyCustomNames() []string {
	return []string{
		"auth_resume", "auth_sso_revoke",
		"account_password_update", "account_password_reset", "account_email_change",
		"vault_create_resume", "vault_restore_resume",
		"upload_status", "upload_cancel", "upload_list",
		"pins_add",
	}
}

// expectedProgressiveDirectSet derives the exact progressive tools/list set
// for the full-local inventory assembly from the live curated surface + meta
// constants + the custom tools the wired bundle registers directly. It never
// reads the production direct-registration code paths, so comparing it
// against the wire asserts actual outcomes rather than mirroring
// implementation.
func expectedProgressiveDirectSet(t *testing.T) map[string]bool {
	t.Helper()
	want := map[string]bool{}
	for _, n := range directToolNamesFor(FullDomainScope) {
		want[n] = true
	}
	for _, n := range metaToolNames {
		want[n] = true
	}
	for _, n := range expectedDirectCustomNames() {
		want[n] = true
	}
	return want
}

// openLauncherToolNames lists every per-app open_* launcher the production
// pipeline registers for the fully-wired full-local bundle.
func openLauncherToolNames(t *testing.T) []string {
	t.Helper()
	return []string{
		apps.OpenPinCreatorToolName,
		downloadpkg.OpenPinListToolName,
		uploadpkg.OpenUploadManagerToolName,
		uploadpkg.OpenVaultManagerToolName,
		auth.OpenSSOSigninToolName,
		auth.OpenAccountToolName,
		auth.OpenAccountPasswordToolName,
		auth.OpenAccountEmailToolName,
		vaultpkg.OpenVaultCreateToolName,
		vaultpkg.OpenVaultRestoreToolName,
		vaultpkg.OpenVaultBrowserToolName,
		downloadpkg.OpenDownloadManagerToolName,
		downloadpkg.OpenVaultDownloadManagerToolName,
	}
}

// requireRegisterableDirectCustom asserts a custom tool is on the wire.
func requireWireDirect(t *testing.T, wire map[string]bool, names ...string) {
	t.Helper()
	for _, n := range names {
		require.Truef(t, wire[n], "custom direct tool %q must be listed on tools/list", n)
	}
}

// TestCustomRegistrationInventoryFullLocalProductionPipeline is the complete
// custom-registration inventory characterization for the representative
// full-local configuration: every backend wired, generic agent-only startup
// HTTP host, progressive listing. It records the direct / searchable / hidden
// membership the production registration produced.
func TestCustomRegistrationInventoryFullLocalProductionPipeline(t *testing.T) {
	inv := buildInventoryServer(t, nil, nil)
	wire := inv.wireNames
	catalog := inv.catalog

	// --- Direct membership: the wire set is exactly curated + meta + custom ---
	// (sessionToolNames already enforced per-name uniqueness: a duplicate
	// registration would leak a second entry onto the wire.)
	for _, n := range directToolNamesFor(FullDomainScope) {
		require.Truef(t, wire[n], "curated op %q must be directly listed", n)
	}
	for _, n := range metaToolNames {
		require.Truef(t, wire[n], "meta tool %q must be listed under progressive", n)
	}
	requireWireDirect(t, wire, expectedDirectCustomNames()...)
	require.Equal(t, expectedProgressiveDirectSet(t), wire,
		"progressive tools/list must be exactly curated + meta + the wired custom direct surface")

	// Search-only custom membership: catalog-indexed but never directly listed
	// under progressive (the OOB resume/account members especially — see
	// expectedDirectCustomNames).
	for _, n := range expectedSearchOnlyCustomNames() {
		require.Falsef(t, wire[n], "%q must stay search-only under progressive", n)
		require.Truef(t, searchVisible(t, catalog, n), "%q must be searchable", n)
	}

	// Dual-registration dedup: capabilities / agent_guide / upload_file carry
	// BOTH a direct spec and a catalog index in the same run, so each must
	// appear exactly once on the wire.
	res, err := inv.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, n := range []string{"capabilities", "agent_guide", "upload_file"} {
		count := 0
		for _, tool := range res.Tools {
			if tool.Name == n {
				count++
			}
		}
		require.Equalf(t, 1, count,
			"%q must appear exactly once on tools/list despite dual direct+index registration", n)
	}
	// Every wire tool outside the app-helper set resolves through the catalog
	// (the production pipeline indexes everything directly registered in this
	// bundle). The app-helper descriptors are server-registered only — they
	// carry no catalog entry, so they are discoverable on the wire but not
	// through search_tools.
	appHelpers := map[string]bool{
		"pin_status": true, "auth_sso_status": true,
		"ipfs_upload_submit": true, "ipfs_upload_status": true,
		"vault_upload_submit": true,
		"vault_create_status": true, "vault_restore_status": true,
	}
	for name := range wire {
		if slices.Contains(metaToolNames, name) || appHelpers[name] {
			continue // server-registered only (meta tools + app helpers), by design
		}
		_, ok := catalog.Get(name)
		require.Truef(t, ok || slices.Contains(catalog.DirectCustom, name),
			"wire tool %q must resolve to a catalog entry (or the explicit DirectCustom set)", name)
	}

	// --- Feature-gated relays: registration is the gate, not description copy
	// The startup generic HTTP host carries neither FeatSourceURL nor
	// FeatSourceData, so upload_url / upload_data are not registered even
	// though their executors are wired.
	require.False(t, wire["upload_url"], "generic HTTP host must not register upload_url (no FeatSourceURL)")
	require.False(t, wire["upload_data"], "generic HTTP host must not register upload_data (no FeatSourceData)")

	// --- Launchers: search-only on the agent-only host ----------------------
	// open_app (the consolidated launcher) stays catalog-indexed but NOT
	// direct; every per-app open_* launcher stays search-only on every host.
	require.False(t, wire["open_app"], "agent-only host must not list open_app directly")
	entry, ok := catalog.Get("open_app")
	require.True(t, ok, "open_app must be catalog-indexed")
	require.False(t, entry.DirectVisible, "open_app must stay search-only on the agent-only host")
	require.True(t, searchVisible(t, catalog, "open_app"), "open_app must stay discoverable via search")
	for _, l := range openLauncherToolNames(t) {
		require.Falsef(t, wire[l], "launcher %q must NOT be listed directly on the agent-only host", l)
		require.Truef(t, searchVisible(t, catalog, l),
			"launcher %q must remain discoverable (indexed) via search_tools", l)
		le, ok := catalog.Get(l)
		require.Truef(t, ok, "launcher %q must be a catalog entry", l)
		require.Falsef(t, le.DirectVisible, "launcher %q must stay search-only (DirectVisible=false)", l)
	}

	// --- Search-only compiled ops (progressive keeps them off tools/list) ---
	for _, n := range []string{"vault_ls", "dns_zones_list"} {
		require.Falsef(t, wire[n], "%q must stay search-only under progressive", n)
		require.Truef(t, searchVisible(t, catalog, n), "%q must be searchable", n)
	}

	// --- Hidden membership: wizard + admin ---------------------------------
	// Wizard tools are catalog-indexed (interactive flows) but hidden from
	// plain search and never direct; only an explicit wizard-category query
	// reveals them.
	for _, w := range []string{"setup_wizard_start", "websites_wizard_start", "domains_wizard_start"} {
		_, ok := catalog.Get(w)
		require.Truef(t, ok, "wizard entry %q must be indexed in the catalog", w)
		require.Falsef(t, wire[w], "wizard entry %q must never be a direct tool", w)
		require.Falsef(t, searchVisible(t, catalog, w),
			"wizard entry %q must be hidden from a plain search query", w)
		found := false
		for _, s := range catalog.SearchFor(w, string(model.CategoryWizard), 0, nil) {
			if s.Name == w {
				found = true
			}
		}
		require.Truef(t, found, "wizard entry %q must be discoverable with the explicit wizard category", w)
	}
	// Admin ops never surface on a plain search and are never direct; they are
	// only visible by browsing category=admin.
	adminOps := catalog.SearchFor("", "admin", 0, nil)
	require.NotEmpty(t, adminOps, "the full surface must materialize admin ops (gated inventory)")
	for _, a := range adminOps {
		require.Falsef(t, wire[a.Name], "admin op %q must never be a direct tool", a.Name)
	}

	// --- Prompt surface rides the production pipeline ----------------------
	prompts, err := inv.session.ListPrompts(context.Background(), nil)
	require.NoError(t, err)
	promptNames := map[string]bool{}
	for _, p := range prompts.Prompts {
		promptNames[p.Name] = true
	}
	for _, n := range []string{"website-onboarding", "website-update", "setup", "ens-publish"} {
		require.Truef(t, promptNames[n], "prompt %q must be registered on the assembled server", n)
	}
}

// TestCustomRegistrationInventoryHostProfileGates characterizes the dedicated
// per-host assembly: the same production pipeline re-run with a detected host
// profile (Claude HTTP) that declares FeatSourceData + FeatMCPApps but NOT
// FeatSourceURL. Registration decisions must follow the profile's feature set,
// not the generic startup transport.
func TestCustomRegistrationInventoryHostProfileGates(t *testing.T) {
	profile := hostenv.ProfileClaudeHTTP
	inv := buildInventoryServer(t, nil, &profile)
	wire := inv.wireNames
	catalog := inv.catalog

	// Host-gated relay tools: registration follows the profile's declared
	// features. Claude declares the data: relay but not the URL relay.
	require.True(t, wire["upload_data"], "host profile with FeatSourceData must register upload_data")
	require.False(t, wire["upload_url"], "host profile without FeatSourceURL must not register upload_url")

	// GUI-capable profile: the consolidated open_app launcher is direct.
	require.True(t, wire["open_app"], "FeatMCPApps host must list the consolidated open_app launcher directly")
	entry, ok := catalog.Get("open_app")
	require.True(t, ok)
	require.True(t, entry.DirectVisible, "open_app must be DirectVisible on a GUI-capable host")

	// Per-app launchers STILL stay search-only, even on a GUI-capable host:
	// the consolidated open_app tool is the single direct launcher.
	for _, l := range openLauncherToolNames(t) {
		require.Falsef(t, wire[l], "per-app launcher %q must never be individually direct", l)
		require.Truef(t, searchVisible(t, catalog, l), "per-app launcher %q must stay discoverable", l)
	}

	// The rest of the direct surface matches the generic inventory.
	requireWireDirect(t, wire, "upload_file", "download_file", "vault_get_file",
		"vault_put_file", "capabilities", "agent_guide", "auth_sso")
}

// TestCustomRegistrationInventoryHostedProductionAssembly characterizes the
// real hosted constructor (BuildHostedServer -> BuildServer ->
// registerCustomTools): the hosted surface drops the Sia vault and the CLI
// OOB/account subsystems entirely, while IPFS transfer wiring with a real
// upload task manager still materializes upload/download, and the prompts
// stay surface-filtered.
func TestCustomRegistrationInventoryHostedProductionAssembly(t *testing.T) {
	restoreConstructionGuards(t)

	srv, catalog, _, err := BuildHostedServer(HostedServerConfig{
		CatalogDeps: func() *CatalogDepsBundle { return fullTestBundle() },
		Options: []MCPServerOption{
			WithPrompts(),
			WithUploadTaskManager(mctf.NewUploadTaskManager(stubUploadExec, 0)),
			WithIPFSDownload(transfer.IPFSDownloadHandler(func(context.Context, string, io.Writer) error { return nil })),
		},
	})
	require.NoError(t, err, "real hosted assembly must build")

	sess := connectOfficialClient(t, srv)
	wire, _ := sessionToolNames(t, sess)

	// Hosted direct inventory.
	for _, n := range directToolNamesFor(HostedDomainScope) {
		require.Truef(t, wire[n], "hosted curated op %q must be directly listed", n)
	}
	for _, n := range metaToolNames {
		require.Truef(t, wire[n], "hosted meta tool %q must be listed", n)
	}
	requireWireDirect(t, wire, "capabilities", "agent_guide", "upload_file", "download_file")

	// The Sia vault and the CLI OOB/account subsystems are absent entirely —
	// not hidden behind the meta tools, never registered or indexed at all.
	for _, n := range []string{
		"vault_status", "vault_create", "vault_restore", "vault_put_file",
		"vault_get_file", "vault_create_resume", "vault_restore_resume",
		"auth_sso", "auth_resume", "auth_sso_revoke",
		"account_password_update", "account_password_reset", "account_email_change",
		"upload_url", "upload_data",
	} {
		require.Falsef(t, wire[n], "hosted server must not register %q", n)
		_, inCat := catalog.Get(n)
		require.Falsef(t, inCat, "hosted catalog must not index %q", n)
	}
	// open_app is DIRECTLY listed on the hosted server: this assembly installs
	// hosted-admissible app views (pin list, account, and — with transfer
	// executors wired — the upload/download managers), and the shared app
	// table's hosted gating makes the capability feature follow the installed
	// inventory, so the consolidated launcher is real here. The async
	// upload-management tools stay search-only: catalog-indexed (discoverable)
	// but never directly listed.
	require.Truef(t, wire["open_app"], "hosted assembly with installed app views must directly list open_app")
	for _, n := range []string{"upload_status", "upload_cancel", "upload_list"} {
		require.Falsef(t, wire[n], "hosted server must not directly list %q", n)
		_, inCat := catalog.Get(n)
		require.Truef(t, inCat, "hosted catalog must keep %q discoverable", n)
	}
	// The per-app open_* launchers stay search-only too — open_app is the
	// single direct launcher on every surface.
	for _, n := range []string{"open_pin_list", "open_account", "open_upload_manager", "open_download_manager"} {
		require.Falsef(t, wire[n], "hosted per-app launcher %q must stay search-only", n)
		_, inCat := catalog.Get(n)
		require.Truef(t, inCat, "hosted catalog must keep %q discoverable", n)
	}
	// No vault op surfaces in hosted search at all.
	require.False(t, searchVisible(t, catalog, "vault"), "no vault op may surface in hosted search")

	// DomainScope-filtered prompts still register through the pipeline, BUT the
	// wizard-workflow prompts (website-onboarding → websites_wizard_start,
	// setup → setup_wizard_start) must NOT be advertised: a hosted assembly
	// provisions NO wizard tools, so the surface filter alone is not
	// sufficient — the wizard-tool gate drops them (finding 6). The non-wizard
	// prompts whose underlying website/ENS tools ARE registered remain.
	prompts, err := sess.ListPrompts(context.Background(), nil)
	require.NoError(t, err)
	promptNames := map[string]bool{}
	for _, p := range prompts.Prompts {
		promptNames[p.Name] = true
	}
	require.NotContains(t, promptNames, "website-onboarding",
		"hosted assembly must not advertise the website-onboarding prompt: it scripts wizard tools it never registered")
	require.NotContains(t, promptNames, "setup",
		"hosted assembly must not advertise the setup prompt: it scripts wizard tools it never registered")
	require.Contains(t, promptNames, "website-update",
		"website-update only uses registered website tools, so it must survive")
	require.Contains(t, promptNames, "ens-publish",
		"ens-publish only uses registered ENS tools, so it must survive")
}

// TestListingPolicyIsolationUnderInterleavedConstruction characterizes
// per-server listing-policy isolation: four full-production servers built
// INTERLEAVED (progressive, flat-no-meta, progressive, flat-meta) through
// BuildServer + registerCustomTools. Each server's captured policy, wire
// tools/list, per-server card, and construction-time instructions must reflect
// its OWN policy — the listing policy each catalog captured at build time is
// the SOLE production source (the deprecated listing globals are never read by
// production, and are additionally NEVER altered by these builds). It ends by
// mutating the legacy globals and building two MORE servers afterwards to
// prove the globals cannot change construction outcomes at all.
func TestListingPolicyIsolationUnderInterleavedConstruction(t *testing.T) {
	flatNoMeta := ListingPolicy{Strategy: ListingFlat, IncludeMetaOnFlat: boolPtr(false)}
	flatMeta := ListingPolicy{Strategy: ListingFlat} // safe default: meta kept

	// Interleaved construction order (the realistic multi-server pattern).
	prog1 := buildInventoryServer(t, nil, nil)
	noMeta := buildInventoryServer(t, &flatNoMeta, nil)
	prog2 := buildInventoryServer(t, nil, nil)
	withMeta := buildInventoryServer(t, &flatMeta, nil)

	type serverCase struct {
		inv         *assembledServer
		progressive bool
		card        []string
	}
	checks := []serverCase{
		{prog1, true, serverCardNames(prog1.card.Tools())},
		{noMeta, false, serverCardNames(noMeta.card.Tools())},
		{prog2, true, serverCardNames(prog2.card.Tools())},
		{withMeta, false, serverCardNames(withMeta.card.Tools())},
	}

	// --- Per-policy capture correctness (each server reflects its own policy)
	for i, c := range checks {
		inv := c.inv
		want := ListingProgressive
		if !c.progressive {
			want = ListingFlat
		}
		require.Equalf(t, want, inv.catalog.Strategy,
			"each server's catalog must record its own strategy (server %d)", i)

		if c.progressive {
			// The interleaved progressive servers still materialize exactly
			// the progressive surface: meta tools present, pins_add hidden,
			// and no cross-contamination from the flat builds.
			for _, n := range metaToolNames {
				require.Truef(t, inv.wireNames[n], "progressive server %d must keep meta tool %q listed", i, n)
			}
			require.Falsef(t, inv.wireNames["pins_add"], "progressive server %d must keep pins_add search-only", i)
			continue
		}

		// Flat servers: every agent-safe op is direct (pins_add surfaced),
		// gated ops stay behind the gated surface, meta per the switch.
		require.Truef(t, inv.wireNames["pins_add"], "flat server %d must surface search-only pins_add directly", i)
		adminOps := inv.catalog.SearchFor("", "admin", 0, nil)
		require.NotEmptyf(t, adminOps, "flat full surface must still materialize gated admin ops (server %d)", i)
		for _, a := range adminOps {
			require.Falsef(t, inv.wireNames[a.Name],
				"flat must keep admin op %q behind the gated surface (server %d)", a.Name, i)
		}
		for _, w := range []string{"setup_wizard_start", "websites_wizard_start"} {
			require.Falsef(t, inv.wireNames[w], "flat must keep wizard entry %q non-direct (server %d)", w, i)
		}
		if i == 1 { // flat, IncludeMetaOnFlat=false
			for _, n := range metaToolNames {
				require.Falsef(t, inv.wireNames[n], "explicit IncludeMetaOnFlat=false must hide meta tool %q", n)
			}
			require.Falsef(t, inv.catalog.IncludeMetaOnFlat, "flat-no-meta catalog must record its own meta switch")
		} else {
			for _, n := range metaToolNames {
				require.Truef(t, inv.wireNames[n], "flat with the safe-default meta must list %q", n)
			}
			require.Truef(t, inv.catalog.IncludeMetaOnFlat, "flat-with-meta catalog must record its own meta switch")
		}
	}

	// Instructions are selected at construction time from the catalog's OWN
	// captured policy (ToolCatalog.Instructions): progressive promises
	// progressive discovery; flat (any meta switch) promises the fully-direct
	// surface without a discovery step.
	for i, c := range checks {
		if c.progressive {
			require.Containsf(t, c.inv.instructions, "two-tier",
				"progressive server %d instructions must describe the curated+discovery two-tier surface", i)
			require.Containsf(t, c.inv.instructions, "search_tools",
				"progressive server %d instructions must describe the discovery workflow", i)
		} else {
			require.Containsf(t, c.inv.instructions, "callable by name",
				"flat server %d instructions must promise the fully-direct surface", i)
		}
	}
	// The flat variants differ only in their meta-tool prose: with meta kept,
	// the gated ops stay reachable through the meta tools; without meta they
	// are absent from the channel entirely.
	require.Contains(t, withMeta.instructions, "no two-tier curated surface",
		"flat-with-meta instructions must say there is no two-tier surface")
	require.Contains(t, noMeta.instructions, "no discovery step",
		"flat-no-meta instructions must say the meta tools are absent")
	require.NotContains(t, noMeta.instructions, "search_tools",
		"flat-no-meta instructions must not advertise search_tools")

	// --- Per-server card isolation -----------------------------------------
	// Progressive cards advertise curated+meta; flat cards pin their own switch.
	require.Contains(t, checks[0].card, "search_tools", "progressive card must advertise the meta tools")
	require.NotContains(t, checks[0].card, "pins_add", "progressive card must not advertise search-only pins_add")
	require.Contains(t, checks[3].card, "pins_add", "flat card must advertise directly-visible pins_add")
	require.Contains(t, checks[3].card, "search_tools", "flat-with-meta card must advertise the meta tools")
	for _, m := range metaToolNames {
		require.NotContainsf(t, checks[1].card, m, "flat-no-meta card must not advertise meta tool %q", m)
	}

	// --- Isolation after mutating the legacy construction globals ------------
	// The catalog — not any global — decided every surface above. Now mutate
	// the deprecated listing globals to the most hostile values (flat, no
	// meta) and re-derive/re-read every previously captured surface: none of
	// the four servers may change.
	SetListingStrategy(ListingFlat)
	SetIncludeMetaOnFlat(false)
	SetDomainScope(HostedDomainScope)

	for i, c := range checks {
		// The cached per-server card still reflects THIS server's policy...
		require.Equalf(t, c.card, serverCardNames(c.inv.card.Tools()),
			"captured card must be per-server: unaffected by later construction-time global writes (server %d)", i)

		// ...and the wire tools/list of the still-connected session must be
		// unchanged: materialization happened at that server's construction.
		names, _ := sessionToolNames(t, c.inv.session)
		require.Equalf(t, c.inv.wireNames, names,
			"earlier server's tools/list must not change when later builds overwrite the listing globals (server %d)", i)
	}

	// The captured per-server catalogs still carry their own strategy pair.
	require.Equal(t, ListingProgressive, prog1.catalog.Strategy)
	require.Equal(t, ListingFlat, noMeta.catalog.Strategy)
	require.Equal(t, ListingProgressive, prog2.catalog.Strategy)
	require.Equal(t, ListingFlat, withMeta.catalog.Strategy)

	// --- Legacy globals cannot alter CONSTRUCTION ---------------------------
	// The catalogs above never read the globals, so none of these builds
	// should even write them. Then build two MORE servers while the globals
	// still say "flat, no-meta": a NO-policy server must come out progressive
	// with the safe meta default (its policy came from DefaultPolicy
	// resolution, not the globals), and an explicitly flat server must come
	// out flat per its own policy value — proving the deprecated globals are
	// a dead seam for production construction.
	afterProg := buildInventoryServer(t, nil, nil)
	for _, n := range metaToolNames {
		require.Truef(t, afterProg.wireNames[n], "no-policy build after hostile global writes must list meta tool %q", n)
	}
	require.True(t, afterProg.wireNames["search_tools"], "no-policy progressive build keeps search_tools")
	require.NotContains(t, afterProg.instructions, "whole agent-safe tool catalog directly",
		"no-policy build after hostile flat global writes must keep progressive instructions")
	require.Contains(t, afterProg.instructions, "intentionally two-tier",
		"no-policy build after hostile flat global writes must keep the two-tier progressive prose")
	require.Equal(t, ListingProgressive, afterProg.catalog.Strategy,
		"the hostile flat strategy global must not leak into a no-policy build")

	flatAfter := ListingPolicy{Strategy: ListingFlat} // safe default: meta kept
	afterFlat := buildInventoryServer(t, &flatAfter, nil)
	require.Truef(t, afterFlat.wireNames["pins_add"], "flat build after hostile global writes still materializes per its OWN policy")
	for _, n := range metaToolNames {
		require.Truef(t, afterFlat.wireNames[n], "flat build (meta kept) must list meta tool %q regardless of the hostile global false", n)
	}
	require.Equal(t, ListingFlat, afterFlat.catalog.Strategy)
	require.True(t, afterFlat.catalog.IncludeMetaOnFlat,
		"flat build with an omitted meta switch must record its own safe default true, not the hostile global false")
}
