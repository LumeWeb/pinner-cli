package mcp

import (
	"fmt"

	"go.lumeweb.com/mcpplane/session"
	mcptransfer "go.lumeweb.com/mcpplane/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/download"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
	pinnermcp "go.lumeweb.com/pinner/mcp"
	"go.lumeweb.com/pinner/mcp/appswire"
)

// customToolDeps bundles everything the custom/direct-tool registration needs.
// Keeping it a struct (rather than a long positional parameter list) makes the
// single registration entry point readable and lets tests build a mostly-zero
// value with only the fields they exercise.
type customToolDeps struct {
	// srv is the official SDK server onto which direct tools/resources/prompts
	// are registered.
	srv *sdk.Server
	// catalog is the internal ToolCatalog carrying every invocable tool. The
	// wizard and SSO tools are appended here (they are built after buildCatalog
	// returns); stampDirectTools then stamps which of them are directly visible.
	catalog *ToolCatalog
	// store backs wizard sessions and resource providers.
	store *session.SessionStore
	// oob, when non-nil, backs the out-of-band sign-in (SSO) and restore
	// tools; authHandles stores their pending handles, and handoffReg maps a
	// handle to its domain-specific resume continuation so the shared resume
	// template can poll it.
	oob         *auth.OutOfBandLogin
	authHandles *session.AsyncHandleStore
	handoffReg  *handoff.HandoffRegistry
	// seedDrop, oobRestore, and oobCreate back the vault create/restore OOB
	// hand-offs. seedDrop is the vault-create seed-drop coordinator, oobRestore
	// is the vault-restore coordinator, and oobCreate is the vault-create
	// coordinator. They are threaded here so the resume tools
	// (vault_create_resume / vault_restore_resume) can poll the
	// coordinators to completion over the same shared handoffReg + handles.
	seedDrop   *oob.SeedDrop
	oobRestore *oob.OOBRestore
	oobCreate  *oob.OOBCreate
	// curlUpload, when non-nil, backs the presigned HTTP PUT upload route (the
	// Upload coordinator): it mints a one-time endpoint whose PUT body
	// streams into the async UploadTaskManager. It feeds the consolidated
	// upload_file tool in remote (HTTP/tunnel) mode.
	curlUpload *mcptransfer.Upload
	// vaultUpload, when non-nil, backs the presigned HTTP PUT vault-write route
	// (the VaultHTTPUpload coordinator). It mints a one-time endpoint bound to
	// a destination vault path whose PUT body streams into the authenticated
	// vault write, staging the bytes locally (status: staged) before returning.
	// It feeds the "Upload to Vault" MCP App.
	vaultUpload *transfer.VaultHTTPUpload
	// downloadDrop, when non-nil, backs the one-time filedrop GET route (the
	// Download coordinator). It serves downloaded bytes out of band to a
	// consumer that shares no disk with the server. It feeds the access
	// download_file / vault_get_file drop branches.
	downloadDrop *mcptransfer.Download
	// accountOOB backs the out-of-band account credential change coordinator
	// (hosted browser forms -> authenticated UpdatePassword/UpdateEmail). It
	// enforces an authenticated session; the secret never transits the MCP/LLM
	// channel.
	accountOOB *auth.OOBAccountChange
	// accountWebAppURL is the account web app base URL surfaced by the password
	// reset tool's hand-off.
	accountWebAppURL string
	// resourceFactory, when non-nil, builds the pinner:// resource providers.
	resourceFactory ResourceProvidersFactory
	// opts carries the optional custom tools wired by MCPServerOption (upload,
	// apps, prompts).
	opts *mcpServerOptions
	// coLocated reports whether the server is running in pure stdio/local mode
	// (no HTTP transport, no tunnel). The local-path source modes of
	// upload_file and vault_put_file read arbitrary host paths, so they are
	// only safe — and only meaningful — when the caller shares the host. They
	// are never registered over a remote transport, where a network client
	// could use them to read/exfiltrate server-side files.
	coLocated bool
	// devTools reports whether the MCP server was launched with --dev-tools.
	// When enabled, the module-owned dev_* introspection tools are provisioned
	// onto the catalog (see registerDevTools) and the per-request raw wire
	// snapshot is captured so they can introspect the connected host. When
	// disabled they are absent from the surface entirely.
	devTools bool
	// tunnelOpenAI reports whether the server is running through the embedded
	// OpenAI Secure MCP Tunnel, which exposes no reachable HTTP mux (all RPC
	// flows through the tunnel protocol). It distinguishes the OpenAI tunnel
	// from a plain HTTP server or a non-OpenAI tunnel even when no presigned
	// curl coordinator is wired, so the transport advertised by capabilities
	// and the upload tools is derived from reachability rather than from
	// whether a coordinator happens to be registered.
	tunnelOpenAI bool
	// hostProfile, when non-nil, is the detected host profile for a dedicated
	// per-host HTTP server. It overrides the upload_file/vault_put_file tool
	// DESCRIPTION so tools/list advertises the host's file-handoff / source
	// presentation (e.g. an OpenAI-over-HTTP host sees the `file` handoff even
	// though the startup HTTP transport bakes the mint-only description). The
	// schema (source.mode enum) and handler remain transport-bound; only the
	// presented description varies. Nil means the startup server (descriptions
	// resolved for the startup transport only).
	hostProfile *hostenv.PlatformProfile
	// wizard deps are built from wizardFactory at Action time. All three are
	// nil when no wizard factory is configured.
	hasWizard bool
	wizardW   wizard.WebsitesWizardDeps
	wizardS   wizard.SetupWizardDeps
	wizardD   wizard.DomainWizardDeps
}

