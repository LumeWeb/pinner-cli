package cli

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/avast/retry-go/v4"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil/filter"
)

// UploadServiceConfig holds configuration options for UploadService.
type UploadServiceConfig struct {
	HTTPClient       *http.Client
	AccountClient    portalsdk.AccountAPI
	IPFSEndpoint     string
	AccountEndpoint  string
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
		return fmt.Errorf("not authenticated: please run 'pinner auth login' first or provide --auth-token")
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
func (s *UploadServiceDefault) Upload(ctx context.Context, filesystem fs.FS, name string, wait bool) (*UploadResult, error) {
	startTime := time.Now()

	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using IPFS endpoint: %s", s.ipfsEndpoint)

	// Resolve the auth token — exchange API key JWT for login JWT if needed
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
		MemoryLimit:    memoryLimit,
		UploadLimit:    uploadLimit,
		ChunkSize:      s.chunkSize,
		ChunkerStrategy: s.chunkerStrategy,
		MaxLinks:       s.maxLinks,
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

	duration := time.Since(startTime)

	return &UploadResult{
		CID:      sdkResult.CID,
		Size:     sdkResult.Size,
		Duration: duration,
	}, nil
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

// waitForPin waits for a file to be pinned by first waiting for the account
// operation to complete, then verifying the pin exists in the pinning API.
func (s *UploadServiceDefault) waitForPin(ctx context.Context, rootCID string, authToken string) error {
	accountClient := portalsdk.NewClient(portalsdk.WithEndpoint(s.accountEndpoint), portalsdk.WithJWT(authToken))

	operations, _, err := accountClient.ListOperations(ctx, portalsdk.WithFilters(filter.FieldEqual("cid", rootCID)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOperationFailed, err)
	}

	if len(operations) == 0 {
		return fmt.Errorf("%w for CID %s. Check 'pinner status %s'", ErrOperationNotFound, rootCID, rootCID)
	}

	_, err = accountClient.WaitForOperation(ctx, int64(operations[0].Id))
	if err != nil {
		return fmt.Errorf("Pin operation failed for CID %s: %w. Check 'pinner status %s'", rootCID, err, rootCID)
	}

	if s.pinningService != nil {
		s.output.PrintVerbosef("Account operation completed, verifying pin status for CID %s", rootCID)
		err := retry.Do(func() error {
			status, err := s.pinningService.Status(ctx, rootCID, false)
			if err != nil {
				return err
			}
			if status.Status != "pinned" {
				return fmt.Errorf("pin status is %s, expected pinned", status.Status)
			}
			return nil
		},
			retry.Context(ctx),
			retry.Attempts(10),
			retry.Delay(2*time.Second),
			retry.DelayType(retry.BackOffDelay),
			retry.MaxDelay(30*time.Second),
			retry.LastErrorOnly(true),
		)
		if err != nil {
			return fmt.Errorf("Pin verification failed for CID %s: %w. Check 'pinner status %s'", rootCID, err, rootCID)
		}
		s.output.PrintVerbosef("Pin verified for CID %s", rootCID)
	}

	return nil
}
