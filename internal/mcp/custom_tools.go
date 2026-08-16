package mcp

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// customToolDeps bundles everything the custom/direct-tool registration needs.
// Keeping it a struct (rather than a long positional parameter list) makes the
// single registration entry point readable and lets tests build a mostly-zero
// value with only the fields they exercise.
type customToolDeps struct {
	// srv is the official SDK server onto which direct tools/resources/prompts
	// are registered.
	srv *mcp.Server
	// catalog is the internal ToolCatalog carrying every invocable tool. The
	// wizard and SSO tools are appended here (they are built after buildCatalog
	// returns); markCurated then stamps which of them are directly visible.
	catalog *ToolCatalog
	// store backs wizard sessions and resource providers.
	store *SessionStore
	// oob, when non-nil, backs the out-of-band sign-in (SSO) and restore
	// tools; authHandles stores their pending handles, and handoffReg maps a
	// handle to its domain-specific resume continuation so the shared resume
	// template can poll it.
	oob         *OutOfBandLogin
	authHandles *AsyncHandleStore
	handoffReg  *HandoffRegistry
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
	// (no HTTP transport, no tunnel). The local-path upload tools
	// (upload_path, vault_put_path) read arbitrary host paths, so they are only
	// safe — and only meaningful — when the caller shares the host. They are
	// never registered over a remote transport, where a network client could
	// use them to read/exfiltrate server-side files.
	coLocated bool
	// remoteUploadSupported reports whether the presigned HTTP PUT upload route
	// (httpUpload) is actually reachable by a remote agent. It is false for the
	// embedded OpenAI Secure MCP Tunnel, which carries only MCP RPC through an
	// in-memory transport to the tunnel client — there is no reachable HTTP
	// mux on which to mount the PUT route, and the minted endpoint would fall
	// back to an unreachable loopback URL. When false, the consolidated
	// upload_file tool's remote branch is not registered for that transport.
	remoteUploadSupported bool
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
//   - the upload-backend tools (ChatGPT, relay URL, data URI, async)
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
	deps.catalog.Add(toolEntryFromDescriptor(authSSO))
	deps.catalog.Add(toolEntryFromDescriptor(authResume))

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
	deps.catalog.Add(toolEntryFromDescriptor(accountUpdate))
	deps.catalog.Add(toolEntryFromDescriptor(accountReset))
	deps.catalog.Add(toolEntryFromDescriptor(accountEmail))

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
	deps.catalog.Add(toolEntryFromDescriptor(vaultCreateResume))
	deps.catalog.Add(toolEntryFromDescriptor(vaultRestoreResume))

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
		if err := RegisterOfficialResources(deps.srv, resources, templates); err != nil {
			return err
		}
	}

	opts := deps.opts
	if opts == nil {
		opts = &mcpServerOptions{}
	}
	if opts.chatGPTUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, ChatGPTUploadDescriptor(opts.chatGPTUpload)); err != nil {
			return err
		}
	}
	if opts.chatGPTVaultPut != nil {
		if err := RegisterOfficialDescriptor(deps.srv, ChatGPTVaultPutDescriptor(opts.chatGPTVaultPut)); err != nil {
			return err
		}
	}
	if opts.relayURLUpload != nil {
		if err := RegisterOfficialDescriptor(deps.srv, RelayURLUploadDescriptor(opts.relayURLUpload, opts.relayAllowedHosts, opts.maxRelayBytes)); err != nil {
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
	//   - co-located (stdio/local): upload a host-side path via the local-path
	//     handler (opts.localPathUpload).
	//   - remote (HTTP/tunnel): mint a one-time presigned HTTP PUT endpoint via
	//     the httpUpload coordinator (deps.curlUpload).
	// Register it whenever at least one upload path is available. The remote
	// branch additionally requires remoteUploadSupported: the openai tunnel
	// carries only MCP RPC (no reachable HTTP mux), so a minted presigned PUT
	// would be an unreachable loopback URL — the tool must not advertise a
	// branch no agent could use.
	if uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, deps.remoteUploadSupported) {
		var pathFn UploadFileHandler
		if deps.coLocated {
			pathFn = opts.localPathUpload
		}
		uploadFileDesc := NewUploadFileDescriptor(deps.coLocated, pathFn, deps.curlUpload)

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
			deps.catalog.Add(toolEntryFromDescriptor(uploadFileDesc))
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
	if deps.coLocated && opts.localPathVaultPut != nil {
		if err := RegisterOfficialDescriptor(deps.srv, LocalPathVaultPutDescriptor(opts.localPathVaultPut)); err != nil {
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
		opts.chatGPTUpload != nil,
		opts.chatGPTVaultPut != nil,
		opts.relayURLUpload != nil,
		opts.dataURIUpload != nil,
		deps.coLocated && (opts.localPathUpload != nil || opts.localPathVaultPut != nil),
		uploadFileAvailable(deps.coLocated, opts.localPathUpload != nil, deps.curlUpload != nil, deps.remoteUploadSupported),
		opts.maxRelayBytes,
	)); err != nil {
		return err
	}
	// Always expose the static agent guide so a model can orient to the four
	// primary flows without probing each tool's description.
	if err := RegisterOfficialDescriptor(deps.srv, NewAgentGuideDescriptor()); err != nil {
		return err
	}
	if opts.prompts {
		if err := RegisterOfficialPrompts(deps.srv, PromptDescriptors()); err != nil {
			return err
		}
	}
	return nil
}

// uploadFileAvailable reports whether the consolidated upload_file tool has at
// least one usable branch for the running transport:
//
//   - co-located (stdio/local): the local-path upload handler is wired.
//   - remote (HTTP/tunnel): the presigned HTTP PUT coordinator is wired AND the
//     transport actually exposes a reachable HTTP mux (remoteUploadSupported).
//     The embedded OpenAI Secure MCP Tunnel carries only MCP RPC through an
//     in-memory transport to the tunnel client — there is no reachable HTTP
//     mux on which to mount the PUT route, so a minted endpoint would fall
//     back to an unreachable loopback URL. The tool must not be advertised
//     (in registration or capabilities) for a branch no agent could use.
//
// It is the single decision used both when registering the tool and when
// reporting the upload_file capability, so the two can never drift.
func uploadFileAvailable(coLocated, localPathWired, curlWired, remoteUploadSupported bool) bool {
	if coLocated {
		return localPathWired
	}
	return curlWired && remoteUploadSupported
}