// transferToolAvailability is the single Pinner-owned registration-availability
// calculation for the byte-transfer tool families (upload_file, vault_put_file,
// upload_url, upload_data, download_file, vault_get_file). It combines the
// three inputs registration itself uses — the assembly surface, the wired
// handler/executors, and the effective host feature set — into one value, so
// the extension registration gates and the capabilities descriptor can never
// disagree: a tool is advertised by capabilities exactly when this value says
// it was registered, and this value is the same predicate the registration
// branches below consult.
type transferToolAvailability struct {
	// uploadFile / vaultPutFile: the unified transport tools are registered.
	uploadFile, vaultPutFile bool
	// uploadURL / uploadData: the separate relay tools are registered (the
	// handler is wired AND the effective feature set declares the feature AND
	// the upload surface is enabled).
	uploadURL, uploadData bool
	// downloadFile / vaultGetFile: the unified download tools are registered.
	downloadFile, vaultGetFile bool
}

// computeTransferAvailability applies the exact registration predicates
// (uploadFileAvailable / vaultPutFileAvailable plus the surface + feature +
// handler gates custom_tools.go registers under) once, so registration and
// the capabilities descriptor share one answer. It stays handler-faithful:
// wiring booleans are read from the same deps/options fields the branches use.
func computeTransferAvailability(deps customToolDeps, opts *mcpServerOptions, surface DomainScope, feats hostenv.FeatureSet) transferToolAvailability {
	uploadOn := surface.UploadOn()
	vaultOn := surface.VaultOn()
	featsCopy := feats
	return transferToolAvailability{
		uploadFile:   uploadOn && uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI),
		vaultPutFile: vaultOn && vaultPutFileAvailable(deps.coLocated, opts.localPathVaultPut != nil, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI),
		uploadURL:    uploadOn && opts.relayURLUpload != nil && featsCopy.Has(hostenv.FeatSourceURL),
		uploadData:   uploadOn && opts.dataURIUpload != nil && featsCopy.Has(hostenv.FeatSourceData),
		downloadFile: uploadOn && opts.ipfsDownload != nil,
		vaultGetFile: vaultOn && opts.vaultGet != nil,
	}
}

