package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	"go.lumeweb.com/pinner-cli/build"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	mcpauth "go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	mcpvault "go.lumeweb.com/pinner-cli/internal/mcp/vault"
	mcpwizard "go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// Run executes the CLI application with the given context and arguments.
func Run(ctx context.Context, args []string) error {
	cmd := NewRootCommand()
	return cmd.Run(ctx, args)
}

// notInitErr returns the "not initialized" error shared by every lazily-wired
// MCP dependency guard, so the message format cannot drift across call sites.
func notInitErr(label string) error {
	return fmt.Errorf("%s dependencies are not initialized", label)
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
	var pinProvider apps.PinningProviderFactory
	// uploadHandler is the single vendor-agnostic stream→upload executor shared
	// by every file-input tool (file object, URL relay, draft data: URI,
	// async). Only the byte source differs; the authenticated upload contract is
	// the same. It is also assigned to the type-alias handler (a type alias
	// of UploadHandler) that the vendored pinner_upload_file tool needs.
	var uploadHandler transfer.UploadHandler
	var vaultPutHandler mcpvault.VaultPutHandler
	// ipfsDownload is the IPFS download executor used by download_file's sinks.
	// It is built inside the wizard factory (where cfgMgr/secure are available)
	// and read by the WithIPFSDownload option below — mirror of uploadHandler.
	var ipfsDownload transfer.IPFSDownloadHandler
	// vaultGet is the vault-read executor used by vault_get_file's sinks. It is
	// built inside the wizard factory (where the vault service is available)
	// and read by the WithVaultGet option below — mirror of vaultPutHandler.
	var vaultGet transfer.VaultGetHandler
	// localPathUpload is the co-located (stdio/local-mode) handler that backs
	// the consolidated upload_file tool's co-located branch: it uploads
	// a host-side file/directory/archive. It is built inside the wizard factory
	// (where uploadSvc lives) and read by the WithLocalPathUpload option below.
	var localPathUpload transfer.LocalPathUploadHandler
	// localPathVaultPut is the vault_put_file (SDIO/local-mode path branch)
	// handler: it writes a host-side file/directory/archive into the encrypted
	// vault. It is built inside the wizard factory (where the vault service is
	// available) and read by the WithLocalPathVaultPut option below.
	var localPathVaultPut mcpvault.LocalPathVaultPutHandler
	// Build the `mcp` command. It must be captured (not appended inline) so the
	// `pinner mcp install` golden-path subcommand can be attached to its
	// Commands below: NewMcpInstallCommand lives in internal/cli and cannot be
	// referenced from the internal/mcp package (internal/cli imports
	// internal/mcp, so importing internal/cli there would form a cycle). Root
	// is the join point where both sides exist.
	mcpCmd := mcpadapter.MCPCommand(root,
		func() (mcpwizard.WebsitesWizardDeps, mcpwizard.SetupWizardDeps, mcpwizard.DomainWizardDeps, error) {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return mcpwizard.WebsitesWizardDeps{}, mcpwizard.SetupWizardDeps{}, mcpwizard.DomainWizardDeps{}, err
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
			websitesSvc := websites.DefaultFactory(cfgMgr, secure)
			authSvc := defaultAuthServiceFactory(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure())
			// Build the pinning backend early so the upload path can apply the
			// requested pin Name after upload (and the "Create a Pin" MCP App can
			// reuse it). Reuse the CLI's PinningService, which reads cfgMgr live
			// at request time.
			pinningSvc := defaultPinningServiceFactory(cfgMgr, secure)
			uploadSvc := defaultUploadServiceFactory(cfgMgr, output, WithUploadAuthService(authSvc), WithUploadPinningService(pinningSvc))

			// Adapt the pinning backend into the SDK-neutral PinningProvider.
			pinProvider = func() (apps.PinningProvider, error) {
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
			uploadHandler = func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, wrap bool) (any, error) {
				if name == "" {
					name = transfer.DefaultUploadName
				}
				file, err := os.CreateTemp("", "pinner-mcp-upload-*")
				if err != nil {
					return nil, err
				}
				path := file.Name()
				defer os.Remove(path)
				defer file.Close()
				// A wrapped (website) single-file upload with no explicit name
				// sniffs the content: HTML becomes index.html so the site
				// resolves at its root instead of exposing a temp/upload label.
				// The sniffed head bytes are written to the temp file first so
				// the subsequent io.Copy appends the remainder without dropping
				// the content consumed during sniffing.
				if wrap && (name == "" || name == transfer.DefaultUploadName) {
					var head [512]byte
					n, _ := io.ReadFull(reader, head[:])
					if resolved := transfer.ResolveWrappedFileName(name, true, head[:n]); resolved != "" {
						name = resolved
					}
					if n > 0 {
						if _, err := file.Write(head[:n]); err != nil {
							return nil, err
						}
					}
				}
				if _, err := io.Copy(file, reader); err != nil {
					return nil, err
				}
				if _, err := file.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
				result, err := uploadSvc.Upload(ctx, contentfs.NewSingleFileFS(file, name), name, wait, wrap)
				if err != nil {
					return nil, err
				}
				return result, nil
			}
			// resolvePath stats path, returns a not-exist error, and applies the
			// upload name defaulting (filepath.Base, else "upload") when name is
			// empty. Shared by localPathUpload and localPathVaultPut.
			resolvePath := func(path, name string) (info os.FileInfo, resolved string, err error) {
				info, err = os.Stat(path)
				if err != nil {
					if os.IsNotExist(err) {
						return nil, "", fmt.Errorf("path does not exist: %s", path)
					}
					return nil, "", err
				}
				if name == "" {
					name = filepath.Base(path)
					if name == "" || name == "." || name == string(filepath.Separator) {
						name = transfer.DefaultUploadName
					}
				}
				return info, name, nil
			}

			// localPathUpload is the co-located (stdio/local-mode) handler that
			// backs the consolidated upload_file tool's co-located branch. It
			// homes the file-vs-directory-vs-archive decision here, in the CLI
			// layer where uploadSvc lives, so MCP only owns the tool surface.
			localPathUpload = func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
				// Operator-set per-tool cap, applied to every local-path surface
				// (single file, directory tree, or archive) so none can bypass
				// the same total-transfer limit enforced on relay/DataURI/curl.
				maxBytes := int64(cfgMgr.Config().GetMaxMCPUploadSize())
				info, name, err := resolvePath(path, name)
				if err != nil {
					return nil, err
				}
				// Directory: upload the tree rooted at path. Reject up front if
				// the aggregate (or any single entry) exceeds the cap.
				if info.IsDir() {
					// CheckDirectorySize follows symlinks (os.Stat) so the
					// pre-flight size matches the bytes uploadSvc.Upload will
					// actually read through the DirFS — an fs.WalkDir over
					// os.DirFS would lstat entries and let a symlink pointing
					// at an oversized file bypass the cap.
					if err := ieo.CheckDirectorySize(path, maxBytes, ieo.TreeSizeAggregate); err != nil {
						return nil, err
					}
					result, err := uploadSvc.Upload(ctx, os.DirFS(path), name, wait, false)
					if err != nil {
						return nil, err
					}
					return result, nil
				}
				// Regular file (or unknown). Open it; *os.File satisfies the
				// ReaderAtSeeker contract contentArchive needs for extraction.
				file, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				defer file.Close()
				// In convert mode (default), sniff for an archive and upload its
				// extracted contents; otherwise upload the single file as-is.
				if ieo.ParseArchiveMode(archiveMode) == ieo.ArchiveConvert {
					if _, isArc, serr := ieo.SniffArchive(file); serr == nil && isArc {
						vfs, closer, aerr := ieo.OpenArchiveFS(ctx, file)
						if aerr == nil {
							defer closer()
							if err := ieo.CheckTreeSize(vfs, maxBytes, ieo.TreeSizeAggregate); err != nil {
								return nil, err
							}
							result, uerr := uploadSvc.Upload(ctx, vfs, name, wait, false)
							if uerr != nil {
								return nil, uerr
							}
							return result, nil
						}
					}
				}
				// Not an archive (or preserve mode): upload the file directly.
				// Enforce the operator-set max_mcp_upload_size cap before transfer so
				// the local-path surface cannot bypass the same limit applied to the
				// relay/DataURI/curl surfaces.
				if info.Size() > maxBytes {
					return nil, fmt.Errorf("file %s (%d bytes) exceeds max_mcp_upload_size (%d)", path, info.Size(), maxBytes)
				}
				// Wrapped (website) single-file upload with no explicit name:
				// sniff for HTML and default to index.html (see uploadHandler).
				if wrap && (name == "" || name == transfer.DefaultUploadName) {
					var head [512]byte
					n, _ := file.Read(head[:])
					if resolved := transfer.ResolveWrappedFileName(name, true, head[:n]); resolved != "" {
						name = resolved
					}
					_, _ = file.Seek(0, io.SeekStart)
				}
				result, err := uploadSvc.Upload(ctx, contentfs.NewSingleFileFS(file, name), name, wait, wrap)
				if err != nil {
					return nil, err
				}
				return result, nil
			}
			vaultPutHandler = func(ctx context.Context, reader io.Reader, size int64, path string) (any, error) {
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

			// vaultGet is the vault_get_file sink executor: it streams a single
			// encrypted vault file's decrypted bytes to w. The vault path is
			// resolved against the active profile at call time (mirroring
			// vaultPutHandler); the tool surface owns sink selection and path
			// validation.
			vaultGet = func(ctx context.Context, vaultPath string, w io.Writer) error {
				profile, err := vault.ResolveProfile("")
				if err != nil {
					return err
				}
				vaultSvc, err := newVaultService(profile)
				if err != nil {
					return err
				}
				defer vaultSvc.Close()
				return vaultSvc.Get(ctx, vaultPath, w)
			}

			// ipfsDownload is the download_file sink executor: it streams a
			// single IPFS node (CID or CID/path) to w via the authenticated
			// download service. The service reads the auth token live from
			// cfgMgr at request time (matching the other long-lived MCP
			// services), so a `pinner login` that relocates the token applies
			// without a restart.
			ipfsDownload = func(ctx context.Context, ipfsPath string, w io.Writer) error {
				authSvc := auth.NewAuthService(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure(), nil)
				var svcOpts []DownloadServiceOption
				svcOpts = append(svcOpts, WithDownloadAuthService(authSvc), WithDownloadIPFSEndpoint(cfgMgr.Config().GetIPFSEndpointWithSecure(secure)))
				downloadSvc := defaultDownloadServiceFactory(cfgMgr, output, svcOpts...)
				if err := downloadSvc.RequireAuthenticated(); err != nil {
					return err
				}
				reader, err := downloadSvc.Cat(ctx, ipfsPath)
				if err != nil {
					return err
				}
				defer reader.Close()
				_, err = io.Copy(w, reader)
				return err
			}

			// localPathVaultPut is the vault_put_file path-mode handler (SDIO/local
			// mode). It writes a host-side file/directory/archive into the
			// encrypted vault: a directory is walked into one vault object
			// per file via mcpvault.DirToVault; a file is written as a
			// single vault object, except in archive_mode=convert where an
			// archive is extracted to a temp dir and then written per-file
			// the same way. The vault service is built here, where profile
			// resolution and service construction are available.
			localPathVaultPut = func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
				// Operator-set per-tool cap, applied to every entry (single
				// file, directory tree, or archive) so no local-path surface
				// bypasses the same limit enforced on the other upload tools.
				maxBytes := int64(cfgMgr.Config().GetMaxMCPUploadSize())
				info, _, err := resolvePath(path, "")
				if err != nil {
					return nil, err
				}
				profile, err := vault.ResolveProfile("")
				if err != nil {
					return nil, err
				}
				vaultSvc, err := newVaultService(profile)
				if err != nil {
					return nil, err
				}
				defer vaultSvc.Close()
				put := func(ctx context.Context, r io.Reader, size int64, vp string) (any, error) {
					return vaultSvc.Put(ctx, r, size, vp, nil)
				}
				if info.IsDir() {
					return mcpvault.DirToVault(ctx, path, vaultPath, put, maxBytes)
				}
				// Regular file (or unknown). In convert mode, sniff for an
				// archive and materialize its contents to a temp dir, then
				// write each entry as a vault object under vaultPath.
				if ieo.ParseArchiveMode(archiveMode) == ieo.ArchiveConvert {
					file, err := os.Open(path)
					if err != nil {
						return nil, err
					}
					_, isArc, serr := ieo.SniffArchive(file)
					file.Close()
					if serr == nil && isArc {
						// Enforce the operator-set max_mcp_upload_size cap on the
						// archive's extracted contents BEFORE materializing, so an
						// archive larger than the cap (or a decompression bomb)
						// cannot be fully extracted into the temp dir and into
						// memory first. Reject it up front on the aggregate of all
						// regular-file sizes, mirroring the upload convert path.
						if cerr := checkArchiveTreeSize(ctx, path, maxBytes); cerr != nil {
							return nil, cerr
						}
						tmp, err := os.MkdirTemp("", "pinner-vault-archive-*")
						if err != nil {
							return nil, err
						}
						defer os.RemoveAll(tmp)
						if err := materializeArchive(ctx, path, tmp); err != nil {
							return nil, err
						}
						return mcpvault.DirToVault(ctx, tmp, vaultPath, put, maxBytes)
					}
				}
				// Not an archive (or preserve mode): put the file as a single
				// vault object. Enforce the operator-set max_mcp_upload_size cap so
				// the local-path surface cannot bypass the limit applied to the
				// relay/DataURI/curl surfaces.
				if info.Size() > maxBytes {
					return nil, fmt.Errorf("file %s (%d bytes) exceeds max_mcp_upload_size (%d)", path, info.Size(), maxBytes)
				}
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				return vaultSvc.Put(ctx, f, info.Size(), vaultPath, nil)
			}

			// Wire the resource factory with the services we just built.
			// Capture cfgMgr (not a config snapshot) so resource reads see latest state.
			resourceFactory = func(store *session.SessionStore) mcpadapter.ResourceProviders {
				return mcpadapter.ResourceProviders{
					Account:  &accountStatusAdapter{cfgMgr: cfgMgr, auth: authSvc},
					Websites: &websitesResourceAdapter{ws: websitesSvc},
					Vault:    &vaultStatusAdapter{cfgMgr: cfgMgr},
				}
			}

			wDeps := mcpwizard.WebsitesWizardDeps{
				CfgMgr:          cfgMgr,
				WebsitesService: websitesSvc,
				WebsitesFactory: func() mcpwizard.WebsitesWizardState {
					return NewWebsitesWizard(websitesSvc, cfgMgr, nil, output)
				},
			}
			sDeps := mcpwizard.SetupWizardDeps{
				CfgMgr:      cfgMgr,
				AuthService: authSvc,
				// Out-of-band sign-in runs in the user's browser on an
				// auto-started loopback listener (stdio) or the tunneled/HTTP
				// mux (remote). The base URL is empty at construction: the
				// coordinator derives the loopback address when a login is
				// requested.
				OutOfBand: mcpauth.NewOutOfBandLogin(authSvc, "", mcpauth.DefaultMCPKeyName),
				// OOB restore completes a vault restore from a mnemonic the
				// human enters in a browser form (loopback in stdio, shared
				// mux over HTTP), so the seed never transits the MCP channel.
				Restore: NewVaultRestoreRunner(output, cfgMgr.Config().GetSiaIndexerURL()),
				// OOB create provisions + activates a new vault (generating a
				// fresh seed) in a browser page, symmetric with restore, so the
				// seed never transits the MCP channel.
				Create: NewVaultCreateRunner(cfgMgr.Config().GetSiaIndexerURL()),
				SetupFactory: func() mcpwizard.SetupWizardState {
					return NewSetupWizard(cfgMgr, authSvc, nil, SetupOptions{})
				},
			}
			dDeps := mcpwizard.DomainWizardDeps{
				CfgMgr:          cfgMgr,
				WebsitesService: websitesSvc,
				DomainFactory: func() mcpwizard.DomainWizardState {
					return NewDomainAddWizard(websitesSvc, cfgMgr, nil, output)
				},
			}
			return wDeps, sDeps, dDeps, nil
		},
		func(store *session.SessionStore) mcpadapter.ResourceProviders {
			if resourceFactory != nil {
				provs := resourceFactory(store)
				provs.Sessions = store
				return provs
			}
			return mcpadapter.ResourceProviders{Sessions: store}
		},
		mcpadapter.WithPrompts(),
		mcpadapter.WithPinningProvider(func() (apps.PinningProvider, error) {
			if pinProvider == nil {
				return nil, notInitErr("pinning provider")
			}
			return pinProvider()
		}),
		mcpadapter.WithUploadHandler(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, wrap bool) (any, error) {
			if uploadHandler == nil {
				return nil, notInitErr("file upload")
			}
			return uploadHandler(ctx, reader, size, name, wait, wrap)
		}),
		mcpadapter.WithVaultPutHandler(func(ctx context.Context, reader io.Reader, size int64, path string) (any, error) {
			if vaultPutHandler == nil {
				return nil, notInitErr("vault upload")
			}
			return vaultPutHandler(ctx, reader, size, path)
		}),
		mcpadapter.WithIPFSDownload(func(ctx context.Context, ipfsPath string, w io.Writer) error {
			if ipfsDownload == nil {
				return notInitErr("IPFS download")
			}
			return ipfsDownload(ctx, ipfsPath, w)
		}),
		mcpadapter.WithVaultGet(func(ctx context.Context, vaultPath string, w io.Writer) error {
			if vaultGet == nil {
				return notInitErr("vault download")
			}
			return vaultGet(ctx, vaultPath, w)
		}),
		// Confine download_file / vault_get_file local-sink writes to the
		// configured download root (default <config-dir>/downloads). Resolved
		// lazily from the config manager at server setup.
		mcpadapter.WithDownloadRoot(func() string {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().GetDownloadRoot()
		}),
		mcpadapter.WithUploadTaskManager(transfer.NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, wrap bool) (any, error) {
			if uploadHandler == nil {
				return nil, notInitErr("file upload")
			}
			return uploadHandler(ctx, reader, size, name, wait, wrap)
		}, 0)),
		// pinner_upload_url: vendor-agnostic relay fetch of a caller-supplied
		// public HTTPS URL. The no-allowlist default permits any public HTTPS
		// host (the tool's documented contract for remote HTTP clients); the
		// SSRF guard in the relay's default transport is the hard boundary and
		// always blocks private/link-local IPs. Operators can restrict further
		// by passing an explicit allowlist here.
		mcpadapter.WithRelayURLUpload(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, wrap bool) (any, error) {
			if uploadHandler == nil {
				return nil, notInitErr("upload")
			}
			return uploadHandler(ctx, reader, size, name, wait, wrap)
		}, nil),
		mcpadapter.WithDataURIUpload(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, wrap bool) (any, error) {
			if uploadHandler == nil {
				return nil, notInitErr("upload")
			}
			return uploadHandler(ctx, reader, size, name, wait, wrap)
		}),
		// Local-path handler for the consolidated upload_file tool's co-located
		// branch: upload of a host-side file, directory, or archive. Only
		// meaningful when the MCP server is co-located with the caller's files
		// (stdio/local); the handler homes the file-vs-directory-vs-archive
		// decision via uploadSvc + ipfs-content.
		mcpadapter.WithLocalPathUpload(func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			if localPathUpload == nil {
				return nil, notInitErr("local path upload")
			}
			return localPathUpload(ctx, path, name, wait, archiveMode, wrap)
		}),
		// vault_put_file: path mode put of a host-side file, directory,
		// or archive into the encrypted vault. Only meaningful when the MCP
		// server is co-located with the caller's files (stdio/local); the
		// handler homes the file-vs-directory-vs-archive decision via the
		// vault service + ipfs-content.
		mcpadapter.WithLocalPathVaultPut(func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
			if localPathVaultPut == nil {
				return nil, notInitErr("local path vault")
			}
			return localPathVaultPut(ctx, path, vaultPath, archiveMode)
		}),
		// Honor the configured max_mcp_upload_size (default 1 GiB when unset)
		// as the per-tool file-upload cap for the relay URL, data URI, the
		// consolidated upload_file (local + presigned) surfaces, and vault.
		// Resolved lazily from the config manager at server setup, mirroring
		// the wizard factory pattern; the local-path handlers also enforce the
		// same cap on single-file sources before
		mcpadapter.WithMaxMCPUploadSize(func() uint64 {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return 0
			}
			return cfgMgr.Config().GetMaxMCPUploadSize()
		}),
		// Wire the production operation-catalog deps bundle so the
		// compiler-derived MCP surface (auth, vault-setup, vault, pins,
		// websites, dns, ipns, api-keys, operations) goes LIVE for real
		// invocations. The bundle resolves config and services lazily per
		// request via buildCatalogOpsDeps, so live token reload is preserved.
		mcpadapter.WithCatalogOps(buildCatalogOpsDeps),
	)

	// Attach the `pinner mcp install` golden-path subcommand to the `mcp`
	// command tree, then register `mcp` on the root. Both symbols are
	// internal/cli-local here, so no import cycle is introduced.
	mcpCmd.Commands = append(mcpCmd.Commands, NewMcpInstallCommand())
	root.Commands = append(root.Commands, mcpCmd)

	return root
}

