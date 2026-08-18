package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
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
	oob         *OutOfBandLogin
	authHandles *session.AsyncHandleStore
	handoffReg  *handoff.HandoffRegistry
	// seedDrop, oobRestore, and oobCreate back the vault create/restore OOB
	// hand-offs. seedDrop is the vault-create seed-drop coordinator, oobRestore
	// is the vault-restore coordinator, and oobCreate is the vault-create
	// coordinator. They are threaded here so the resume tools
	// (vault_create_resume / vault_restore_resume) can poll the
	// coordinators to completion over the same shared handoffReg + handles.
	seedDrop   *SeedDrop
	oobRestore *OOBRestore
	oobCreate  *OOBCreate
	// curlUpload, when non-nil, backs the presigned HTTP PUT upload route (the
	// httpUpload coordinator): it mints a one-time endpoint whose PUT body
	// streams into the async UploadTaskManager. It feeds the consolidated
	// upload_file tool in remote (HTTP/tunnel) mode.
	curlUpload *httpUpload
	// vaultUpload, when non-nil, backs the presigned HTTP PUT vault-write route
	// (the vaultHTTPUpload coordinator). It mints a one-time endpoint bound to
	// a destination vault path whose PUT body streams into the authenticated
	// vault write synchronously. It feeds the "Upload to Vault" MCP App.
	vaultUpload *vaultHTTPUpload
	// downloadDrop, when non-nil, backs the one-time filedrop GET route (the
	// httpDownload coordinator). It serves downloaded bytes out of band to a
	// consumer that shares no disk with the server. It feeds the access
	// download_file / vault_get_file drop branches.
	downloadDrop *httpDownload
	// accountOOB backs the out-of-band account credential change coordinator
	// (hosted browser forms -> authenticated UpdatePassword/UpdateEmail). It
	// enforces an authenticated session; the secret never transits the MCP/LLM
	// channel.
	accountOOB *OOBAccountChange
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
	// wizard deps are built from wizardFactory at Action time. All three are
	// nil when no wizard factory is configured.
	hasWizard bool
	wizardW   WebsitesWizardDeps
	wizardS   SetupWizardDeps
	wizardD   DomainWizardDeps
}