// collectServerExtensions declares every custom/direct tool, resource, prompt,
// and app extension for the deps bundle and completes the extension-plan
// COLLECTION phase. It is the single named home for the adhoc registration
// that used to live inline in the MCPCommand transport closure. It needs no
// official server to run: the projection closures (app installers, direct
// registrations, resources/prompts) read deps.srv only at materialize time, so
// a deps bundle with a nil srv collects fine and the caller injects the server
// via MaterializationPlan.Materialize — which is exactly what lets BuildServer collect
// BEFORE construction and derive instructions/card from the completed catalog.
//
// The wiring is delegated to a fixed, phase-based serverExtensionRegistry (see
// custom_tools_register.go): specs declare explicit registration roles
// (catalog-searchable membership, the direct SDK projection, app launchers,
// app-only helpers) instead of a boolean role matrix, and the collection phase
// validates the declared roles, provisions the direct catalog additions
// (wizard tools), and indexes every searchable extension — in that order —
// so the final indexed membership exists before any projection is derived.
// The extension families it covers: wizard tools (sessions + step handlers),
// the agent-facing out-of-band sign-in tools, direct/stamped catalog
// additions, the MCP App launchers (open_*) and their ui:// views, the
// upload/download/vault transport tools (upload_file, upload_url, upload_data,
// upload_status/cancel/list, download_file, vault_get_file, vault_put_file),
// the capability-detection tool + agent guide, and the pinner:// resources and
// (optionally) the prompt templates.
func collectServerExtensions(deps customToolDeps) (*MaterializationPlan, error) {
	opts := deps.opts
	if opts == nil {
		opts = &mcpServerOptions{}
	}

	// The surface controls which tool families are registered. It is recorded
	// on the ToolCatalog by buildCatalog; a zero surface is the full surface.
	// Restricting a family here (rather than at call time) means an omitted
	// tool cannot be called and never appears in search/discovery/guide copy.
	surface := deps.catalog.DomainScope
	vaultOn := surface.VaultOn()
	accountOn := surface.AccountOn()
	uploadOn := surface.UploadOn()

	// GUI-capable hosts (FeatMCPApps) get the consolidated open_app launcher on
	// tools/list; agent-only hosts do not (per-app open_* launchers are already
	// search-only). The effective features resolve from the detected host
	// profile (dedicated per-host server) or the startup transport's generic
	// profile.
	guiCapable := effectiveFeaturesFor(deps).Has(hostenv.FeatMCPApps)

	reg := newServerExtensionRegistry(deps.srv, deps.catalog)

	// The consolidated open_app launcher is INDEXED during the collection
	// phase (searchable, exactly like every other extension spec) so the
	// construction-time initialize-instruction count equals the FINAL indexed
	// catalog — the post-surface replace below swaps the entry's descriptor
	// (rebuilt after app views are installed, with the enumerated description)
	// without changing the catalog's size. The collection-time descriptor is
	// newOpenAppCollectionPlaceholder: a non-enumerating copy whose static
	// description names NO app inventory — no app view is installed yet at
	// collection time, so an enumerated "Available apps: none" copy would
	// violate the no-enumeration invariant and go stale on any assembly. Only
	// the materialized descriptor (post-surface) carries the enumerated
	// openAppDescriptionFor copy.
	reg.add(serverExtensionSpec{
		desc:  newOpenAppCollectionPlaceholder(deps.catalog),
		roles: serverExtensionRoles{roleCatalogSearch},
	})

	// Direct-phase provisions: wizard tools and (under --dev-tools) the
	// module-owned dev_* introspection tools append catalog entries whose
	// direct visibility rides the DirectVisible projection. The provisions run
	// first so the direct stamp sees them exactly as the former pre-run
	// registration did.
	if deps.hasWizard {
		reg.beforeDirectTools(func(cat *ToolCatalog) error {
			return wizard.RegisterWizardTools(cat, deps.store, deps.wizardW, deps.wizardS, deps.wizardD)
		})
	}
	if deps.devTools {
		reg.beforeDirectTools(func(cat *ToolCatalog) error {
			return registerDevTools(cat)
		})
	}

	// "Create a Pin" MCP App: open_pin_creator is the ONLY tool that opens the
	// Create a Pin app view. pins_add stays headless.
	if opts.pinnerPins != nil {
		pins, err := opts.pinnerPins()
		if err != nil {
			return nil, fmt.Errorf("failed to build pinning provider: %w", err)
		}
		if err := reg.addLauncherFor(appswire.LauncherPinCreator, func(srv *sdk.Server, catalog apps.AppCatalog) error {
			return apps.RegisterPinApp(srv, catalog, pins)
		}); err != nil {
			return nil, err
		}
	}

	// Agent-facing out-of-band sign-in tools (start + resume) are part of the
	// direct surface AND indexed for progressive discovery. Adding them to the
	// catalog with DirectVisible means a single registration path (the
	// DirectVisible scan in RegisterOfficialDirectTools) exposes them on
	// tools/list while the catalog entry supplies search/describe/invoke. When
	// the wizard transport is absent oob is nil and both tools return a
	// structured not-configured hand-off instead of hanging.
	//
	// These CLI out-of-band sign-in and account-credential tools are gated on
	// the presence of the OOB coordinator (deps.oob). A hosted server has no
	// such coordinator — Portal handles authentication via its own OAuth IdP —
	// so hosting must never advertise (or let a model attempt) a CLI SSO
	// browser flow, an account password/email OOB change, or their app views.
	if deps.oob != nil {
		authSSO := auth.NewAuthSSODescriptor(deps.oob, deps.authHandles, deps.handoffReg)
		authSSO.DirectVisible = true
		authResume := auth.NewAuthResumeDescriptor(deps.handoffReg, deps.authHandles)
		authSSORevoke := auth.NewAuthSSORevokeDescriptor(deps.oob, deps.authHandles, deps.handoffReg)
		reg.add(searchableOnly(authSSO))
		reg.add(searchableOnly(authResume))
		reg.add(searchableOnly(authSSORevoke))

		// auth_sso stays headless (it returns a needs_human URL+handle handoff);
		// open_sso_signin is the ONLY tool that opens the Sign In app view.
		if err := reg.addLauncherFor(appswire.LauncherSSOSignin, func(srv *sdk.Server, catalog apps.AppCatalog) error {
			return auth.RegisterAuthSSOApp(srv, catalog, deps.handoffReg, deps.authHandles)
		}); err != nil {
			return nil, err
		}

		// Out-of-band account credential tools: change the password (hosted browser
		// form -> authenticated UpdatePassword, requires an authenticated session)
		// and reset the password via an emailed link to the webapp. Direct-surface
		// tools like the SSO pair; when the coordinator/service are absent they
		// return a structured not-configured hand-off instead of hanging.
		accountUpdate := auth.NewAccountPasswordUpdateDescriptor(deps.accountOOB, deps.wizardS.AuthService, deps.authHandles, deps.handoffReg)
		accountReset := auth.NewAccountPasswordResetDescriptor(deps.wizardS.AuthService, deps.accountWebAppURL)
		accountEmail := auth.NewAccountEmailChangeDescriptor(deps.accountOOB, deps.wizardS.AuthService)
		reg.add(searchableOnly(accountUpdate))
		reg.add(searchableOnly(accountReset))
		reg.add(searchableOnly(accountEmail))

		// account_password_update / account_email_change stay headless (they return
		// a needs_human URL handoff); open_account_password / open_account_email
		// are the ONLY tools that open their one-shot deep-link app views.
		if err := reg.addLauncherFor(appswire.LauncherAccountPassword, auth.RegisterAccountPasswordApp); err != nil {
			return nil, err
		}
		if err := reg.addLauncherFor(appswire.LauncherAccountEmail, auth.RegisterAccountEmailApp); err != nil {
			return nil, err
		}
	} // end of CLI OOB / account-credential tool gating

	// Vault create/restore OOB hand-offs ride the SAME generic handoff-resume
	// framework: the invoke path (buildCatalog) mints a handle and registers a
	// per-domain continuation against it when it attaches a seed_url /
	// restore_url; these two named *_resume tools poll that continuation to
	// completion, pattern-matched from their domain-specific names. They are
	// direct-surface tools like the SSO resume tool. When the coordinators or
	// resume machinery are absent, the templates return a structured
	// not-configured hand-off instead of hanging.
	// vault_create / vault_restore stay headless (they return a needs_human
	// URL+handle handoff); open_vault_create / open_vault_restore are the ONLY
	// tools that open their app views. All vault lifecycle/app registration is
	// gated on the Sia vault surface: a hosted server without vault must not
	// advertise vault create/restore/browse at all.
	if vaultOn {
		vaultCreateResume := oob.NewVaultCreateResumeDescriptor(deps.handoffReg, deps.authHandles)
		vaultRestoreResume := oob.NewVaultRestoreResumeDescriptor(deps.handoffReg, deps.authHandles)
		reg.add(searchableOnly(vaultCreateResume))
		reg.add(searchableOnly(vaultRestoreResume))

		if err := reg.addLauncherFor(appswire.LauncherVaultCreate, func(srv *sdk.Server, catalog apps.AppCatalog) error {
			return vault.RegisterVaultCreateApp(srv, catalog, deps.handoffReg, deps.authHandles)
		}); err != nil {
			return nil, err
		}
		if err := reg.addLauncherFor(appswire.LauncherVaultRestore, func(srv *sdk.Server, catalog apps.AppCatalog) error {
			return vault.RegisterVaultRestoreApp(srv, catalog, deps.handoffReg, deps.authHandles)
		}); err != nil {
			return nil, err
		}

		// vault_status stays headless (returns raw JSON); open_vault_browser
		// is the ONLY tool that opens the Vault browser app view.
		if err := reg.addLauncherFor(appswire.LauncherVaultBrowser, vault.RegisterVaultBrowserApp); err != nil {
			return nil, err
		}
	}

	// pins_list stays headless (returns raw JSON); open_pin_list is the ONLY
	// tool that opens the Pin list app view.
	if err := reg.addLauncherFor(appswire.LauncherPinList, download.RegisterPinListApp); err != nil {
		return nil, err
	}

	// auth_status stays headless (returns raw JSON); open_account is the ONLY
	// tool that opens the Account app view. Gated on the account surface.
	if accountOn {
		if err := reg.addLauncherFor(appswire.LauncherAccount, auth.RegisterAuthStatusApp); err != nil {
			return nil, err
		}
	}

	// pinner:// resources and templates are built from the provider factory and
	// projected after the direct tool surface. The vault resource is omitted
	// when the Sia vault surface is disabled so a hosted server never
	// advertises pinner://vault/status.
	if deps.resourceFactory != nil {
		reg.afterSurface(func() error {
			provs := deps.resourceFactory(deps.store)
			provs.Sessions = deps.store
			resources, templates := ResourceDescriptorsForScope(provs, surface.VaultOn())
			return sdk.RegisterResources(reg.srv, resources, templates)
		})
	}

	// The consolidated open_app launcher: the single directly-surfaced UI
	// launcher for a GUI-capable host (all per-app open_* launchers are
	// search-only, see appLauncherSpec). The tool is already catalog-indexed
	// (and, under a FLAT strategy, stamped and projected direct by the direct
	// pass) from the COLLECTION phase; this post-surface hook REPLACES the
	// entry so its descriptor is rebuilt after every app view is installed —
	// the enumerated static description must bake post-install, never from the
	// pre-install collection stage — re-registering the wire descriptor when
	// the entry is direct so the flat wire carries the enumerated copy too.
	// Under the progressive default, open_app is direct only on a GUI-capable
	// host; on an agent-only host it stays search-only.
	reg.afterSurface(func() error {
		openApp := newOpenAppDescriptor(deps.catalog, *effectiveProfileFor(deps))
		openAppEntry := model.ToolEntryFromDescriptor(openApp)
		openAppEntry.DirectVisible = guiCapable || deps.catalog.listingStrategy() == ListingFlat
		deps.catalog.Add(openAppEntry)
		if openAppEntry.DirectVisible {
			// reg.srv is injected at materialize time (MaterializationPlan.Materialize),
			// so the hook works on both the collect-then-materialize build path
			// and the legacy single-pass registerCustomTools call. The SDK map
			// keys by name, so this replaces the collection-time registration
			// in place (exactly one wire slot — the uniqueness tests pin that).
			return RegisterOfficialDescriptor(reg.srv, openApp)
		}
		return nil
	})

	// The single byte-transfer availability calculation (surface + handlers +
	// effective features) both the registration branches below and the
	// capabilities descriptor consume: a tool is registered exactly when this
	// value says so, so capabilities can never advertise a tool absent from a
	// restricted surface.
	feats := effectiveFeaturesFor(deps)
	avail := computeTransferAvailability(deps, opts, surface, feats)

	// --- Vault put file (unified, transport-aware) ---
	// Every vault file/copy tool is gated on the Sia vault surface: a hosted
	// server without vault must never advertise vault_put_file or the vault
	// upload app.
	if avail.vaultPutFile {
		var pathFn vault.LocalPathVaultPutHandler
		if deps.coLocated {
			pathFn = opts.localPathVaultPut
		}
		// The effective features come from the detected host profile when this is
		// a dedicated per-host server, else from the startup transport's generic
		// profile. The schema, description, and Meta are all compiled from them.
		vaultFeatures := hostenv.ProfileForTransport(transfer.UploadFileTransport(deps.coLocated, deps.tunnelOpenAI)).Features
		if deps.hostProfile != nil {
			vaultFeatures = deps.hostProfile.Features
		}
		vaultPutDesc := vault.NewVaultPutFileDescriptor(vaultFeatures, deps.coLocated, deps.tunnelOpenAI, pathFn, deps.vaultUpload, opts.vaultPutHandler, opts.relayAllowedHosts, opts.maxRelayBytes)
		// A dedicated per-host server re-resolves the tool description against
		// the detected host profile (e.g. an OpenAI-over-HTTP host sees the
		// `file` handoff even though the startup HTTP transport bakes the
		// mint-only description). The schema and handler stay transport-bound.
		if deps.hostProfile != nil {
			if d, ok := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, *deps.hostProfile); ok {
				vaultPutDesc.Description = d
			}
		}
		// vault_put_file is a headless operational primitive: it never carries
		// ui.resourceUri (a UI-capable host would otherwise render a card on
		// every mid-workflow call). The App's view lives on the explicit
		// open_vault_manager launcher, which is registered only when the
		// presigned vault-upload coordinator (deps.vaultUpload) can mint a PUT
		// endpoint for the Uppy XHR uploader.
		vaultPutSpec := serverExtensionSpec{
			desc:  vaultPutDesc,
			roles: []serverExtensionRole{roleDirectTool},
		}
		// The Vault Mint App's catalog-search role tracks the presigned route
		// WIRING; the launcher and app register only when the presigned PUT
		// route is REACHABLE — the single shared transfer.SinkDropReachable
		// gate (wired coordinator AND a reachable HTTP mux; the
		// OpenAI-tunnel rationale lives above), the same decision as
		// vaultPutFileAvailable's presigned branch below.
		if deps.vaultUpload != nil {
			vaultPutSpec.roles = append(vaultPutSpec.roles, roleCatalogSearch)
		}
		if transfer.SinkDropReachable(deps.vaultUpload != nil, deps.tunnelOpenAI) {
			reg.add(appLauncherSpec(upload.NewOpenVaultManagerDescriptor(deps.vaultUpload), func(srv *sdk.Server, catalog apps.AppCatalog) error {
				return upload.RegisterVaultUploadApp(srv, catalog, deps.vaultUpload)
			}))
		}
		reg.add(vaultPutSpec)
	}

	// --- upload_url: relay a caller-supplied HTTPS URL (remote HTTP fallback) ---
	// (gated on uploadOn so an upload-disabled surface omits the URL relay)
	// Gated on the effective profile's FeatSourceURL: the URL relay tool is
	// registered only for a host that its feature set declares support for
	// (e.g. Grok, the OpenAI tunnel). A host without FeatSourceURL (generic
	// HTTP) does not get the tool at all — an omitted tool cannot be called,
	// whereas a visible tool with only a "do not call" sentence still gets
	// tried. Registration, not copy, is the gate.
	if avail.uploadURL {
		relayDesc := upload.RelayURLUploadDescriptor(opts.relayURLUpload, opts.relayAllowedHosts, opts.maxRelayBytes)
		// Re-resolve the baked tools/list description against the effective
		// profile (the URL relay bakes against generic HTTP, whose no-relay
		// forbid would otherwise stick on a host that actually registers the
		// tool). A dedicated per-host server uses the detected host profile; the
		// startup server (OpenAI tunnel) falls back to its transport profile so
		// upload_url — like upload_data — carries its usable copy there instead
		// of misleading a model into avoiding a callable tool.
		if d, ok := toolforge.ResolveDescription(upload.RelayURLUploadTargets, *effectiveProfileFor(deps)); ok {
			relayDesc.Description = d
		}
		reg.add(directSearchable(relayDesc))
	}

	// --- Consolidated download_file: a single sink-aware IPFS download tool. ---
	//   - sink=local (every transport): opts.ipfsDownload streams the CID bytes
	//     to a host-side path on the MCP server's own disk.
	//   - sink=drop (HTTP / real tunnel): deps.downloadDrop mints a one-time
	//     filedrop GET.
	// Register it whenever the IPFS download executor is wired. The filedrop
	// coordinator (downloadDrop) is wired alongside when any download executor
	// exists, but the drop sink is only honored on transports with a reachable
	// HTTP mux (tunnelOpenAI=false) — see downloadFileDescription.
	if avail.downloadFile {
		downloadRoot := transfer.ResolveDownloadRoot(opts.downloadRoot)
		dlDesc := transfer.NewDownloadFileDescriptor(opts.ipfsDownload, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		// download_file is headless; the app's view attaches to the explicit
		// open_download_manager launcher.
		if err := reg.addLauncherFor(appswire.LauncherDownloadManager, download.RegisterIPFSDownloadApp); err != nil {
			return nil, err
		}
		reg.add(directSearchable(dlDesc))
	}

	// --- Consolidated vault_get_file: a single sink-aware vault download tool. ---
	// Gated on the Sia vault surface like every other vault tool.
	if avail.vaultGetFile {
		downloadRoot := transfer.ResolveDownloadRoot(opts.downloadRoot)
		dlDesc := vault.NewVaultGetFileDescriptor(opts.vaultGet, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		// vault_get_file is headless; the app's view attaches to the explicit
		// open_vault_download_manager launcher.
		if err := reg.addLauncherFor(appswire.LauncherVaultDownloadMgr, download.RegisterVaultDownloadApp); err != nil {
			return nil, err
		}
		reg.add(directSearchable(dlDesc))
	}

	// --- upload_data: SEP-2356 data: URI relay (draft x-mcp-file mode) ---
	// Gated on the effective profile's FeatSourceData (mirroring upload_url):
	// the data: URI relay tool is registered only for a host whose feature set
	// declares data: support (e.g. Grok, the OpenAI tunnel). A host without
	// FeatSourceData does not get the tool at all — registration, not a "do not
	// call" sentence, is the gate.
	if avail.uploadData {
		dataDesc := transfer.DataURIUploadDescriptor(opts.dataURIUpload, opts.maxRelayBytes)
		// Re-resolve the baked tools/list description against the effective
		// profile (mirroring upload_file/vault_put_file/upload_url), so a host
		// whose feature set registers the data: URI relay carries its usable
		// copy rather than a "do NOT call this tool" forbid.
		if d, ok := toolforge.ResolveDescription(transfer.DataURIUploadTargets, *effectiveProfileFor(deps)); ok {
			dataDesc.Description = d
		}
		reg.add(directSearchable(dataDesc))
	}

	// --- Consolidated upload_file: a single transport-aware IPFS upload tool. ---
	// The caller does not pick a mechanism — registration routes by transport:
	//   - co-located (stdio/local): source mode path via the local-path
	//     handler (opts.localPathUpload).
	//   - remote (HTTP/tunnel): source mode mint via the presigned Upload
	//     coordinator (deps.curlUpload).
	//   - openai tunnel: source mode url/data via the file-relay executor
	//     (opts.uploadHandler), since no reachable HTTP mux exists.
	if avail.uploadFile {
		var pathFn transfer.UploadFileHandler
		if deps.coLocated {
			pathFn = opts.localPathUpload
		}
		// The effective features come from the detected host profile when this is
		// a dedicated per-host server, else from the startup transport's generic
		// profile. The schema, description, and Meta are all compiled from them.
		uploadFeatures := hostenv.ProfileForTransport(transfer.UploadFileTransport(deps.coLocated, deps.tunnelOpenAI)).Features
		if deps.hostProfile != nil {
			uploadFeatures = deps.hostProfile.Features
		}
		uploadFileDesc := transfer.NewUploadFileDescriptor(uploadFeatures, deps.coLocated, deps.tunnelOpenAI, pathFn, deps.curlUpload, opts.uploadHandler, opts.relayAllowedHosts, opts.maxRelayBytes)
		// A dedicated per-host server re-resolves the tool description against
		// the detected host profile (e.g. an OpenAI-over-HTTP host sees the
		// `file` handoff even though the startup HTTP transport bakes the
		// mint-only description). The schema (source.mode enum) and handler
		// stay transport-bound.
		if deps.hostProfile != nil {
			if d, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, *deps.hostProfile); ok {
				uploadFileDesc.Description = d
			}
		}
		// upload_file is headless; the app's view attaches to the explicit
		// open_upload_manager launcher, which is registered only when the
		// presigned Upload coordinator (deps.curlUpload) can mint a PUT
		// endpoint for the Uppy XHR uploader. In co-located stdio local-path
		// mode there is no presigned endpoint, so no app is registered.
		uploadFileSpec := serverExtensionSpec{
			desc:  uploadFileDesc,
			roles: []serverExtensionRole{roleDirectTool},
		}
		// Mirror of the vault launcher gate: the Upload to IPFS App's
		// catalog-search role tracks the presigned route WIRING; the launcher
		// and app register only on REACHABILITY — the single shared
		// transfer.SinkDropReachable gate (see its rationale above), the same
		// decision as uploadFileAvailable's presigned branch below.
		if deps.curlUpload != nil {
			uploadFileSpec.roles = append(uploadFileSpec.roles, roleCatalogSearch)
		}
		if transfer.SinkDropReachable(deps.curlUpload != nil, deps.tunnelOpenAI) {
			// Shared seam (go.lumeweb.com/pinner/mcp/appswire): the launcher
			// descriptor and the dependency-bound installer are module-owned;
			// the CLI supplies the render func and the process-global app
			// registry. A descriptor build error is a wiring bug at this seam
			// (same policy as addLauncher) and fails the assembly hard.
			desc, err := appswire.UploadManagerDescriptor(deps.curlUpload)
			if err != nil {
				return nil, err
			}
			reg.add(appLauncherSpec(desc, func(srv *sdk.Server, catalog apps.AppCatalog) error {
				return apps.InstallUploadManagerApp(srv, catalog, deps.curlUpload)
			}))
		}
		reg.add(uploadFileSpec)
	}

	// --- Async upload management tools (upload_status / upload_cancel / upload_list) ---
	// These are search-only: the agent_guide upload flow names upload_status
	// as a step, so an agent following the guide discovers it via search_tools.
	// The descriptors are module-owned (go.lumeweb.com/pinner/mcp), so this
	// surface cannot drift from the hosted assembly's. Descriptors without
	// MCPTargets get the universal Fallback wrap in ToolCatalog.Add.
	// upload_list is registered unconditionally here: this CLI server is a
	// single-user, same-process manager — the per-principal-manager case the
	// module's AsyncUploadList opt-in reserves the enumerator for.
	if uploadOn && opts.uploadTasks != nil {
		for _, desc := range pinnermcp.NewAsyncUploadTools(opts.uploadTasks) {
			reg.add(searchableOnly(desc))
		}
	}

	// Always expose capability detection so hosts can choose a file-input mode
	// without assuming draft MCP file support is negotiated. Each capability
	// reflects whether its handler is actually wired. A dedicated per-host
	// server re-resolves the baked description against the detected profile so
	// tools/list never promises a `file` parameter the host cannot fill. The
	// re-resolve threads the same tool-wiring flags so the description drops the
	// file-handoff prose when no upload/vault tool is wired, matching the report.
	// Every capabilities fact comes from the SAME availability calculation the
	// registration branches above consumed (avail), so the report equals the
	// eligible registration by construction — including on restricted
	// surfaces where a wired handler exists but its family is disabled.
	uploadWired := avail.uploadFile
	vaultWired := avail.vaultPutFile
	capDesc := NewCapabilitiesDescriptor(
		deps.coLocated,
		deps.tunnelOpenAI,
		uploadWired,
		vaultWired,
		avail.downloadFile,
		avail.vaultGetFile,
		deps.downloadDrop != nil,
		avail.uploadURL,  // upload_url relay tool registration (gates upload_tools + URL registration)
		avail.uploadData, // upload_data relay tool registration (gates upload_tools + data registration)
		avail.uploadData, // the data: URI upload tool carries the draft x-mcp-file metadata
		opts.maxRelayBytes,
	)
	if deps.hostProfile != nil {
		capDesc.Description = capabilitiesDescriptionFor(*deps.hostProfile, uploadWired, vaultWired, avail.downloadFile, avail.vaultGetFile)
	}
	// capabilities is both directly visible on tools/list and indexed in the
	// catalog so a cold-start host following search_tools(help) can resolve it.
	reg.add(directSearchable(capDesc))

	// Always expose the agent guide so a model can orient to the primary flows
	// without probing each tool's description. It is both directly visible on
	// tools/list and indexed in the catalog so a cold-start host that follows
	// search_tools(help) can resolve and read it via describe_tool / the typed invoke dispatchers.
	//
	// The guide handler is built from THIS server's captured DomainScope/Hosted (the
	// immutable context recorded on the assembled catalog by buildCatalog), so an
	// active agent_guide request is never crossed by a concurrent host-profile
	// REassembly that rewrites the package construction globals. It deliberately
	// does NOT use NewAgentGuideDescriptor, whose handler reads those globals.
	// deps.catalog.DomainScope is the same value registered against the catalog by
	// buildCatalog (see the surface declaration above); deps.catalog.Hosted is
	// its deployment-mode counterpart. The completed-catalog availability makes
	// the guide a derived projection of the FINAL per-server surface: steps
	// naming tools this assembly never registered (e.g. the OOB pair on a
	// hosted server) are removed at request time.
	reg.add(directSearchable(agentGuideDescriptorFor(deps.catalog.DomainScope, deps.catalog.Hosted, catalogGuideAvailability(deps.catalog))))

	// Optionally expose the prompt templates, filtered to the surface so a
	// hosted server never exposes a prompt whose underlying tools are absent.
	// The wizard-workflow prompts additionally require their wizard START
	// tools: a hosted (or otherwise wizard-free) assembly with the
	// websites/account domain surfaces enabled still must not advertise a
	// website-onboarding/setup prompt whose script calls tools it never
	// registered — so those prompts are dropped unless the finalized catalog
	// carries the wizard tools.
	if opts.prompts {
		reg.afterSurface(func() error {
			prompts := dropWizardPromptsWithoutWizardTools(deps.catalog, PromptDescriptorsForScope(surface, deps.catalog.Hosted))
			if len(prompts) == 0 {
				return nil
			}
			return sdk.RegisterPrompts(reg.srv, prompts)
		})
	}

	// Complete the collection phase NOW: when this returns, the plan is
	// finished (roles validated, direct-phase provisions indexed, every
	// searchable extension indexed into the catalog) and no server-facing
	// projection has run yet. The caller materializes the returned plan against
	// the official server exactly once (MaterializationPlan.Materialize), which is what
	// lets BuildServer collect before construction and derive the
	// instructions/card from the completed per-server catalog.
	plan := &MaterializationPlan{reg: reg}
	if err := reg.complete(); err != nil {
		return nil, err
	}
	return plan, nil
}

