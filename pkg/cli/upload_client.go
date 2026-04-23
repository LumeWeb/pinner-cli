package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"github.com/bdragon300/tusgo"
	"github.com/docker/go-units"
	"github.com/ipfs/go-cid"
	"go.lumeweb.com/ipfs-content/car"
	"go.lumeweb.com/ipfs-content/unixfs"
	"go.lumeweb.com/pinner-cli/pkg/cli/internal"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil/filter"
)

// DefaultUploadLimit is the default upload limit in bytes (100MB).
const DefaultUploadLimit = 100 * units.MiB

// UploadServiceConfig holds configuration options for UploadService.
type UploadServiceConfig struct {
	HTTPClient    *http.Client
	AccountClient portalsdk.AccountAPI
}

// UploadServiceDefault provides upload operations using the Pinner.xyz API.
type UploadServiceDefault struct {
	accountClient portalsdk.AccountAPI
	configMgr     config.Manager
	output        Output
	apiEndpoint   string
	authToken     string
	memoryLimit   uint64
	config        UploadServiceConfig
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

// NewUploadService creates a new UploadService with the given dependencies.
func NewUploadService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...UploadServiceOption) UploadService {
	s := &UploadServiceDefault{
		accountClient: portalsdk.NewClient(portalsdk.WithEndpoint(apiEndpoint)),
		configMgr:     cfgMgr,
		output:        output,
		apiEndpoint:   apiEndpoint,
		config: UploadServiceConfig{
			HTTPClient: internal.NewRetryHTTPClient(uint(cfgMgr.Config().MaxRetries)),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.config.AccountClient != nil {
		s.accountClient = s.config.AccountClient
	}
	return s
}

// WithAuthToken sets an auth token override that takes precedence over config.
func (s *UploadServiceDefault) WithAuthToken(token string) *UploadServiceDefault {
	s.authToken = token
	return s
}

// memoryLimitOverride stores a runtime override for memory limit
type memoryLimitOverride struct {
	limit uint64
}

// WithMemoryLimit sets a memory limit override that takes precedence over config.
func WithMemoryLimit(limit uint64) UploadServiceOption {
	return func(s *UploadServiceDefault) {
		s.memoryLimit = limit
	}
}

// RequireAuthenticated checks if the service is authenticated and returns an error if not.
func (s *UploadServiceDefault) RequireAuthenticated() error {
	authToken := s.authToken
	if authToken == "" {
		authToken = s.configMgr.Config().AuthToken
	}
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

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	// Check if filesystem wraps a directory
	fileInfo, err := fs.Stat(filesystem, ".")
	if err != nil {
		return nil, fmt.Errorf("cannot access filesystem: %w", err)
	}

	var cid string
	var totalSize int64
	maxMemory := s.memoryLimit
	if maxMemory == 0 {
		maxMemory = s.configMgr.Config().GetMemoryLimitBytes()
	}

	if fileInfo.IsDir() {
		cid, totalSize, err = s.uploadWithCAR(ctx, filesystem, name, maxMemory, true, wait)
	} else {
		// Single file - determine file size for memory limit
		if info, err := fs.Stat(filesystem, name); err == nil {
			maxMemory = uint64(info.Size()) + 1024*1024
		}
		cid, totalSize, err = s.uploadWithCAR(ctx, filesystem, name, maxMemory, false, wait)
	}

	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)

	return &UploadResult{
		CID:      cid,
		Size:     totalSize,
		Duration: duration,
	}, nil
}

func (s *UploadServiceDefault) getUploadLimit(ctx context.Context) int64 {
	uploadLimit, err := s.accountClient.UploadLimit(ctx)
	if err != nil {
		return DefaultUploadLimit
	}
	return uploadLimit
}

// uploadWithCAR generates a CAR and routes to POST or TUS based on size.
func (s *UploadServiceDefault) uploadWithCAR(ctx context.Context, filesystem fs.FS, name string, maxMemory uint64, wrapInDir bool, wait bool) (string, int64, error) {
	// Build tree summary first to get CID and calculate CAR size
	bs, dagService := car.NewDAGServiceWithMemoryLimit(maxMemory)
	generator := unixfs.NewUnixFSNodeGenerator(
		unixfs.WithUnixFSNodeDAGService(dagService),
		unixfs.WithUnixFSNodeBlockstore(bs),
	)
	builder := car.NewCARBuilder(bs, dagService, generator)
	summary, err := builder.BuildSummary(ctx, filesystem, wrapInDir)
	if err != nil {
		return "", 0, fmt.Errorf("Failed to prepare upload: %w. Try reducing --memory-limit if this is a large directory", err)
	}

	// Calculate CAR size to determine upload method
	carSize, err := car.CalculateCARSize(summary)
	if err != nil {
		return "", 0, fmt.Errorf("Failed to calculate upload size: %w", err)
	}
	uploadLimit := s.getUploadLimit(ctx)

	// Create pipe for streaming CAR
	pr, pw := io.Pipe()

	go func() {
		err := builder.WriteCAR(ctx, pw)
		if err != nil {
			_ = pw.CloseWithError(err)
		} else {
			if err := pw.Close(); err != nil {
				_ = pw.CloseWithError(err)
			}
		}
	}()

	// Route based on CAR size
	if carSize <= uploadLimit {
		return s.uploadViaPOST(ctx, summary.RootCID, pr, name, wait, carSize)
	}

	return s.uploadViaTUS(ctx, summary.RootCID, pr, carSize, name, wait)
}

// uploadViaPOST uploads via HTTP POST as multipart form.
func (s *UploadServiceDefault) uploadViaPOST(ctx context.Context, rootCID cid.Cid, carReader io.Reader, name string, wait bool, carSize int64) (string, int64, error) {
	s.output.PrintVerbosef("Uploading via CAR (HTTP POST)")

	// Wrap carReader with progress tracking if not in JSON/quiet mode
	shouldShowProgress := !s.output.IsJSON() && !s.output.IsQuiet()
	var progressWriter *ProgressWriter
	if shouldShowProgress && carSize > 0 {
		progressWriter = NewProgressWriter(carReader, carSize, true, "Uploading")
		if err := progressWriter.Start(); err != nil {
			return "", 0, err
		}
		defer progressWriter.Stop()
		carReader = progressWriter
	}

	// Create a pipe for streaming multipart form
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	type result struct {
		err error
	}

	// Channel to capture results from multipart writing
	resultChan := make(chan result, 1)

	// Write CAR to multipart form in goroutine
	go func() {
		var resultErr error

		defer func() {
			// Close writer first to finalize multipart form
			if err := writer.Close(); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("failed to close multipart writer: %w", err)
			}
			// Then close pipe writer
			if err := pw.Close(); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("failed to close pipe writer: %w", err)
			}
			// Always send result
			resultChan <- result{err: resultErr}
			close(resultChan)
		}()

		// Create form file field
		part, err := writer.CreateFormFile("file", name+".car")
		if err != nil {
			resultErr = fmt.Errorf("failed to create form file: %w", err)
			return
		}

		// Copy CAR from reader to multipart form
		if _, err := io.Copy(part, carReader); err != nil {
			resultErr = fmt.Errorf("failed to write CAR to multipart form: %w", err)
			return
		}
	}()

	// Upload the CAR as multipart form
	uploadEndpoint := s.configMgr.Config().GetUploadEndpointSecure()
	authToken := s.getAuthToken()

	uploadErr := s.postUpload(ctx, uploadEndpoint, authToken, pr, writer.FormDataContentType())

	// If the upload fails, the reading side of the pipe is abandoned.
	// We must close the pipe writer to unblock the writing goroutine, allowing it to terminate gracefully.
	if uploadErr != nil {
		pw.CloseWithError(uploadErr)
	}

	// Always wait for the multipart writing goroutine to complete to prevent leaks.
	res := <-resultChan
	if res.err != nil {
		// The writer goroutine failed, which is a more specific and critical error.
		return "", 0, res.err
	}

	// If the upload itself failed, return that error now that we've cleaned up the goroutine.
	if uploadErr != nil {
		return "", 0, WrapAuthError("Upload", uploadErr)
	}

	s.output.PrintVerbosef("Uploaded CAR with root CID: %s", rootCID)
	cid, _ := s.handlePostUpload(ctx, rootCID, wait)
	return cid, carSize, nil
}

