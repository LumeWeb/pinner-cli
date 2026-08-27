package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/download"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
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
	// returns); markCurated then stamps which of them are directly visible.
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
	curlUpload *transfer.Upload
	// vaultUpload, when non-nil, backs the presigned HTTP PUT vault-write route
	// (the VaultHTTPUpload coordinator). It mints a one-time endpoint bound to
	// a destination vault path whose PUT body streams into the authenticated
	// vault write synchronously. It feeds the "Upload to Vault" MCP App.
	vaultUpload *transfer.VaultHTTPUpload
	// downloadDrop, when non-nil, backs the one-time filedrop GET route (the
	// Download coordinator). It serves downloaded bytes out of band to a
	// consumer that shares no disk with the server. It feeds the access
	// download_file / vault_get_file drop branches.
	downloadDrop *transfer.Download
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
	// devTools reports whether the MCP server was launched with --dev-tools. When
	// enabled, the dev_* introspection tools are registered onto the catalog (as
	// DirectVisible entries) and the per-request raw wire snapshot is captured so
	// they can introspect the connected host. When disabled they are absent from
	// the surface entirely.
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

// registerCustomTools registers every custom/direct tool, resource, and prompt
// onto the server. It is the single named home for the adhoc registration that
// used to live inline in the MCPCommand transport closure, so the wiring is
// cognitively isolated from the server pump:
//
//   - wizard tools (wizard.RegisterWizardTools) — sessions + step handlers
//   - the agent-facing out-of-band sign-in tools, which join the catalog as
//     DirectVisible tools instead of a separate RegisterOfficialDescriptor path
//   - the markCurated + curated tools/list surface
//   - the MCP App launchers (open_*) and their ui:// views
//   - the upload/download/vault transport tools (upload_file, upload_url,
//     upload_data, upload_status/cancel/list, download_file, vault_get_file,
//     vault_put_file) and the capability-detection tool + agent guide
//   - the pinner:// resources and (optionally) the prompt templates
//
// The wiring itself is delegated to a fixed, phase-based customToolRegistry
// (see custom_tools_register.go): specs are declared here, then the pipeline
// indexes the catalog, installs app views, stamps the curated surface, and
// projects the direct tools/list — in that order — so ordering dependencies
// (an app view attaching _meta.ui to a launcher that must be catalog-indexed
// first) resolve regardless of declaration order.
func registerCustomTools(deps customToolDeps) error {
	// Wizard tools and dev-introspection tools register directly into the
	// catalog (wizard via its own toolAdder, dev tools as DirectVisible
	// entries). Both must already be present when the pipeline stamps the
	// curated surface, so they are registered before customToolRegistry.run().
	if deps.hasWizard {
		if err := wizard.RegisterWizardTools(deps.catalog, deps.store, deps.wizardW, deps.wizardS, deps.wizardD); err != nil {
			return err
		}
	}
	if deps.devTools {
		registerDevTools(deps.catalog)
	}

	opts := deps.opts
	if opts == nil {
		opts = &mcpServerOptions{}
	}

	reg := newCustomToolRegistry(deps.srv, deps.catalog)

	// "Create a Pin" MCP App: open_pin_creator is the ONLY tool that opens the
	// Create a Pin app view. pins_add stays headless.
	if opts.pinnerPins != nil {
		pins, err := opts.pinnerPins()
		if err != nil {
			return fmt.Errorf("failed to build pinning provider: %w", err)
		}
		reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        apps.OpenPinCreatorToolName,
			Title:       "Create a Pin",
			Description: "Open the interactive Create a Pin app. This is a UI launcher: it renders an iframe for a human to enter a CID and pin it. It is not a headless primitive. Prefer pins_add (headless) for autonomous workflows; call this only when a human-facing pin form is desired.",
			Category:    model.CategoryCore,
			ResourceURI: apps.PinCreateAppURI,
		}), func(srv *sdk.Server, catalog apps.AppCatalog) error {
			return apps.RegisterPinApp(srv, catalog, pins)
		}))
	}

	// Agent-facing out-of-band sign-in tools (start + resume) are part of the
	// direct surface AND indexed for progressive discovery. Adding them to the
	// catalog with DirectVisible means a single registration path (the
	// DirectVisible scan in RegisterOfficialCuratedTools) exposes them on
	// tools/list while the catalog entry supplies search/describe/invoke. When
	// the wizard transport is absent oob is nil and both tools return a
	// structured not-configured hand-off instead of hanging.
	authSSO := auth.NewAuthSSODescriptor(deps.oob, deps.authHandles, deps.handoffReg)
	authSSO.DirectVisible = true
	authResume := auth.NewAuthResumeDescriptor(deps.handoffReg, deps.authHandles)
	authResume.DirectVisible = true
	reg.add(customToolSpec{desc: authSSO, index: true})
	reg.add(customToolSpec{desc: authResume, index: true})

	// auth_sso stays headless (it returns a needs_human URL+handle handoff);
	// open_sso_signin is the ONLY tool that opens the Sign In app view.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenSSOSigninToolName,
		Title:       "Sign In (App)",
		Description: "Open the interactive Sign In app. This is a UI launcher: it renders an iframe for a human to complete SSO approval. It is not a headless primitive. Prefer auth_sso (headless) for autonomous sign-in, which returns the approval URL + resume handle without rendering a card.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AuthSSOAppURI,
	}), func(srv *sdk.Server, catalog apps.AppCatalog) error {
		return auth.RegisterAuthSSOApp(srv, catalog, deps.handoffReg, deps.authHandles)
	}))

	// Out-of-band account credential tools: change the password (hosted browser
	// form -> authenticated UpdatePassword, requires an authenticated session)
	// and reset the password via an emailed link to the webapp. Direct-surface
	// tools like the SSO pair; when the coordinator/service are absent they
	// return a structured not-configured hand-off instead of hanging.
	accountUpdate := auth.NewAccountPasswordUpdateDescriptor(deps.accountOOB, deps.wizardS.AuthService, deps.authHandles, deps.handoffReg)
	accountUpdate.DirectVisible = true
	accountReset := auth.NewAccountPasswordResetDescriptor(deps.wizardS.AuthService, deps.accountWebAppURL)
	accountReset.DirectVisible = true
	accountEmail := auth.NewAccountEmailChangeDescriptor(deps.accountOOB, deps.wizardS.AuthService)
	accountEmail.DirectVisible = true
	reg.add(customToolSpec{desc: accountUpdate, index: true})
	reg.add(customToolSpec{desc: accountReset, index: true})
	reg.add(customToolSpec{desc: accountEmail, index: true})

	// account_password_update / account_email_change stay headless (they return
	// a needs_human URL handoff); open_account_password / open_account_email
	// are the ONLY tools that open their one-shot deep-link app views.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountPasswordToolName,
		Title:       "Change Password (App)",
		Description: "Open the interactive Change Password app. This is a UI launcher: it renders an iframe for a human to change their password. It is not a headless primitive. Prefer account_password_update (headless) for autonomous flows.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AccountPasswordAppURI,
	}), auth.RegisterAccountPasswordApp))
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountEmailToolName,
		Title:       "Change Email (App)",
		Description: "Open the interactive Change Email app. This is a UI launcher: it renders an iframe for a human to change their email. It is not a headless primitive. Prefer account_email_change (headless) for autonomous flows.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AccountEmailAppURI,
	}), auth.RegisterAccountEmailApp))

	// Vault create/restore OOB hand-offs ride the SAME generic handoff-resume
	// framework: the invoke path (buildCatalog) mints a handle and registers a
	// per-domain continuation against it when it attaches a seed_url /
	// restore_url; these two named *_resume tools poll that continuation to
	// completion, pattern-matched from their domain-specific names. They are
	// direct-surface tools like the SSO resume tool. When the coordinators or
	// resume machinery are absent, the templates return a structured
	// not-configured hand-off instead of hanging.
	vaultCreateResume := oob.NewVaultCreateResumeDescriptor(deps.handoffReg, deps.authHandles)
	vaultCreateResume.DirectVisible = true
	vaultRestoreResume := oob.NewVaultRestoreResumeDescriptor(deps.handoffReg, deps.authHandles)
	vaultRestoreResume.DirectVisible = true
	reg.add(customToolSpec{desc: vaultCreateResume, index: true})
	reg.add(customToolSpec{desc: vaultRestoreResume, index: true})

	// vault_create / vault_restore stay headless (they return a needs_human
	// URL+handle handoff); open_vault_create / open_vault_restore are the ONLY
	// tools that open their app views.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultCreateToolName,
		Title:       "Create Vault (App)",
		Description: "Open the interactive Create Vault app. This is a UI launcher: it renders an iframe for a human to create a vault (Sia approval + recovery seed). It is not a headless primitive. Prefer vault_create (headless) which returns the create URL + resume handle without rendering a card.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultCreateAppURI,
	}), func(srv *sdk.Server, catalog apps.AppCatalog) error {
		return vault.RegisterVaultCreateApp(srv, catalog, deps.handoffReg, deps.authHandles)
	}))
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultRestoreToolName,
		Title:       "Restore Vault (App)",
		Description: "Open the interactive Restore Vault app. This is a UI launcher: it renders an iframe for a human to restore a vault from its recovery seed. It is not a headless primitive. Prefer vault_restore (headless) which returns the restore URL + resume handle without rendering a card.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultRestoreAppURI,
	}), func(srv *sdk.Server, catalog apps.AppCatalog) error {
		return vault.RegisterVaultRestoreApp(srv, catalog, deps.handoffReg, deps.authHandles)
	}))

	// vault_status stays headless (returns raw JSON); open_vault_browser is the
	// ONLY tool that opens the Vault browser app view.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultBrowserToolName,
		Title:       "Vault Browser (App)",
		Description: "Open the interactive Vault browser app. This is a UI launcher: it renders an iframe for a human to browse the vault. It is not a headless primitive. Prefer vault_status / vault_ls (headless) for autonomous access.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultBrowserAppURI,
	}), vault.RegisterVaultBrowserApp))

	// pins_list stays headless (returns raw JSON); open_pin_list is the ONLY
	// tool that opens the Pin list app view.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        download.OpenPinListToolName,
		Title:       "Pin List (App)",
		Description: "Open the interactive Pin list app. This is a UI launcher: it renders an iframe for a human to browse pins. It is not a headless primitive. Prefer pins_list (headless) for autonomous access.",
		Category:    model.CategoryCore,
		ResourceURI: download.PinListAppURI,
	}), download.RegisterPinListApp))

	// auth_status stays headless (returns raw JSON); open_account is the ONLY
	// tool that opens the Account app view.
	reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountToolName,
		Title:       "Account (App)",
		Description: "Open the interactive Account app. This is a UI launcher: it renders an iframe for a human to view authentication status. It is not a headless primitive. Prefer auth_status (headless) for autonomous access.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AuthStatusAppURI,
	}), auth.RegisterAuthStatusApp))

	// pinner:// resources and templates are built from the provider factory and
	// projected after the direct tool surface.
	if deps.resourceFactory != nil {
		reg.afterSurface(func() error {
			provs := deps.resourceFactory(deps.store)
			provs.Sessions = deps.store
			resources, templates := ResourceDescriptors(provs)
			return sdk.RegisterResources(deps.srv, resources, templates)
		})
	}

	// --- Vault put file (unified, transport-aware) ---
	if vaultPutFileAvailable(deps.coLocated, opts.localPathVaultPut != nil, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI) {
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
		vaultPutSpec := customToolSpec{desc: vaultPutDesc, direct: true}
		if deps.vaultUpload != nil {
			vaultPutSpec.index = true
			reg.add(launcherSpec(upload.NewOpenVaultManagerDescriptor(deps.vaultUpload), func(srv *sdk.Server, catalog apps.AppCatalog) error {
				return upload.RegisterVaultUploadApp(srv, catalog, deps.vaultUpload)
			}))
		}
		reg.add(vaultPutSpec)
	}

	// --- upload_url: relay a caller-supplied HTTPS URL (remote HTTP fallback) ---
	if opts.relayURLUpload != nil {
		reg.add(customToolSpec{
			desc:   upload.RelayURLUploadDescriptor(opts.relayURLUpload, opts.relayAllowedHosts, opts.maxRelayBytes),
			index:  true,
			direct: true,
		})
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
	if opts.ipfsDownload != nil {
		downloadRoot := transfer.ResolveDownloadRoot(opts.downloadRoot)
		dlDesc := transfer.NewDownloadFileDescriptor(opts.ipfsDownload, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		// download_file is headless; the app's view attaches to the explicit
		// open_download_manager launcher.
		reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        download.OpenDownloadManagerToolName,
			Title:       "Download from IPFS",
			Description: "Open the interactive Download from IPFS app. This is a UI launcher: it renders an iframe for a human to initiate a download. It is not a headless primitive. Prefer download_file (headless) for autonomous workflows; call this only when a human downloader is desired.",
			Category:    model.CategoryCore,
			ResourceURI: download.IPFSDownloadAppURI,
		}), download.RegisterIPFSDownloadApp))
		reg.add(customToolSpec{desc: dlDesc, index: true, direct: true})
	}

	// --- Consolidated vault_get_file: a single sink-aware vault download tool. ---
	if opts.vaultGet != nil {
		downloadRoot := transfer.ResolveDownloadRoot(opts.downloadRoot)
		dlDesc := vault.NewVaultGetFileDescriptor(opts.vaultGet, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		// vault_get_file is headless; the app's view attaches to the explicit
		// open_vault_download_manager launcher.
		reg.add(launcherSpec(apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        download.OpenVaultDownloadManagerToolName,
			Title:       "Download from Vault",
			Description: "Open the interactive Download from Vault app. This is a UI launcher: it renders an iframe for a human to initiate a vault download. It is not a headless primitive. Prefer vault_get_file (headless) for autonomous workflows; call this only when a human vault downloader is desired.",
			Category:    model.CategoryVault,
			ResourceURI: download.VaultDownloadAppURI,
		}), download.RegisterVaultDownloadApp))
		reg.add(customToolSpec{desc: dlDesc, index: true, direct: true})
	}

	// --- upload_data: SEP-2356 data: URI relay (draft x-mcp-file mode) ---
	if opts.dataURIUpload != nil {
		reg.add(customToolSpec{
			desc:   transfer.DataURIUploadDescriptor(opts.dataURIUpload, opts.maxRelayBytes),
			index:  true,
			direct: true,
		})
	}

	// --- Consolidated upload_file: a single transport-aware IPFS upload tool. ---
	// The caller does not pick a mechanism — registration routes by transport:
	//   - co-located (stdio/local): source mode path via the local-path
	//     handler (opts.localPathUpload).
	//   - remote (HTTP/tunnel): source mode mint via the presigned Upload
	//     coordinator (deps.curlUpload).
	//   - openai tunnel: source mode url/data via the file-relay executor
	//     (opts.uploadHandler), since no reachable HTTP mux exists.
	if uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI) {
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
		uploadFileSpec := customToolSpec{desc: uploadFileDesc, direct: true}
		if deps.curlUpload != nil {
			uploadFileSpec.index = true
			reg.add(launcherSpec(upload.NewOpenUploadManagerDescriptor(deps.curlUpload), func(srv *sdk.Server, catalog apps.AppCatalog) error {
				return upload.RegisterIPFSUploadApp(srv, catalog, deps.curlUpload)
			}))
		}
		reg.add(uploadFileSpec)
	}

	// --- Async upload management tools (upload_status / upload_cancel / upload_list) ---
	if opts.uploadTasks != nil {
		for _, desc := range upload.NewAsyncUploadTools(opts.uploadTasks) {
			reg.add(customToolSpec{desc: desc, index: true, direct: true})
		}
	}

	// Always expose capability detection so hosts can choose a file-input mode
	// without assuming draft MCP file support is negotiated. Each capability
	// reflects whether its handler is actually wired. A dedicated per-host
	// server re-resolves the baked description against the detected profile so
	// tools/list never promises a `file` parameter the host cannot fill. The
	// re-resolve threads the same tool-wiring flags so the description drops the
	// file-handoff prose when no upload/vault tool is wired, matching the report.
	uploadWired := uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI)
	vaultWired := vaultPutFileAvailable(deps.coLocated, opts.localPathVaultPut != nil, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI)
	capDesc := NewCapabilitiesDescriptor(
		deps.coLocated,
		deps.tunnelOpenAI,
		uploadWired,
		vaultWired,
		opts.ipfsDownload != nil,
		opts.vaultGet != nil,
		deps.downloadDrop != nil,
		opts.dataURIUpload != nil, // the data: URI upload tool carries the draft x-mcp-file metadata
		opts.maxRelayBytes,
	)
	if deps.hostProfile != nil {
		capDesc.Description = capabilitiesDescriptionFor(*deps.hostProfile, uploadWired, vaultWired)
	}
	reg.add(customToolSpec{desc: capDesc, direct: true})

	// Always expose the static agent guide so a model can orient to the
	// primary flows without probing each tool's description.
	reg.add(customToolSpec{desc: NewAgentGuideDescriptor(), direct: true})

	// Optionally expose the prompt templates.
	if opts.prompts {
		reg.afterSurface(func() error {
			return sdk.RegisterPrompts(deps.srv, PromptDescriptors())
		})
	}

	return reg.run()
}