// registerCustomTools is the legacy single-pass path: it collects the
// extension plan and immediately materializes it against deps.srv. Production
// construction (BuildServer via ServerConfig.CollectExtensions, the hosted
// constructor, the tunnel assembly) routes through collectServerExtensions +
// MaterializationPlan.Materialize instead, so instructions and the server card derive
// from the completed plan; registerCustomTools remains for tests and callers
// that hold an already-constructed server.
func registerCustomTools(deps customToolDeps) error {
	plan, err := collectServerExtensions(deps)
	if err != nil {
		return err
	}
	_, err = plan.Materialize(deps.srv)
	return err
}

// MINT LAUNCHER REACHABILITY: presigned mint routes (the upload_file curl PUT
// coordinator and the vault_put_file presigned vault-upload coordinator) are
// usable for a LAUNCHER/app view only when the coordinator is wired AND the
// transport exposes a reachable HTTP mux. The embedded OpenAI tunnel exposes
// no mux (all RPC flows through the tunnel protocol), so its minted URL would
// fall back to an unreachable loopback — the launcher and app must not be
// advertised there. The single gate is transfer.SinkDropReachable — the ONE
// mux-reachability predicate, shared verbatim with the filedrop-sink decision
// (sink=drop advertisement vs. download acceptance) so the launcher gates,
// transferBranchAvailable (both transfer predicates' presigned branch), and
// the sink advertisement can never drift.
//
// Keep calling transfer.SinkDropReachable directly at every gate below; do
// not reintroduce a local same-truth-table copy.