// checkArchiveTreeSize opens the archive at srcPath as a virtual filesystem and
// rejects it up front if the aggregate of all regular-file sizes (or any single
// entry) exceeds maxBytes. It lets the vault_put_file archive convert path
// enforce the operator-set max_mcp_upload_size cap BEFORE materializing an
// archive into a temp dir and into memory, so an oversized archive or
// decompression bomb cannot bypass the cap. Returns nil when the archive fits.
func checkArchiveTreeSize(ctx context.Context, srcPath string, maxBytes int64) error {
	arc, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer arc.Close()
	vfs, closer, err := ieo.OpenArchiveFS(ctx, arc)
	if err != nil {
		return err
	}
	defer closer()
	return ieo.CheckTreeSize(vfs, maxBytes, ieo.TreeSizeAggregate)
}

// materializeArchive extracts the archive at srcPath into the existing local
// directory dstDir by walking its virtual filesystem (from ipfs-content) and
// writing each entry out to disk. It is used by the vault_put_file archive
// convert path, which must hand DirToVault a real local directory. Returns the
// first error encountered; on error dstDir may be partially populated (the
// caller is responsible for cleanup via os.RemoveAll).
func materializeArchive(ctx context.Context, srcPath, dstDir string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	vfs, closer, err := ieo.OpenArchiveFS(ctx, src)
	if err != nil {
		return err
	}
	defer closer()
	return fs.WalkDir(vfs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Zip-Slip guard: entry paths come from an untrusted archive, so reject
		// any that could escape dstDir (absolute paths, "..", or backslash
		// tricks). filepath.IsLocal verifies the path is relative, has a single
		// non-empty element come after a leading dot, and contains no ".." or
		// volume name. We additionally re-verify the joined, cleaned result is
		// still inside dstDir as a defense-in-depth check against any
		// separator/alias edge on Windows.
		rel := filepath.Clean(filepath.FromSlash(p))
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("archive entry escapes destination directory: %q", p)
		}
		dest := filepath.Join(dstDir, rel)
		cleanDest := filepath.Clean(dest)
		if !strings.HasPrefix(cleanDest, dstDir+string(filepath.Separator)) && cleanDest != dstDir {
			return fmt.Errorf("archive entry escapes destination directory: %q", p)
		}
		if d.IsDir() {
			return os.MkdirAll(cleanDest, 0o755)
		}
		// Stream each entry to disk rather than buffering it fully in memory
		// via fs.ReadFile. A single entry can be as large as the operator-set
		// max_mcp_upload_size cap (1 GiB by default), and buffering every entry
		// would press down on a long-lived server's RAM. Open the entry and
		// io.Copy it to the destination file in bounded chunks.
		srcEntry, err := vfs.Open(p)
		if err != nil {
			return err
		}
		defer srcEntry.Close()
		if err := os.MkdirAll(filepath.Dir(cleanDest), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, srcEntry)
		return err
	})
}
