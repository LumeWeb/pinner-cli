package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/avast/retry-go/v5"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	statuspkg "go.lumeweb.com/pinner-cli/internal/core/status"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// UploadServiceConfig holds configuration options for UploadService.
type UploadServiceConfig struct {
	HTTPClient      *http.Client
	AccountClient   portalsdk.AccountAPI
	IPFSEndpoint    string
	AccountEndpoint string
}

// UploadServiceDefault provides upload operations using the ipfs-sdk UploadService.
type UploadServiceDefault struct {
	accountClient   portalsdk.AccountAPI
	authService     AuthService
	pinningService  PinningService
	configMgr       config.Manager
	output          Output
	ipfsEndpoint    string
	accountEndpoint string
	authToken       string
	memoryLimit     uint64
	chunkSize       int64
	chunkerStrategy ipfs.ChunkerStrategy
	maxLinks        int
	config          UploadServiceConfig
}

// UploadServiceOption is a function that configures an UploadService.
type UploadServiceOption func(*UploadServiceDefault)

// WithUploadHTTPClient sets a custom HTTP client for the service.
// This is primarily used for testing to inject mock HTTP clients.
func WithUploadHTTPClient(client *http.Client) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.config.HTTPClient = client
	}
}

// WithUploadAccountClient sets a custom account client for the upload service.
// This is primarily used for testing to inject mock clients.
func WithUploadAccountClient(client portalsdk.AccountAPI) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.config.AccountClient = client
	}
}

// WithUploadAuthService sets the auth service for token exchange (needed for TUS uploads).
func WithUploadAuthService(authSvc AuthService) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.authService = authSvc
	}
}

// WithUploadPinningService sets the pinning service for verifying pin status after upload.
func WithUploadPinningService(ps PinningService) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.pinningService = ps
	}
}

// WithAccountEndpoint sets a custom account API endpoint for auth/operations calls.
func WithAccountEndpoint(endpoint string) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.config.AccountEndpoint = endpoint
	}
}

// WithIPFSEndpoint sets a custom IPFS endpoint for upload calls.
func WithIPFSEndpoint(endpoint string) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.config.IPFSEndpoint = endpoint
	}
}

// NewUploadService creates a new UploadService with the given dependencies.
func NewUploadService(cfgMgr config.Manager, output Output, opts ...UploadServiceOption) UploadService {
	cfg := cfgMgr.Config()
	s := &UploadServiceDefault{
		accountClient:   portalsdk.NewClient(portalsdk.WithEndpoint(cfg.GetAPIEndpoint())),
		accountEndpoint: cfg.GetAPIEndpoint(),
		ipfsEndpoint:    cfg.GetIPFSEndpointSecure(),
		configMgr:       cfgMgr,
		output:          output,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.config.AccountClient != nil {
		s.accountClient = s.config.AccountClient
	}
	if s.config.AccountEndpoint != "" {
		s.accountEndpoint = s.config.AccountEndpoint
		if s.config.AccountClient == nil {
			s.accountClient = portalsdk.NewClient(portalsdk.WithEndpoint(s.accountEndpoint))
		}
	}
	if s.config.IPFSEndpoint != "" {
		s.ipfsEndpoint = s.config.IPFSEndpoint
	}
	return s
}

// WithAuthToken sets an auth token override that takes precedence over config.
func (s *UploadServiceDefault) WithAuthToken(token string) *UploadServiceDefault {
	s.authToken = token
	return s
}

// WithMemoryLimit sets a memory limit override that takes precedence over config.
func WithMemoryLimit(limit uint64) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.memoryLimit = limit
	}
}

// WithChunkSize sets the chunk size in bytes for UnixFS file splitting.
func WithChunkSize(size int64) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.chunkSize = size
	}
}

// WithChunkerStrategy sets the DAG layout strategy for UnixFS node generation.
func WithChunkerStrategy(strategy ipfs.ChunkerStrategy) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.chunkerStrategy = strategy
	}
}

// WithMaxLinks sets the maximum number of links per DAG node.
func WithMaxLinks(max int) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.maxLinks = max
	}
}

// RequireAuthenticated checks if the service is authenticated and returns an error if not.
func (s *UploadServiceDefault) RequireAuthenticated() error {
	authToken := s.getAuthToken()
	if authToken == "" {
		return fmt.Errorf("not authenticated: please run 'pinner auth' first or provide --auth-token")
	}
	return nil
}

// getAuthToken returns the auth token to use, with override taking precedence.
func (s *UploadServiceDefault) getAuthToken() string {
	if s.authToken != "" {
		return s.authToken
	}
	return s.configMgr.Config().AuthToken
}