// transferBranchAvailable is the ONE branch decision shared by
// uploadFileAvailable and vaultPutFileAvailable (the two differ only in the
// presigned-route wiring name). The tool has at least one real file-input
// branch when —
//
//   - co-located (stdio): a local path handler is wired; or
//   - an HTTP / real tunnel transport exposes the reachable presigned route
//     (transfer.SinkDropReachable); or
//   - an OpenAI tunnel transport has a relay executor wired (no reachable HTTP
//     mux — all RPC flows through the tunnel protocol — so only the url/data
//     relay path can carry bytes).
//
// Routing both predicates' presigned branch through transfer.SinkDropReachable
// keeps the tool-registration decision, the launcher gates, and the
// transferToolAvailability report on one code path.
func transferBranchAvailable(coLocated, localPathWired, presignedWired, relayWired, tunnelOpenAI bool) bool {
	if coLocated {
		return localPathWired
	}
	if transfer.SinkDropReachable(presignedWired, tunnelOpenAI) {
		return true
	}
	return tunnelOpenAI && relayWired
}

// uploadFileAvailable reports whether the consolidated upload_file tool has at
// least one real file-input branch for the running transport (the shared
// transferBranchAvailable decision):
//
//   - co-located (stdio): a local path upload handler is wired.
//   - HTTP / real tunnel: a reachable presigned HTTP PUT coordinator is wired
//     (the shared mux is reachable, so mint is usable).
//   - OpenAI tunnel: a file-relay executor is wired (no reachable HTTP mux —
//     all RPC flows through the tunnel protocol — so only the url/data relay
//     path can carry bytes).
//
// It is the single decision used both when registering the tool and when
// reporting the upload_file capability, so the two can never drift.
func uploadFileAvailable(coLocated, localPathWired, curlWired, relayWired, tunnelOpenAI bool) bool {
	return transferBranchAvailable(coLocated, localPathWired, curlWired, relayWired, tunnelOpenAI)
}

