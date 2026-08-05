package cli

import (
	"context"
	"io"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
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
		},
		Flags: GlobalFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return cli.ShowRootCommandHelp(cmd)
		},
	}

	// Register the MCP server command with a reference to root for in-process tool execution.
	var resourceFactory mcpadapter.ResourceProvidersFactory
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

			authToken := cfgMgr.Config().AuthToken
			secure := cfgMgr.Config().Secure
			// Build websites service without RequireAuthenticated — the setup wizard
			// must be reachable for unauthenticated users, and the websites wizard's
			// auth_check step enforces authentication at runtime.
			var svcOpts []WebsitesServiceOption
			if authToken != "" {
				svcOpts = append(svcOpts, WithWebsitesAuthToken(authToken))
			}
			websitesSvc := websitesServiceFactory(cfgMgr, output, secure, svcOpts...)
			authSvc := defaultAuthServiceFactory(cfgMgr, output, cfgMgr.Config().BaseEndpoint)

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
	))

	return root
}