// Upload uploads a file or directory to IPFS and optionally pins it.
//
// When wrap is true and the filesystem is a single file, the SDK wraps the
// file in a root directory so the resulting CID root is a directory (required
// for website content). Directory filesystems are already a directory root.
func (s *UploadServiceDefault) Upload(ctx context.Context, filesystem fs.FS, name string, wait bool, wrap bool) (*UploadResult, error) {
	startTime := time.Now()

	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using IPFS endpoint: %s", s.ipfsEndpoint)

	// Resolve the auth token; exchange API key JWT for login JWT if needed
	authToken, err := s.resolveAuthToken(ctx)
	if err != nil {
		return nil, err
	}

	// Determine upload limit from account client
	uploadLimit, err := s.accountClient.UploadLimit(ctx)
	if err != nil {
		uploadLimit = ipfs.DefaultUploadLimit
	}

	// Build SDK upload options
	memoryLimit := s.memoryLimit
	if memoryLimit == 0 {
		memoryLimit = s.configMgr.Config().GetMemoryLimitBytes()
	}

	opts := &ipfs.UploadOptions{
		MemoryLimit:     memoryLimit,
		UploadLimit:     uploadLimit,
		ChunkSize:       s.chunkSize,
		ChunkerStrategy: s.chunkerStrategy,
		MaxLinks:        s.maxLinks,
		WrapInDir:       wrap,
	}

	// Create SDK upload service using the configured IPFS endpoint
	sdkUpload, err := ipfs.NewUploadService(s.ipfsEndpoint, authToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload service: %w", err)
	}

	// Perform upload via SDK
	sdkResult, err := sdkUpload.UploadFromFS(ctx, filesystem, name, opts)
	if err != nil {
		return nil, s.wrapUploadError(err)
	}

	s.output.PrintVerbosef("Uploaded CAR with root CID: %s", sdkResult.CID)

	// Handle post-upload wait for pin
	if wait && sdkResult.CID != "" {
		if err := s.waitForPin(ctx, sdkResult.CID, authToken); err != nil {
			return nil, err
		}
	}

	// Apply the requested pin Name so the pin carries the caller's label rather
	// than the server default (the in-CAR filename is the object name, not the
	// pin's Name metadata). This runs whenever a pinning service is wired in,
	// i.e. the CLI waited path (upload.go only injects one when waiting) and
	// the MCP path (root.go always injects one). The call is synchronous: the
	// pinning service is only present on the one-shot CLI when the process stays
	// alive for the waited path, and the MCP server is long-lived, so a
	// background goroutine is never needed.
	//
	// The name-set is best-effort on every path: it is a post-upload metadata
	// label, never part of the upload's core success, so a failure must never
	// turn an already-succeeded upload into an error. It runs on a fresh context
	// with its own upload timeout (decoupled from the nearly-exhausted upload
	// deadline, still propagating Ctrl+C) so a hanging pinning backend cannot
	// block the process indefinitely; failures are downgraded to a warning.
	//
	// It is retried briefly: on the fire-and-forget path the pin may not have
	// registered in the pinning API yet, so UpdatePin's LsSync can report
	// ErrPinNotFound and silently drop the caller's name. A short fixed-delay
	// retry gives the pin time to appear before we give up and warn.
	if name != "" && s.pinningService != nil && sdkResult.CID != "" {
		nameCtx, cancel := s.freshTimeoutCtx(ctx)
		err := retry.New(
			retry.Context(nameCtx),
			retry.Attempts(3),
			retry.Delay(2*time.Second),
			retry.LastErrorOnly(true),
		).Do(func() error {
			return s.setPinName(nameCtx, sdkResult.CID, name)
		})
		cancel()
		if err != nil {
			s.output.PrintVerbosef("warning: could not set pin name %q on %s: %v", name, sdkResult.CID, err)
		}
	}

	duration := time.Since(startTime)

	return &UploadResult{
		CID:      sdkResult.CID,
		Size:     sdkResult.Size,
		Duration: duration,
		Location: sdkResult.Location,
	}, nil
}

// setPinName writes the caller-supplied name to the pin's Name metadata in a
// single synchronous UpdatePin call.
func (s *UploadServiceDefault) setPinName(ctx context.Context, cidStr, name string) error {
	if err := s.pinningService.UpdatePin(ctx, cidStr, name, nil, false); err != nil {
		return fmt.Errorf("%w: failed to set pin name %q on %s", err, name, cidStr)
	}
	return nil
}

// resolveAuthToken returns the auth token to use for uploads.
// If the stored token is an API key JWT, it exchanges it for a login JWT
// (required by TUS endpoint which rejects API key tokens).
func (s *UploadServiceDefault) resolveAuthToken(ctx context.Context) (string, error) {
	authToken := s.getAuthToken()

	// If we have an AuthService, use it to exchange API key for login JWT
	if s.authService != nil {
		purpose, err := GetJWTPurpose(authToken)
		if err != nil {
			s.output.PrintVerbosef("Could not decode JWT purpose, using raw token: %v", err)
			return authToken, nil
		}
		if purpose == "api" {
			s.output.PrintVerbose("Detected API key JWT, exchanging for login token for upload")
			loginJWT, err := s.accountClient.LoginWithAPIKey(ctx, authToken)
			if err != nil {
				return "", fmt.Errorf("failed to exchange API key for upload: %w", err)
			}
			return loginJWT, nil
		}
	}

	return authToken, nil
}

