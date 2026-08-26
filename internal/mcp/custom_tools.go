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
//   - the "Create a Pin" MCP App (ui:// view + app-only status helper)
//   - the agent-facing out-of-band sign-in tools, which join the catalog as
//     DirectVisible tools instead of a separate RegisterOfficialDescriptor path
//   - markCurated + RegisterOfficialCuratedTools, the direct tools/list surface
//   - pinner:// resources and templates
//   - the upload-backend tools (relay URL, data URI, async)
//   - the capability-detection tool and (optionally) the prompt templates
func registerCustomTools(deps customToolDeps) error {
	if deps.hasWizard {
		if err := wizard.RegisterWizardTools(deps.catalog, deps.store, deps.wizardW, deps.wizardS, deps.wizardD); err != nil {
			return err
		}
	}

	// Resolve options before registering curated tools so App wiring (which
	// must attach _meta.ui to the pin tool in the catalog BEFORE the curated
	// loop registers it) can read provider factories.
	if deps.opts != nil && deps.opts.pinnerPins != nil {
		pins, err := deps.opts.pinnerPins()
		if err != nil {
			return fmt.Errorf("failed to build pinning provider: %w", err)
		}
		// pins_add stays headless; open_pin_creator is the ONLY tool that
		// opens the Create a Pin app view.
		if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        apps.OpenPinCreatorToolName,
			Title:       "Create a Pin",
			Description: "Open the interactive Create a Pin app. This is a UI launcher: it renders an iframe for a human to enter a CID and pin it. It is not a headless primitive. Prefer pins_add (headless) for autonomous workflows; call this only when a human-facing pin form is desired.",
			Category:    model.CategoryCore,
			ResourceURI: apps.PinCreateAppURI,
		})); err != nil {
			return fmt.Errorf("failed to register pin creator launcher: %w", err)
		}
		if err := apps.RegisterPinApp(deps.srv, deps.catalog, pins); err != nil {
			return fmt.Errorf("failed to register pin app: %w", err)
		}
	}

	// Agent-facing out-of-band sign-in tools (start + resume) are part of the
	// direct surface AND indexed for progressive discovery. Adding them to the
	// catalog with DirectVisible before the curated loop means a single
	// registration path (the DirectVisible scan in RegisterOfficialCuratedTools)
	// exposes them on tools/list while the catalog entry supplies
	// search/describe/invoke. When the wizard transport is absent oob is nil
	// and both tools return a structured not-configured hand-off instead of
	// hanging.
	authSSO := auth.NewAuthSSODescriptor(deps.oob, deps.authHandles, deps.handoffReg)
	authSSO.DirectVisible = true
	authResume := auth.NewAuthResumeDescriptor(deps.handoffReg, deps.authHandles)
	authResume.DirectVisible = true
	deps.catalog.Add(model.ToolEntryFromDescriptor(authSSO))
	deps.catalog.Add(model.ToolEntryFromDescriptor(authResume))

	// auth_sso stays headless (it returns a needs_human URL+handle handoff);
	// open_sso_signin is the ONLY tool that opens the Sign In app view.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenSSOSigninToolName,
		Title:       "Sign In (App)",
		Description: "Open the interactive Sign In app. This is a UI launcher: it renders an iframe for a human to complete SSO approval. It is not a headless primitive. Prefer auth_sso (headless) for autonomous sign-in, which returns the approval URL + resume handle without rendering a card.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AuthSSOAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register SSO launcher: %w", err)
	}
	if err := auth.RegisterAuthSSOApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register auth SSO app: %w", err)
	}

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
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountUpdate))
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountReset))
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountEmail))

	// account_password_update / account_email_change stay headless (they return
	// a needs_human URL handoff); open_account_password / open_account_email
	// are the ONLY tools that open their one-shot deep-link app views.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountPasswordToolName,
		Title:       "Change Password (App)",
		Description: "Open the interactive Change Password app. This is a UI launcher: it renders an iframe for a human to change their password. It is not a headless primitive. Prefer account_password_update (headless) for autonomous flows.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AccountPasswordAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register account password launcher: %w", err)
	}
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountEmailToolName,
		Title:       "Change Email (App)",
		Description: "Open the interactive Change Email app. This is a UI launcher: it renders an iframe for a human to change their email. It is not a headless primitive. Prefer account_email_change (headless) for autonomous flows.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AccountEmailAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register account email launcher: %w", err)
	}
	if err := auth.RegisterAccountPasswordApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register account password app: %w", err)
	}
	if err := auth.RegisterAccountEmailApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register account email app: %w", err)
	}

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
	deps.catalog.Add(model.ToolEntryFromDescriptor(vaultCreateResume))
	deps.catalog.Add(model.ToolEntryFromDescriptor(vaultRestoreResume))

	// vault_create / vault_restore stay headless (they return a needs_human
	// URL+handle handoff); open_vault_create / open_vault_restore are the ONLY
	// tools that open their app views.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultCreateToolName,
		Title:       "Create Vault (App)",
		Description: "Open the interactive Create Vault app. This is a UI launcher: it renders an iframe for a human to create a vault (Sia approval + recovery seed). It is not a headless primitive. Prefer vault_create (headless) which returns the create URL + resume handle without rendering a card.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultCreateAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register vault create launcher: %w", err)
	}
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultRestoreToolName,
		Title:       "Restore Vault (App)",
		Description: "Open the interactive Restore Vault app. This is a UI launcher: it renders an iframe for a human to restore a vault from its recovery seed. It is not a headless primitive. Prefer vault_restore (headless) which returns the restore URL + resume handle without rendering a card.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultRestoreAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register vault restore launcher: %w", err)
	}
	if err := vault.RegisterVaultCreateApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register vault create app: %w", err)
	}
	if err := vault.RegisterVaultRestoreApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register vault restore app: %w", err)
	}

	// vault_status stays headless (returns raw JSON); open_vault_browser is the
	// ONLY tool that opens the Vault browser app view.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        vault.OpenVaultBrowserToolName,
		Title:       "Vault Browser (App)",
		Description: "Open the interactive Vault browser app. This is a UI launcher: it renders an iframe for a human to browse the vault. It is not a headless primitive. Prefer vault_status / vault_ls (headless) for autonomous access.",
		Category:    model.CategoryVault,
		ResourceURI: vault.VaultBrowserAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register vault browser launcher: %w", err)
	}
	if err := vault.RegisterVaultBrowserApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register vault browser app: %w", err)
	}

	// pins_list stays headless (returns raw JSON); open_pin_list is the ONLY
	// tool that opens the Pin list app view.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        download.OpenPinListToolName,
		Title:       "Pin List (App)",
		Description: "Open the interactive Pin list app. This is a UI launcher: it renders an iframe for a human to browse pins. It is not a headless primitive. Prefer pins_list (headless) for autonomous access.",
		Category:    model.CategoryCore,
		ResourceURI: download.PinListAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register pin list launcher: %w", err)
	}
	if err := download.RegisterPinListApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register pin list app: %w", err)
	}

	// auth_status stays headless (returns raw JSON); open_account is the ONLY
	// tool that opens the Account app view.
	if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        auth.OpenAccountToolName,
		Title:       "Account (App)",
		Description: "Open the interactive Account app. This is a UI launcher: it renders an iframe for a human to view authentication status. It is not a headless primitive. Prefer auth_status (headless) for autonomous access.",
		Category:    model.CategoryAccount,
		ResourceURI: auth.AuthStatusAppURI,
	})); err != nil {
		return fmt.Errorf("failed to register account launcher: %w", err)
	}
	if err := auth.RegisterAuthStatusApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register auth status app: %w", err)
	}

	// Stamp which tools are part of the direct tools/list surface. This must
	// run after the wizard tools and SSO tools are added to the catalog (both
	// are created after buildCatalog returns), so the compiled curated names
	// are all present before visibility is marked.
	markCurated(deps.catalog)

	if err := RegisterOfficialCuratedTools(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register curated tools: %w", err)
	}

	if deps.resourceFactory != nil {
		provs := deps.resourceFactory(deps.store)
		provs.Sessions = deps.store
		resources, templates := ResourceDescriptors(provs)
		if err := sdk.RegisterResources(deps.srv, resources, templates); err != nil {
			return err
		}
	}

	opts := deps.opts
	if opts == nil {
		opts = &mcpServerOptions{}
	}
	if vaultPutFileAvailable(deps.coLocated, opts.localPathVaultPut != nil, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI) {
		var pathFn vault.LocalPathVaultPutHandler
		if deps.coLocated {
			pathFn = opts.localPathVaultPut
		}
		vaultPutDesc := vault.NewVaultPutFileDescriptor(deps.coLocated, deps.tunnelOpenAI, pathFn, deps.vaultUpload, opts.vaultPutHandler, opts.relayAllowedHosts, opts.maxRelayBytes)
		// A dedicated per-host server re-resolves the tool description against
		// the detected host profile (e.g. an OpenAI-over-HTTP host sees the
		// `file` handoff even though the startup HTTP transport bakes the
		// mint-only description). The schema and handler stay transport-bound.
		if deps.hostProfile != nil {
			if d, ok := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, *deps.hostProfile); ok {
				vaultPutDesc.Description = d
			}
		}
		// Pair vault_put_file with its "Upload to Vault" MCP App view
		// (ui://uploads/vault.html) when the presigned vault-upload coordinator
		// can mint a PUT endpoint for the Uppy XHR uploader. The app must be
		// indexed in the catalog before its view attaches _meta.ui.
		// Index vault_put_file in the catalog and register the vault upload app,
		// but do NOT copy the app's _meta.ui onto the served descriptor.
		// vault_put_file is a headless operational primitive: attaching a
		// ui:// resourceUri would cause a UI-capable host to render the
		// file-picker iframe on every invocation, which is the wrong UX for
		// mid-workflow agent calls. The App is reachable only through the
		// explicit open_vault_manager launcher (below).
		if deps.vaultUpload != nil {
			// open_vault_manager must be catalog-indexed BEFORE
			// RegisterVaultUploadApp runs: the app's AttachTo resolves the
			// launcher from the catalog to stamp _meta.ui on it. registerOpenLauncher
			// publishes it both to the catalog (for AttachTo + curated discovery)
			// and to the direct descriptor surface. vault_put_file itself stays
			// headless — this launcher is the ONLY tool carrying the App's
			// resourceUri.
			if err := registerOpenLauncher(deps, upload.NewOpenVaultManagerDescriptor(deps.vaultUpload)); err != nil {
				return fmt.Errorf("failed to register vault manager launcher: %w", err)
			}
			deps.catalog.Add(model.ToolEntryFromDescriptor(vaultPutDesc))
			if err := upload.RegisterVaultUploadApp(deps.srv, deps.catalog, deps.vaultUpload); err != nil {
				return err
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, vaultPutDesc); err != nil {
			return err
		}
	}
	if opts.relayURLUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, upload.RelayURLUploadDescriptor(opts.relayURLUpload, opts.relayAllowedHosts, opts.maxRelayBytes)); err != nil {
			return err
		}
	}
	// Consolidated download_file: a single sink-aware IPFS download tool.
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
		// Pair download_file with its "Download from IPFS" MCP App view
		// (ui://downloads/ipfs.html) so a UI-capable host renders a download
		// panel. RegisterAppView attaches _meta.ui to a catalog entry, so the
		// tool must be indexed first. Like upload_file, the app (sink=local or
		// sink=drop) is meaningful on every transport, so it is always paired
		// when the tool is registered.
		deps.catalog.Add(model.ToolEntryFromDescriptor(dlDesc))
		// download_file is a headless primitive: it never carries
		// ui.resourceUri on the served descriptor. The app's UI view is
		// attached to the explicit open_download_manager launcher below, so
		// mid-workflow download calls do not render a card.
		if err := RegisterOfficialDescriptor(deps.srv, dlDesc); err != nil {
			return err
		}
		if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        download.OpenDownloadManagerToolName,
			Title:       "Download from IPFS",
			Description: "Open the interactive Download from IPFS app. This is a UI launcher: it renders an iframe for a human to initiate a download. It is not a headless primitive. Prefer download_file (headless) for autonomous workflows; call this only when a human downloader is desired.",
			Category:    model.CategoryCore,
			ResourceURI: download.IPFSDownloadAppURI,
		})); err != nil {
			return err
		}
		if err := download.RegisterIPFSDownloadApp(deps.srv, deps.catalog); err != nil {
			return err
		}
	}
	// Consolidated vault_get_file: a single sink-aware vault download tool.
	//   - sink=local (every transport): opts.vaultGet streams the encrypted
	//     vault file's decrypted bytes to a host-side path.
	//   - sink=drop (HTTP / real tunnel): deps.downloadDrop mints a filedrop.
	if opts.vaultGet != nil {
		downloadRoot := transfer.ResolveDownloadRoot(opts.downloadRoot)
		dlDesc := vault.NewVaultGetFileDescriptor(opts.vaultGet, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		deps.catalog.Add(model.ToolEntryFromDescriptor(dlDesc))
		// vault_get_file is a headless primitive. The app's UI view is
		// attached to the explicit open_vault_download_manager launcher.
		if err := RegisterOfficialDescriptor(deps.srv, dlDesc); err != nil {
			return err
		}
		if err := registerOpenLauncher(deps, apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
			Name:        download.OpenVaultDownloadManagerToolName,
			Title:       "Download from Vault",
			Description: "Open the interactive Download from Vault app. This is a UI launcher: it renders an iframe for a human to initiate a vault download. It is not a headless primitive. Prefer vault_get_file (headless) for autonomous workflows; call this only when a human vault downloader is desired.",
			Category:    model.CategoryVault,
			ResourceURI: download.VaultDownloadAppURI,
		})); err != nil {
			return err
		}
		if err := download.RegisterVaultDownloadApp(deps.srv, deps.catalog); err != nil {
			return err
		}
	}
	if opts.dataURIUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, transfer.DataURIUploadDescriptor(opts.dataURIUpload, opts.maxRelayBytes)); err != nil {
			return err
		}
	}
	// Consolidated upload_file: a single transport-aware IPFS upload tool.
	// The caller does not pick a mechanism — registration routes by transport.
	//   - co-located (stdio/local): source mode path via the local-path
	//     handler (opts.localPathUpload).
	//   - remote (HTTP/tunnel): source mode mint via the presigned Upload
	//     coordinator (deps.curlUpload).
	//   - openai tunnel: source mode url/data via the file-relay executor
	//     (opts.uploadHandler), since no reachable HTTP mux exists.
	// Register it whenever at least one upload path is available for the
	// running transport.
	if uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI) {
		var pathFn transfer.UploadFileHandler
		if deps.coLocated {
			pathFn = opts.localPathUpload
		}
		// relayFn is the authenticated file executor for the openai-tunnel
		// url/data source modes.
		uploadFileDesc := transfer.NewUploadFileDescriptor(deps.coLocated, deps.tunnelOpenAI, pathFn, deps.curlUpload, opts.uploadHandler, opts.relayAllowedHosts, opts.maxRelayBytes)
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

		// Pair upload_file with its "Upload to IPFS" MCP App view
		// (ui://uploads/ipfs.html) so a UI-capable host renders a file-picker
		// panel. RegisterAppView attaches _meta.ui to a catalog entry, so the
		// tool must be indexed first. The app is only meaningfully available
		// when a presigned Upload coordinator can mint a PUT endpoint for
		// the Uppy XHR uploader (deps.curlUpload != nil); in co-located stdio
		// local-path mode there is no presigned endpoint, so the app is not
		// registered and upload_file simply serves the out-of-band local path
		// surface. Gating on both uploadFileAvailable and curlUpload != nil
		// keeps attachAppMeta from ever running when the tool is absent (e.g.
		// --tunnel openai) or when no mint URL could be produced.
		// Index upload_file in the catalog and register the companion upload app,
		// but do NOT copy the app's _meta.ui onto the served descriptor.
		// upload_file is a headless operational primitive: attaching a
		// ui:// resourceUri would cause a UI-capable host to render the
		// file-picker iframe on every invocation, which is the wrong UX for
		// mid-workflow agent calls. The App is reachable only through the
		// explicit open_upload_manager launcher (below).
		if deps.curlUpload != nil {
			// open_upload_manager must be catalog-indexed BEFORE
			// RegisterIPFSUploadApp runs: the app's AttachTo resolves the
			// launcher from the catalog to stamp _meta.ui on it. registerOpenLauncher
			// publishes it both to the catalog (for AttachTo + curated discovery)
			// and to the direct descriptor surface. upload_file itself stays
			// headless — this launcher is the ONLY tool carrying the App's
			// resourceUri.
			if err := registerOpenLauncher(deps, upload.NewOpenUploadManagerDescriptor(deps.curlUpload)); err != nil {
				return fmt.Errorf("failed to register upload manager launcher: %w", err)
			}
			deps.catalog.Add(model.ToolEntryFromDescriptor(uploadFileDesc))
			if err := upload.RegisterIPFSUploadApp(deps.srv, deps.catalog, deps.curlUpload); err != nil {
				return err
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, uploadFileDesc); err != nil {
			return err
		}
	}
	if opts.uploadTasks != nil {
		for _, desc := range upload.NewAsyncUploadTools(opts.uploadTasks) {
			if err := RegisterOfficialDescriptor(deps.srv, desc); err != nil {
				return err
			}
		}
	}
	// Always expose capability detection so hosts can choose a file-input mode
	// without assuming draft MCP file support is negotiated. Each capability
	// reflects whether its handler is actually wired.
	if err := RegisterOfficialDescriptor(deps.srv, NewCapabilitiesDescriptor(
		deps.coLocated,
		deps.tunnelOpenAI,
		uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI),
		vaultPutFileAvailable(deps.coLocated, opts.localPathVaultPut != nil, deps.vaultUpload != nil, opts.vaultPutHandler != nil, deps.tunnelOpenAI),
		opts.ipfsDownload != nil,
		opts.vaultGet != nil,
		deps.downloadDrop != nil,
		opts.dataURIUpload != nil, // the data: URI upload tool carries the draft x-mcp-file metadata
		opts.maxRelayBytes,
	)); err != nil {
		return err
	}
	// Always expose the static agent guide so a model can orient to the
	// primary flows without probing each tool's description.
	if err := RegisterOfficialDescriptor(deps.srv, NewAgentGuideDescriptor()); err != nil {
		return err
	}
	if opts.prompts {
		if err := sdk.RegisterPrompts(deps.srv, PromptDescriptors()); err != nil {
			return err
		}
	}
	return nil
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