// registerCustomTools registers every custom/direct tool, resource, and prompt
// onto the server. It is the single named home for the adhoc registration that
// used to live inline in the MCPCommand transport closure, so the wiring is
// cognitively isolated from the server pump:
//
//   - wizard tools (RegisterWizardTools) — sessions + step handlers
//   - the "Create a Pin" MCP App (ui:// view + app-only status helper)
//   - the agent-facing out-of-band sign-in tools, which join the catalog as
//     DirectVisible tools instead of a separate RegisterOfficialDescriptor path
//   - markCurated + RegisterOfficialCuratedTools, the direct tools/list surface
//   - pinner:// resources and templates
//   - the upload-backend tools (relay URL, data URI, async)
//   - the capability-detection tool and (optionally) the prompt templates
func registerCustomTools(deps customToolDeps) error {
	if deps.hasWizard {
		if err := RegisterWizardTools(deps.catalog, deps.store, deps.wizardW, deps.wizardS, deps.wizardD); err != nil {
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
		if err := RegisterPinApp(deps.srv, deps.catalog, pins); err != nil {
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
	authSSO := NewAuthSSODescriptor(deps.oob, deps.authHandles, deps.handoffReg)
	authSSO.DirectVisible = true
	authResume := NewAuthResumeDescriptor(deps.handoffReg, deps.authHandles)
	authResume.DirectVisible = true
	deps.catalog.Add(model.ToolEntryFromDescriptor(authSSO))
	deps.catalog.Add(model.ToolEntryFromDescriptor(authResume))

	// Pair the auth_sso tool with its "Sign In" MCP App view (ui://auth/sso.html)
	// so a UI-capable host renders the SSO approval in a panel. This must run
	// after auth_sso is added to the catalog (AttachTo requires it) and before
	// the curated registration loop reads _meta.ui.
	if err := RegisterAuthSSOApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register auth SSO app: %w", err)
	}

	// Out-of-band account credential tools: change the password (hosted browser
	// form -> authenticated UpdatePassword, requires an authenticated session)
	// and reset the password via an emailed link to the webapp. Direct-surface
	// tools like the SSO pair; when the coordinator/service are absent they
	// return a structured not-configured hand-off instead of hanging.
	accountUpdate := NewAccountPasswordUpdateDescriptor(deps.accountOOB, deps.wizardS.AuthService, deps.authHandles, deps.handoffReg)
	accountUpdate.DirectVisible = true
	accountReset := NewAccountPasswordResetDescriptor(deps.wizardS.AuthService, deps.accountWebAppURL)
	accountReset.DirectVisible = true
	accountEmail := NewAccountEmailChangeDescriptor(deps.accountOOB, deps.wizardS.AuthService)
	accountEmail.DirectVisible = true
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountUpdate))
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountReset))
	deps.catalog.Add(model.ToolEntryFromDescriptor(accountEmail))

	// Pair the account_password_update / account_email_change tools with their
	// "Change Password" / "Change Email" MCP App views (ui://account/password.html
	// / ui://account/email.html) so a UI-capable host renders the one-shot deep
	// link in a panel. These are link apps with no poll loop: the change runs
	// synchronously in the browser. Must run after the tools are added (AttachTo
	// requires them) and before the curated registration loop reads _meta.ui.
	if err := RegisterAccountPasswordApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register account password app: %w", err)
	}
	if err := RegisterAccountEmailApp(deps.srv, deps.catalog); err != nil {
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
	vaultCreateResume := NewVaultCreateResumeDescriptor(deps.handoffReg, deps.authHandles)
	vaultCreateResume.DirectVisible = true
	vaultRestoreResume := NewVaultRestoreResumeDescriptor(deps.handoffReg, deps.authHandles)
	vaultRestoreResume.DirectVisible = true
	deps.catalog.Add(model.ToolEntryFromDescriptor(vaultCreateResume))
	deps.catalog.Add(model.ToolEntryFromDescriptor(vaultRestoreResume))

	// Pair vault_create / vault_restore with their "Create Vault" / "Restore
	// Vault" MCP App views (ui://vault/create.html / ui://vault/restore.html)
	// so a UI-capable host renders the flows in a panel. The compiled
	// vault_create / vault_restore tools are in the catalog from buildCatalog.
	// Must run before the curated registration loop reads _meta.ui.
	if err := RegisterVaultCreateApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register vault create app: %w", err)
	}
	if err := RegisterVaultRestoreApp(deps.srv, deps.catalog, deps.handoffReg, deps.authHandles); err != nil {
		return fmt.Errorf("failed to register vault restore app: %w", err)
	}

	// Pair the read-only vault_status tool with the "Vault browser" MCP App view
	// (ui://vault/browser.html) so a UI-capable host renders a readable status +
	// listing panel. The view only reads via the existing vault_status /
	// vault_ls catalog tools and registers no helper. Must run before the
	// curated registration loop reads _meta.ui, like the create/restore apps.
	if err := RegisterVaultBrowserApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register vault browser app: %w", err)
	}

	// Pair the read-only pins_list tool with the "Pin list" MCP App view
	// (ui://pins/list.html) so a UI-capable host renders a readable table of
	// the account's pins and their status. The view only reads via the
	// existing pins_list catalog tool and registers no helper. Must run before
	// the curated registration loop reads _meta.ui, like the other apps.
	if err := RegisterPinListApp(deps.srv, deps.catalog); err != nil {
		return fmt.Errorf("failed to register pin list app: %w", err)
	}

	// Pair the read-only auth_status tool with the "Account" MCP App view
	// (ui://auth/status.html) so a UI-capable host renders a readable
	// authentication/account strip. The view only reads via the existing
	// auth_status catalog tool and registers no helper. Must run before the
	// curated registration loop reads _meta.ui, like the other apps.
	if err := RegisterAuthStatusApp(deps.srv, deps.catalog); err != nil {
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
		var pathFn LocalPathVaultPutHandler
		if deps.coLocated {
			pathFn = opts.localPathVaultPut
		}
		vaultPutDesc := NewVaultPutFileDescriptor(deps.coLocated, deps.tunnelOpenAI, pathFn, deps.vaultUpload, opts.vaultPutHandler, opts.relayAllowedHosts, opts.maxRelayBytes)
		// Pair vault_put_file with its "Upload to Vault" MCP App view
		// (ui://uploads/vault.html) when the presigned vault-upload coordinator
		// can mint a PUT endpoint for the Uppy XHR uploader. The app must be
		// indexed in the catalog before its view attaches _meta.ui.
		if deps.vaultUpload != nil {
			deps.catalog.Add(model.ToolEntryFromDescriptor(vaultPutDesc))
			if err := RegisterVaultUploadApp(deps.srv, deps.catalog, deps.vaultUpload); err != nil {
				return err
			}
			// Copy the app-view _meta (registered above onto the catalog entry)
			// onto the descriptor served directly to hosts, so a UI-capable host
			// reading vault_put_file from tools/list still sees the file-picker
			// panel. The direct surface and the catalog entry are distinct
			// objects; without this copy the direct tool would miss _meta.ui.
			if entry, ok := deps.catalog.Get("vault_put_file"); ok {
				if vaultPutDesc.Meta == nil {
					vaultPutDesc.Meta = map[string]any{}
				}
				for k, v := range entry.Meta {
					vaultPutDesc.Meta[k] = v
				}
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, vaultPutDesc); err != nil {
			return err
		}
	}
	if opts.relayURLUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, RelayURLUploadDescriptor(opts.relayURLUpload, opts.relayAllowedHosts, opts.maxRelayBytes)); err != nil {
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
		downloadRoot := resolveDownloadRoot(opts.downloadRoot)
		dlDesc := NewDownloadFileDescriptor(opts.ipfsDownload, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		// Pair download_file with its "Download from IPFS" MCP App view
		// (ui://downloads/ipfs.html) so a UI-capable host renders a download
		// panel. RegisterAppView attaches _meta.ui to a catalog entry, so the
		// tool must be indexed first. Like upload_file, the app (sink=local or
		// sink=drop) is meaningful on every transport, so it is always paired
		// when the tool is registered.
		deps.catalog.Add(model.ToolEntryFromDescriptor(dlDesc))
		if err := RegisterIPFSDownloadApp(deps.srv, deps.catalog); err != nil {
			return err
		}
		// Copy the app-view _meta (registered above onto the catalog entry)
		// onto the descriptor served directly to hosts, so a UI-capable host
		// reading download_file from tools/list still sees the panel.
		if entry, ok := deps.catalog.Get("download_file"); ok {
			if dlDesc.Meta == nil {
				dlDesc.Meta = map[string]any{}
			}
			for k, v := range entry.Meta {
				dlDesc.Meta[k] = v
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, dlDesc); err != nil {
			return err
		}
	}
	// Consolidated vault_get_file: a single sink-aware vault download tool.
	//   - sink=local (every transport): opts.vaultGet streams the encrypted
	//     vault file's decrypted bytes to a host-side path.
	//   - sink=drop (HTTP / real tunnel): deps.downloadDrop mints a filedrop.
	if opts.vaultGet != nil {
		downloadRoot := resolveDownloadRoot(opts.downloadRoot)
		dlDesc := NewVaultGetFileDescriptor(opts.vaultGet, deps.downloadDrop, downloadRoot, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes), deps.tunnelOpenAI)
		deps.catalog.Add(model.ToolEntryFromDescriptor(dlDesc))
		if err := RegisterVaultDownloadApp(deps.srv, deps.catalog); err != nil {
			return err
		}
		if entry, ok := deps.catalog.Get("vault_get_file"); ok {
			if dlDesc.Meta == nil {
				dlDesc.Meta = map[string]any{}
			}
			for k, v := range entry.Meta {
				dlDesc.Meta[k] = v
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, dlDesc); err != nil {
			return err
		}
	}
	if opts.dataURIUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, DataURIUploadDescriptor(opts.dataURIUpload, opts.maxRelayBytes)); err != nil {
			return err
		}
	}
	// Consolidated upload_file: a single transport-aware IPFS upload tool.
	// The caller does not pick a mechanism — registration routes by transport.
	//   - co-located (stdio/local): source mode path via the local-path
	//     handler (opts.localPathUpload).
	//   - remote (HTTP/tunnel): source mode mint via the presigned httpUpload
	//     coordinator (deps.curlUpload).
	//   - openai tunnel: source mode url/data via the file-relay executor
	//     (opts.uploadHandler), since no reachable HTTP mux exists.
	// Register it whenever at least one upload path is available for the
	// running transport.
	if uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, opts.uploadHandler != nil, deps.tunnelOpenAI) {
		var pathFn UploadFileHandler
		if deps.coLocated {
			pathFn = opts.localPathUpload
		}
		// relayFn is the authenticated file executor for the openai-tunnel
		// url/data source modes.
		uploadFileDesc := NewUploadFileDescriptor(deps.coLocated, deps.tunnelOpenAI, pathFn, deps.curlUpload, opts.uploadHandler, opts.relayAllowedHosts, opts.maxRelayBytes)

		// Pair upload_file with its "Upload to IPFS" MCP App view
		// (ui://uploads/ipfs.html) so a UI-capable host renders a file-picker
		// panel. RegisterAppView attaches _meta.ui to a catalog entry, so the
		// tool must be indexed first. The app is only meaningfully available
		// when a presigned httpUpload coordinator can mint a PUT endpoint for
		// the Uppy XHR uploader (deps.curlUpload != nil); in co-located stdio
		// local-path mode there is no presigned endpoint, so the app is not
		// registered and upload_file simply serves the out-of-band local path
		// surface. Gating on both uploadFileAvailable and curlUpload != nil
		// keeps attachAppMeta from ever running when the tool is absent (e.g.
		// --tunnel openai) or when no mint URL could be produced.
		if deps.curlUpload != nil {
			deps.catalog.Add(model.ToolEntryFromDescriptor(uploadFileDesc))
			if err := RegisterIPFSUploadApp(deps.srv, deps.catalog, deps.curlUpload); err != nil {
				return err
			}
			// Copy the app-view _meta (registered above onto the catalog entry)
			// onto the descriptor served directly to hosts, so a UI-capable host
			// reading upload_file from tools/list (the RegisterOfficialDescriptor
			// surface) still sees the file-picker panel. The direct surface and
			// the catalog entry are distinct objects; without this copy the
			// direct tool would miss _meta.ui.
			if entry, ok := deps.catalog.Get("upload_file"); ok {
				if uploadFileDesc.Meta == nil {
					uploadFileDesc.Meta = map[string]any{}
				}
				for k, v := range entry.Meta {
					uploadFileDesc.Meta[k] = v
				}
			}
		}
		if err := RegisterOfficialDescriptor(deps.srv, uploadFileDesc); err != nil {
			return err
		}
	}
	if opts.uploadTasks != nil {
		for _, desc := range NewAsyncUploadTools(opts.uploadTasks) {
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
//     (vaultHTTPUpload) is wired AND the transport exposes a reachable HTTP
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