// wrapUploadError wraps SDK upload errors with appropriate CLI error types.
func (s *UploadServiceDefault) wrapUploadError(err error) error {
	if err == nil {
		return nil
	}
	return WrapAuthError("Upload", err)
}

// freshTimeoutCtx builds a context with its own upload-timeout deadline,
// decoupled from the parent's (possibly nearly-exhausted) deadline while still
// propagating user-initiated cancellation (Ctrl+C/SIGINT). Because the parent
// deadline may be mostly consumed by the upload, post-upload work (pin
// polling, name application) uses a fresh timeout rather than inheriting the
// parent's; only a user cancel aborts it.
func (s *UploadServiceDefault) freshTimeoutCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	// base is the user-cancel propagation root. It is deliberately bound once
	// and never reassigned, so the watcher goroutine below captures an
	// immutable value: rebinding the same variable after spawning the goroutine
	// would race the goroutine's read of it (detected by -race in CI).
	base, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				cancel()
			}
		case <-base.Done():
		}
	}()
	child, timeoutCancel := context.WithTimeout(base, s.configMgr.Config().GetUploadTimeout())
	return child, func() {
		timeoutCancel()
		cancel()
	}
}

// waitForPin waits for a file to be pinned by waiting on both the account
// operation (which validates the pin exists) and the pin status (which marks
// the pin done). Verification confirms the pin is actually "pinned"; if the
// pinning API briefly reports the pin as not-found (404) while the operation is
// still being processed, waitForPin falls back to the account operation to
// confirm the pin exists and keeps waiting rather than failing.
//
// The pin wait uses a fresh context with the upload timeout, decoupled from
// the upload's context deadline. This is necessary because pin processing on
// the server (DAG block downloads from renterd) can take longer than the
// upload itself, and the upload context may have already consumed most of
// its deadline by the time pinning starts.
func (s *UploadServiceDefault) waitForPin(ctx context.Context, rootCID string, authToken string) error {
	accountClient := portalsdk.NewClient(portalsdk.WithEndpoint(s.accountEndpoint), portalsdk.WithJWT(authToken))

	// Fresh context with its own timeout, decoupled from the upload's deadline
	// while propagating user cancellation.
	pinCtx, cancel := s.freshTimeoutCtx(ctx)
	defer cancel()

	op, err := statuspkg.FindOperation(pinCtx, accountClient, rootCID)
	if err != nil {
		if errors.Is(err, coreerrors.ErrPinNotFound) {
			return fmt.Errorf("%w for CID %s. Check 'pinner status %s'", ErrOperationNotFound, rootCID, rootCID)
		}
		return fmt.Errorf("%w: %v", ErrOperationFailed, err)
	}

	_, err = accountClient.WaitForOperation(pinCtx, int64(op.Id),
		portalsdk.WithPollInterval(2*time.Second),
		portalsdk.WithPollTimeout(s.configMgr.Config().GetUploadTimeout()),
	)
	if err != nil {
		return fmt.Errorf("Pin operation failed for CID %s: %w. Check 'pinner status %s'", rootCID, err, rootCID)
	}

	if s.pinningService != nil {
		s.output.PrintVerbosef("Account operation completed, verifying pin status for CID %s", rootCID)
		err := retry.New(
			retry.Context(pinCtx),
			retry.Attempts(10),
			retry.Delay(2*time.Second),
			retry.DelayType(retry.BackOffDelay),
			retry.MaxDelay(30*time.Second),
			retry.LastErrorOnly(true),
		).Do(func() error {
			status, err := s.pinningService.Status(pinCtx, rootCID, false)
			if err != nil {
				// Edge case: the pinning API can report the pin as not-found
				// (404) while the upload's account operation is still being
				// processed. The account operation only validates that the pin
				// exists, so confirm it here and, when present, keep waiting on
				// the pin status until it is actually marked pinned. Only a
				// missing operation is a genuine not-found.
				if errors.Is(err, ErrPinNotFound) {
					if _, opErr := statuspkg.FindOperation(pinCtx, accountClient, rootCID); opErr == nil {
						return fmt.Errorf("pin not yet registered for CID %s, waiting", rootCID)
					}
				}
				return err
			}
			if status.Status != "pinned" {
				return fmt.Errorf("pin status is %s, expected pinned", status.Status)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("Pin verification failed for CID %s: %w. Check 'pinner status %s'", rootCID, err, rootCID)
		}
		s.output.PrintVerbosef("Pin verified for CID %s", rootCID)
	}

	return nil
}