// uploadFileAvailable reports whether the consolidated upload_file tool has at
// least one real file-input branch for the running transport:
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
	if coLocated {
		return localPathWired
	}
	if curlWired && !tunnelOpenAI {
		return true
	}
	return tunnelOpenAI && relayWired
}

// vaultPutFileAvailable reports whether the unified vault_put_file tool has at
// least one usable branch for the running transport, mirroring
// uploadFileAvailable for the vault surface:
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
	if coLocated {
		return localPathWired
	}
	if mintWired && !tunnelOpenAI {
		return true
	}
	return tunnelOpenAI && relayWired
}

// registerOpenLauncher registers a model-facing open_* UI launcher tool. A
// launcher is an explicit, intentional way to open an MCP App view: it carries
// _meta.ui.resourceUri so a supporting host renders the app's iframe, and it
// is model-visible so the agent can choose to open the app. It is the ONLY tool
// that advertises the app's resourceUri — the operational primitives the app is
// attached to (upload_file, vault_status, pins_list, ...) remain headless (no
// resourceUri), so ordinary mid-workflow calls never render a card.
//
// The launcher is added to the catalog (for progressive discovery) AND
// registered directly on tools/list (model-visible). The app's RegisterAppView
// later attaches its _meta.ui to this launcher's catalog entry (via AttachTo),
// which is exactly where the UI linkage should live.
//
// Production registration routes launchers through the customToolRegistry
// (launcherSpec); this helper remains for tests and callers that register a
// single launcher immediately.
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