// uploadViaTUS uploads via TUS protocol for large files/directories.
func (s *UploadServiceDefault) uploadViaTUS(ctx context.Context, rootCID cid.Cid, carReader io.Reader, carSize int64, name string, wait bool) (string, int64, error) {
	s.output.PrintVerbosef("Uploading via TUS")

	// Create TUS client
	tusEndpoint := s.configMgr.Config().GetTUSEndpointSecure()
	baseURL, err := url.Parse(tusEndpoint)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse TUS endpoint: %w", err)
	}

	tusClient := tusgo.NewClient(s.config.HTTPClient, baseURL).WithContext(ctx)

	// Create upload on server
	_upload := &tusgo.Upload{}
	_, err = tusClient.CreateUpload(_upload, carSize, false, nil)
	if err != nil {
		return "", 0, fmt.Errorf("Failed to start upload: %w. Check your network connection", WrapAuthError("Create TUS upload", err))
	}

	s.output.PrintVerbosef("Created TUS upload at: %s", _upload.Location)

	// Upload CAR data
	stream := tusgo.NewUploadStream(tusClient, _upload)

	// Wrap carReader with progress tracking if not in JSON/quiet mode
	shouldShowProgress := !s.output.IsJSON() && !s.output.IsQuiet()
	var progressWriter *ProgressWriter
	if shouldShowProgress && carSize > 0 {
		progressWriter = NewProgressWriter(carReader, carSize, true, "Uploading via TUS")
		if err := progressWriter.Start(); err != nil {
			return "", 0, err
		}
		defer progressWriter.Stop()
		carReader = progressWriter
	}

	written, err := io.Copy(stream, carReader)
	if err != nil {
		return "", 0, fmt.Errorf("Upload interrupted: %w. Try again to resume", err)
	}

	s.output.PrintVerbosef("TUS upload completed: %d bytes", written)

	cid, _ := s.handlePostUpload(ctx, rootCID, wait)
	return cid, carSize, nil
}