// effectiveFeaturesFor returns the feature set that determines tool registration
// for this server. It mirrors the hostProfile-or-transport computation used for
// the upload_file/vault_put_file feature set (see custom_tools.go): a dedicated
// per-host server uses the detected host profile's own features, while the
// startup server falls back to the transport's generic profile. It is the single
// source of truth for feature-gated tool registration (e.g. upload_data on
// FeatSourceData, upload_url on FeatSourceURL), so a host either registers a
// capability-backed tool or omits it entirely — never advertises it and then
// forbids it in prose.
func effectiveFeaturesFor(deps customToolDeps) hostenv.FeatureSet {
	return effectiveProfileFor(deps).Features
}

// effectiveProfileFor returns the PlatformProfile that determines tool
// registration and baked-description resolution for this server. It mirrors
// effectiveFeaturesFor: a dedicated per-host server uses the detected host
// profile, while the startup server falls back to the transport's generic
// profile (e.g. the OpenAI tunnel resolves against TransportOpenAI so its tools
// bake a usable, non-forbid description). Returns a pointer so call sites can
// re-resolve a tool's MCPTargets against the same profile uniformly.
func effectiveProfileFor(deps customToolDeps) *hostenv.PlatformProfile {
	if deps.hostProfile != nil {
		return deps.hostProfile
	}
	p := hostenv.ProfileForTransport(transfer.UploadFileTransport(deps.coLocated, deps.tunnelOpenAI))
	return &p
}

