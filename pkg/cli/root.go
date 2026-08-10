package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	"go.lumeweb.com/pinner-cli/build"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
	"go.lumeweb.com/pinner-cli/pkg/config"
	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

// Run executes the CLI application with the given context and arguments.
func Run(ctx context.Context, args []string) error {
	cmd := NewRootCommand()
	return cmd.Run(ctx, args)
}

// NewRootCommand creates and returns the root CLI command.
func NewRootCommand() *cli.Command {
	root := &cli.Command{
		Name:                  "pinner",
		Usage:                 "Simple IPFS Pinning CLI",
		Version:               build.Version,
		EnableShellCompletion: true,
		Description: `A minimal, developer-focused CLI tool for pinning content to IPFS
via the Pinner.xyz service.

Common workflows:
  First-time setup:      pinner setup
  Quick upload & pin:    pinner upload myfile.txt
  Stream content:        pinner cat bafybeig...td7e
  Download content:      pinner download bafybeig...td7e
  List directory:        pinner ls bafybeig...td7e
  Pin existing CID:      pinner pin bafybeig...td7e --name "my file"
  List your pins:        pinner list
  Check pin status:      pinner status bafybeig...td7e
  Remove a pin:          pinner unpin bafybeig...td7e --force

Onchain domains:
  Point a domain:       pinner point vitalik.eth --cid bafybeig...td7e
  Remove pointing:      pinner unpoint vitalik.eth

Batch operations:
  Pin multiple CIDs:     pinner pin bafybeig...abc bafybeig...def bafybeig...ghi --parallel 5
  Pin from file:        pinner pin --file cids.txt --wait
  Unpin multiple:       pinner unpin --file cids.txt --force

Authentication:
  Login:                pinner auth --email user@example.com
  Logout:               pinner auth logout
  Register account:     pinner register
  Check diagnostics:    pinner doctor

For more help on any command: pinner <command> --help`,
		Commands: []*cli.Command{
			newSetupCommand(),
			newAuthCommand(),
			newRegisterCommand(),
			newConfirmEmailCommand(),
			newAccountCommand(),
			newUploadCommand(),
			newDownloadCommand(),
			newCatCommand(),
			newLsCommand(),
			newPinCommand(),
			newPointCommand(),
			newUnpointCommand(),
			newPinsCommand(),
			newListCommand(),
			newStatusCommand(),
			newUnpinCommand(),
			newMetadataRemovedCommand(),
			newOperationsCommand(),
			newConfigCommand(),
			newDoctorCommand(),
			newBenchCommand(),
			newDNSCommand(),
			newIPNSCommand(),
			newWebsitesCommand(),
			newDagCommand(),
			newExportCommand(),
			newAdminCommand(),
			newDocsCommand(),
			newVaultCommand(),
		},
		Flags: GlobalFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return cli.ShowRootCommandHelp(cmd)
		},
	}

	// Register the MCP server command with a reference to root for in-process tool execution.
	var resourceFactory mcpadapter.ResourceProvidersFactory
	// pinProvider builds the live pinning backend for the "Create a Pin" MCP App
	// (ui:// view + app-only status helper). Populated inside the wizard factory
	// once a cfgMgr/output/secure are available.
	var pinProvider mcpadapter.PinningProviderFactory
	// uploadHandler is the single vendor-agnostic stream→upload executor shared
	// by every file-input tool (ChatGPT file object, URL relay, draft data: URI,
	// async). Only the byte source differs; the authenticated upload contract is
	// the same. It is also assigned to the vendor-typed ChatGPT handler (a type
	// alias of UploadHandler) that the vendored pinner_upload_file tool needs.
	var uploadHandler mcpadapter.UploadHandler
	var chatGPTVaultPut mcpadapter.ChatGPTVaultPutHandler
	root.Commands = append(root.Commands, mcpadapter.MCPCommand(root,
		func() (mcpadapter.WebsitesWizardDeps, mcpadapter.SetupWizardDeps, mcpadapter.DomainWizardDeps, error) {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return mcpadapter.WebsitesWizardDeps{}, mcpadapter.SetupWizardDeps{}, mcpadapter.DomainWizardDeps{}, err
			}

			// Build a minimal output formatter for service construction.
			// Use io.Discard so service-side writes don't corrupt the MCP JSON-RPC stream.
			output := NewOutputFormatter(false, false, false, false)
			output.SetWriter(io.Discard)

			secure := cfgMgr.Config().Secure
			// Build websites service without RequireAuthenticated; the setup wizard
			// must be reachable for unauthenticated users, and the websites wizard's
			// auth_check step enforces authentication at runtime.
			//
			// Do NOT pin the auth token here (no WithWebsitesAuthToken override):
			// the websites/DNS/IPNS services read cfgMgr.Config().AuthToken live at
			// request time, so a `pinner login` (or config edit) that relocates the
			// token on disk is picked up by the running server without a restart.
			// Freezing the startup token as an override would defeat live reload.
			websitesSvc := websitesServiceFactory(cfgMgr, output, secure)
			authSvc := defaultAuthServiceFactory(cfgMgr, output, cfgMgr.Config().BaseEndpoint)
			uploadSvc := defaultUploadServiceFactory(cfgMgr, output, WithUploadAuthService(authSvc))

			// Build the pinning backend for the "Create a Pin" MCP App. Reuse
			// the CLI's PinningService (which reads cfgMgr live at request time)
			// and adapt its Status into the SDK-neutral PinningProvider.
			pinningSvc := defaultPinningServiceFactory(cfgMgr, output, secure)
			pinProvider = func() (mcpadapter.PinningProvider, error) {
				return &pinStatusAdapter{pins: pinningSvc}, nil
			}

			// Live-reload the auth token into the long-lived services without a
			// restart: subscribe to config auth_token changes (fired by the file
			// watcher on a `pinner login` / on-disk edit) and push the new token
			// into the retained clients. RequireAuthenticated/getAuthToken read
			// config live, so this keeps the running server's credential current.
			cfgMgr.Subscribe(config.ConfigKeyAuthToken, func(_pattern, _key string, value any) {
				if tok, ok := value.(string); ok {
					websitesSvc.SetAuthToken(tok)
				}
			})
			uploadHandler = func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
				if name == "" {
					name = "upload"
				}
				file, err := os.CreateTemp("", "pinner-mcp-upload-*")
				if err != nil {
					return nil, err
				}
				path := file.Name()
				defer os.Remove(path)
				defer file.Close()
				if _, err := io.Copy(file, reader); err != nil {
					return nil, err
				}
				if _, err := file.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
				result, err := uploadSvc.Upload(ctx, contentfs.NewSingleFileFS(file, name), name, wait)
				if err != nil {
					return nil, err
				}
				return result, nil
			}
			chatGPTVaultPut = func(ctx context.Context, reader io.Reader, size int64, path string) (any, error) {
				profile, err := vault.ResolveProfile("")
				if err != nil {
					return nil, err
				}
				vaultSvc, err := newVaultService(profile)
				if err != nil {
					return nil, err
				}
				defer vaultSvc.Close()
				return vaultSvc.Put(ctx, reader, size, path, nil)
			}

			// Wire the resource factory with the services we just built.
			// Capture cfgMgr (not a config snapshot) so resource reads see latest state.
			resourceFactory = func(store *mcpadapter.SessionStore) mcpadapter.ResourceProviders {
				return mcpadapter.ResourceProviders{
					Account:  &accountStatusAdapter{cfgMgr: cfgMgr, auth: authSvc},
					Websites: &websitesResourceAdapter{ws: websitesSvc},
					Vault:    &vaultStatusAdapter{cfgMgr: cfgMgr},
				}
			}

			wDeps := mcpadapter.WebsitesWizardDeps{
				CfgMgr:          cfgMgr,
				WebsitesService: websitesSvc,
				WebsitesFactory: func() mcpadapter.WebsitesWizardState {
					return NewWebsitesWizard(websitesSvc, cfgMgr, nil, output)
				},
			}
			sDeps := mcpadapter.SetupWizardDeps{
				CfgMgr:      cfgMgr,
				AuthService: authSvc,
				// Out-of-band sign-in runs in the user's browser on an
				// auto-started loopback listener (stdio) or the tunneled/HTTP
				// mux (remote). The base URL is empty at construction: the
				// coordinator derives the loopback address when a login is
				// requested.
				OutOfBand: mcpadapter.NewOutOfBandLogin(authSvc, "", mcpadapter.DefaultMCPKeyName),
				// OOB restore completes a vault restore from a mnemonic the
				// human enters in a browser form (loopback in stdio, shared
				// mux over HTTP), so the seed never transits the MCP channel.
				Restore: NewVaultRestoreRunner(output, cfgMgr.Config().GetSiaIndexerURL()),
				SetupFactory: func() mcpadapter.SetupWizardState {
					return NewSetupWizard(cfgMgr, authSvc, nil, SetupOptions{})
				},
			}
			dDeps := mcpadapter.DomainWizardDeps{
				CfgMgr:          cfgMgr,
				WebsitesService: websitesSvc,
				DomainFactory: func() mcpadapter.DomainWizardState {
					return NewDomainAddWizard(websitesSvc, cfgMgr, nil, output)
				},
			}
			return wDeps, sDeps, dDeps, nil
		},
		func(store *mcpadapter.SessionStore) mcpadapter.ResourceProviders {
			if resourceFactory != nil {
				provs := resourceFactory(store)
				provs.Sessions = store
				return provs
			}
			return mcpadapter.ResourceProviders{Sessions: store}
		},
		mcpadapter.WithPrompts(),
		mcpadapter.WithPinningProvider(func() (mcpadapter.PinningProvider, error) {
			if pinProvider == nil {
				return nil, fmt.Errorf("pinning provider dependencies are not initialized")
			}
			return pinProvider()
		}),
		mcpadapter.WithChatGPTUpload(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
			if uploadHandler == nil {
				return nil, fmt.Errorf("ChatGPT upload dependencies are not initialized")
			}
			return uploadHandler(ctx, reader, size, name, wait)
		}),
		mcpadapter.WithChatGPTVaultPut(func(ctx context.Context, reader io.Reader, size int64, path string) (any, error) {
			if chatGPTVaultPut == nil {
				return nil, fmt.Errorf("ChatGPT vault dependencies are not initialized")
			}
			return chatGPTVaultPut(ctx, reader, size, path)
		}),
		mcpadapter.WithUploadTaskManager(mcpadapter.NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
			if uploadHandler == nil {
				return nil, fmt.Errorf("ChatGPT upload dependencies are not initialized")
			}
			return uploadHandler(ctx, reader, size, name, wait)
		}, 0)),
		// pinner_upload_url: vendor-agnostic relay fetch of a caller-supplied
		// public HTTPS URL. The no-allowlist default permits any public HTTPS
		// host (the tool's documented contract for remote HTTP clients); the
		// SSRF guard in the relay's default transport is the hard boundary and
		// always blocks private/link-local IPs. Operators can restrict further
		// by passing an explicit allowlist here.
		mcpadapter.WithRelayURLUpload(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
			if uploadHandler == nil {
				return nil, fmt.Errorf("upload dependencies are not initialized")
			}
			return uploadHandler(ctx, reader, size, name, wait)
		}, nil),
		mcpadapter.WithDataURIUpload(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
			if uploadHandler == nil {
				return nil, fmt.Errorf("upload dependencies are not initialized")
			}
			return uploadHandler(ctx, reader, size, name, wait)
		}),
	))

	return root
}