// handlePostUpload handles post-upload operations like waiting for pin.
func (s *UploadServiceDefault) handlePostUpload(ctx context.Context, rootCID cid.Cid, wait bool) (string, int64) {
	if wait {
		if err := s.waitForPin(ctx, rootCID.String()); err != nil {
			s.output.PrintVerbosef("Warning: failed to wait for pin: %v", err)
		}
	}
	return rootCID.String(), 0
}

func (s *UploadServiceDefault) postUpload(ctx context.Context, endpoint, authToken string, body io.Reader, contentType string) error {
	// Create HTTP request with streaming body
	// endpoint already includes protocol from GetUploadEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", contentType)

	resp, err := s.config.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.output.PrintVerbosef("Warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("Authentication expired or invalid. Run 'pinner auth login' to re-authenticate")
		}
		return fmt.Errorf("Upload failed (HTTP %d): %s. Try again or contact support", resp.StatusCode, string(respBody))
	}

	return nil
}

// waitForPin waits for a file to be pinned by querying operations by CID.
func (s *UploadServiceDefault) waitForPin(ctx context.Context, rootCID string) error {
	authToken := s.getAuthToken()
	accountClient := portalsdk.NewClient(portalsdk.WithEndpoint(s.apiEndpoint), portalsdk.WithJWT(authToken))

	operations, err := accountClient.ListOperations(ctx, portalsdk.WithFilters(filter.FieldEqual("cid", rootCID)))
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

	return nil
}