// vaultPutFileAvailable reports whether the unified vault_put_file tool has at
// least one usable branch for the running transport, mirroring
// uploadFileAvailable for the vault surface (same shared
// transferBranchAvailable decision; see the Mint LAUNCHER REACHABILITY note
// above for the OpenAI-tunnel presigned-route rationale):
//
//   - co-located (stdio): a local-path vault handler is wired.
//   - HTTP / real tunnel: a reachable presigned vault-upload coordinator
//     (VaultHTTPUpload) is wired AND the transport exposes a reachable HTTP
//     mux. The embedded OpenAI tunnel has no reachable mux, so its minted URL
//     would fall back to an unreachable loopback — the tool must not be
//     advertised for a branch no agent could use.
//   - OpenAI tunnel: a vault relay write executor is wired (only the url/data
//     path can carry bytes).
//
// It is the single decision used both when registering the tool and when
// reporting the vault_put_file capability, so the two can never drift.
func vaultPutFileAvailable(coLocated, localPathWired, mintWired, relayWired, tunnelOpenAI bool) bool {
	return transferBranchAvailable(coLocated, localPathWired, mintWired, relayWired, tunnelOpenAI)
}

// registerOpenLauncher is a TEST-ONLY launcher registration helper (only
// seedLauncherForTest calls it; no production assembly path does). It registers
// a model-facing open_* UI launcher tool: one that carries
// _meta.ui.resourceUri so a supporting host renders the app's iframe and stays
// the ONLY tool advertising that resourceUri — the operational primitives the
// app is attached to (upload_file, vault_status, pins_list, ...) remain
// headless (no resourceUri), so ordinary mid-workflow calls never render a
// card.
//
// CONTRACT NOTE: this helper's catalog + direct registration is NOT the
// production launcher contract. Production routes every open_* launcher
// through the serverExtensionRegistry (appLauncherSpec in
// custom_tools_register.go), which keeps launchers catalog-searchable and
// attached to their app view but NEVER individually projected onto tools/list
// — the consolidated open_app tool is the single direct launcher, and the role
// validation rejects any direct+launcher combination. This helper exists only
// so tests can seed a launcher the production indexer does before calling a
// RegisterXxxApp view.
func registerOpenLauncher(deps customToolDeps, launcher model.ToolDescriptor) error {
	if launcher.Meta == nil {
		return fmt.Errorf("open_* launcher %q must declare _meta.ui (resourceUri)", launcher.Name)
	}
	// Index for progressive discovery. DirectVisible isn't set here — the
	// app's AttachTo will stamp it onto the catalog entry below; marking it
	// here too would be harmless but redundant.
	deps.catalog.Add(model.ToolEntryFromDescriptor(launcher))
	// Register directly on tools/list so the model can invoke it.
	return RegisterOfficialDescriptor(deps.srv, launcher)
}
