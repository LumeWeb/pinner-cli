package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/ipfs/boxo/pinning/remote/client"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multiaddr"
	"github.com/samber/lo"
	"go.lumeweb.com/pinner-cli/pkg/cli/internal"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

var boxoAuthRe = regexp.MustCompile(`http error 40[13]`)

// WrapNetworkError wraps errors with network troubleshooting hints.
func WrapNetworkError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w. Check your internet connection and API endpoint", operation, err)
}

// isBoxoAuthError checks if an error is a 401 from the boxo pinning client.
// Boxo returns errors like "remote pinning service returned http error 401: ...".
func isBoxoAuthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, portalsdk.ErrUnauthorized) {
		return true
	}
	return boxoAuthRe.MatchString(err.Error())
}

// wrapPinningError wraps errors from pinning operations with proper auth detection.
// If the error is a 401, it wraps with ErrNotAuthenticated for clear messaging.
// Otherwise, it wraps the original error.
func wrapPinningError(operation string, err error, wrapErr error) error {
	if err == nil {
		return nil
	}
	if isBoxoAuthError(err) {
		return fmt.Errorf("%s failed - authentication expired or invalid. Run 'pinner auth' to re-authenticate: %w", operation, ErrNotAuthenticated)
	}
	return fmt.Errorf("%s failed: %w", operation, err)
}

// PinningServiceOption configures a PinningService.
type PinningServiceOption func(*PinningServiceDefault)

// WithPinningClient sets a custom pinning client (useful for testing).
func WithPinningClient(client internal.PinningClient) PinningServiceOption {
	return func(s *PinningServiceDefault) {
		s.pinningClient = client
	}
}

// WithPinningClientFactory sets a custom client factory for creating pinning clients.
func WithPinningClientFactory(factory internal.PinningClientFactory) PinningServiceOption {
	return func(s *PinningServiceDefault) {
		s.clientFactory = factory
	}
}

// WithAuthToken sets an auth token override that takes precedence over config.
func WithAuthToken(token string) PinningServiceOption {
	return func(s *PinningServiceDefault) {
		s.authToken = token
	}
}

// NewUnpinResult creates a new UnpinResult with the given CID.
func NewUnpinResult(cid string) *UnpinResult {
	return &UnpinResult{CID: cid}
}

// NewPinResult creates a new PinResult with the given details.
func NewPinResult(cid, requestID, status string) *PinResult {
	return &PinResult{
		CID:       cid,
		RequestID: requestID,
		Status:    status,
	}
}

// PinningServiceDefault provides pinning operations using the IPFS pinning service API.
type PinningServiceDefault struct {
	pinningClient internal.PinningClient
	configMgr     config.Manager
	output        Output
	apiEndpoint   string
	clientFactory internal.PinningClientFactory
	authToken     string
}

// NewPinningService creates a new PinningService with the given dependencies.
func NewPinningService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...PinningServiceOption) PinningService {
	maxRetries := uint(cfgMgr.Config().MaxRetries)

	s := &PinningServiceDefault{
		configMgr:   cfgMgr,
		output:      output,
		apiEndpoint: apiEndpoint,
		clientFactory: func(endpoint, authToken string) internal.PinningClient {
			return internal.NewBoxoPinningClient(endpoint, authToken, internal.WithMaxRetries(maxRetries))
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	// Only fetch auth token if we need to create a default client
	if s.pinningClient == nil {
		authToken := s.authToken
		if authToken == "" {
			authToken = cfgMgr.Config().AuthToken
		}
		if authToken == "" {
			// Return service with nil client - will return error on actual operations
			return s
		}
		s.pinningClient = s.clientFactory(apiEndpoint, authToken)
	}

	return s
}

// RequireAuthenticated checks if the service is authenticated and returns an error if not.
func (s *PinningServiceDefault) RequireAuthenticated() error {
	if s.pinningClient == nil {
		return fmt.Errorf("not authenticated: please run 'pinner auth' first or provide --auth-token: %w", ErrNotAuthenticated)
	}
	return nil
}

// Pin pins existing content by CID.
func (s *PinningServiceDefault) Pin(ctx context.Context, cidStr, name string, wait bool) (*PinResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	parsedCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CID: %w", ErrInvalidCID)
	}

	opts := []go_pinning_service_http_client.AddOption{}
	if name != "" {
		opts = append(opts, go_pinning_service_http_client.PinOpts.WithName(name))
	}

	result, err := s.pinningClient.Add(ctx, parsedCid, opts...)
	if err != nil {
		return nil, wrapPinningError("Pin", err, ErrPinningFailed)
	}

	if wait {
		if err := s.waitForPinCompletion(ctx, result.GetRequestId()); err != nil {
			return nil, err
		}
	}

	return NewPinResult(cidStr, result.GetRequestId(), result.GetStatus().String()), nil
}

// List returns a list of pinned content with optional filters.
func (s *PinningServiceDefault) List(ctx context.Context, nameFilter string, limit int, statusFilter string) ([]Pin, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	opts := []go_pinning_service_http_client.LsOption{}
	if nameFilter != "" {
		opts = append(opts, go_pinning_service_http_client.PinOpts.FilterName(nameFilter))
	}
	if limit > 0 {
		opts = append(opts, go_pinning_service_http_client.PinOpts.Limit(limit))
	}
	if statusFilter != "" {
		opts = append(opts, go_pinning_service_http_client.PinOpts.FilterStatus(go_pinning_service_http_client.Status(statusFilter)))
	}

	results, err := s.pinningClient.LsSync(ctx, opts...)
	if err != nil {
		return nil, wrapPinningError("List pins", err, ErrPinningFailed)
	}

	pins := lo.Map(results, func(r go_pinning_service_http_client.PinStatusGetter, _ int) Pin {
		pin := r.GetPin()
		return Pin{
			CID:       pin.GetCid().String(),
			Name:      pin.GetName(),
			Status:    r.GetStatus().String(),
			Created:   r.GetCreated().Format(time.DateTime),
			RequestID: r.GetRequestId(),
			Metadata:  pin.GetMeta(),
		}
	})

	return pins, nil
}

// Status returns the status of a pin.
func (s *PinningServiceDefault) Status(ctx context.Context, cidStr string, watch bool) (*PinStatus, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	parsedCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CID: %w", ErrInvalidCID)
	}

	results, err := s.pinningClient.LsSync(ctx, go_pinning_service_http_client.PinOpts.FilterCIDs(parsedCid))
	if err != nil {
		return nil, WrapAuthError("Get pin status", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("Pin not found for CID %s: %w", cidStr, ErrPinNotFound)
	}

	result := results[0]

	// Convert delegates from []multiaddr.Multiaddr to []string
	delegates := lo.Map(result.GetDelegates(), func(d multiaddr.Multiaddr, _ int) string {
		return d.String()
	})

	status := &PinStatus{
		CID:       result.GetPin().GetCid().String(),
		Status:    string(result.GetStatus()),
		Created:   result.GetCreated().Format(time.RFC3339),
		Delegates: delegates,
	}

	if watch {
		return s.watchPinStatus(ctx, cidStr)
	}

	return status, nil
}

// Unpin removes a pin by CID.
func (s *PinningServiceDefault) Unpin(ctx context.Context, cidStr string, confirm bool) (*UnpinResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	if !confirm {
		s.output.Printfln("Use --force to unpin CID: %s", cidStr)
		return NewUnpinResult(cidStr), nil
	}

	parsedCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CID: %w", ErrInvalidCID)
	}

	results, err := s.pinningClient.LsSync(ctx, go_pinning_service_http_client.PinOpts.FilterCIDs(parsedCid))
	if err != nil {
		return nil, WrapAuthError("Find pin", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("Pin not found for CID %s: %w", cidStr, ErrPinNotFound)
	}

	requestID := results[0].GetRequestId()
	err = s.pinningClient.DeleteByID(ctx, requestID)
	if err != nil {
		return nil, WrapAuthError("Unpin", err)
	}

	return NewUnpinResult(cidStr), nil
}

// UpdateMetadata updates metadata for a pin.
func (s *PinningServiceDefault) UpdateMetadata(ctx context.Context, cidStr string, set []string, clear bool) error {
	return s.UpdatePin(ctx, cidStr, "", set, clear)
}

// UpdatePin updates name and/or metadata for a pin.
func (s *PinningServiceDefault) UpdatePin(ctx context.Context, cidStr string, name string, set []string, clear bool) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	if len(set)%2 != 0 {
		return fmt.Errorf("metadata key-value pairs must be provided in pairs")
	}

	parsedCid, err := cid.Decode(cidStr)
	if err != nil {
		return fmt.Errorf("invalid CID: %w", err)
	}

	results, err := s.pinningClient.LsSync(ctx, go_pinning_service_http_client.PinOpts.FilterCIDs(parsedCid))
	if err != nil {
		return WrapAuthError("Find pin", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("%w: %s", ErrPinNotFound, cidStr)
	}

	requestID := results[0].GetRequestId()
	currentPin := results[0].GetPin()

	meta := currentPin.GetMeta()
	if meta == nil {
		meta = make(map[string]string)
	}

	if clear {
		meta = make(map[string]string)
	}

	for i := 0; i < len(set); i += 2 {
		if i+1 < len(set) {
			meta[set[i]] = set[i+1]
		}
	}

	opts := []go_pinning_service_http_client.AddOption{}
	if name != "" {
		opts = append(opts, go_pinning_service_http_client.PinOpts.WithName(name))
	}
	if len(meta) > 0 {
		opts = append(opts, go_pinning_service_http_client.PinOpts.AddMeta(meta))
	}

	_, err = s.pinningClient.Replace(ctx, requestID, parsedCid, opts...)
	if err != nil {
		return WrapAuthError("Update pin", err)
	}

	if name != "" && len(set) > 0 {
		s.output.Printfln("Updated name and metadata for CID: %s", cidStr)
	} else if name != "" {
		s.output.Printfln("Updated name for CID: %s", cidStr)
	} else {
		s.output.Printfln("Updated metadata for CID: %s", cidStr)
	}
	return nil
}

// waitForPinCompletion waits for a pin to complete.
func (s *PinningServiceDefault) waitForPinCompletion(ctx context.Context, requestID string) error {
	s.output.PrintVerbosef("Waiting for pin completion...")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			result, err := s.pinningClient.GetStatusByID(ctx, requestID)
			if err != nil {
				return WrapAuthError("Check pin status", err)
			}

			status := result.GetStatus()
			s.output.PrintFields(FieldGroup{
				Fields: []Field{
					{"Status", status.String()},
				},
			})

			if status == go_pinning_service_http_client.StatusPinned {
				s.output.Print("Pinning completed successfully")
				return nil
			}

			if status == go_pinning_service_http_client.StatusFailed {
				return ErrPinningFailed
			}
		}
	}
}

// watchPinStatus continuously watches a pin's status.
func (s *PinningServiceDefault) watchPinStatus(ctx context.Context, cidStr string) (*PinStatus, error) {
	s.output.PrintFields(FieldGroup{
		Title:  fmt.Sprintf("Watching status for CID: %s", cidStr),
		Fields: []Field{{"Instructions", "Press Ctrl+C to stop"}},
	})

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus *PinStatus

	for {
		select {
		case <-ctx.Done():
			if lastStatus != nil {
				s.output.PrintFields(FieldGroup{
					Title:  "Status watch stopped",
					PadTop: 1,
					Fields: []Field{
						{"CID", lastStatus.CID},
						{"Status", lastStatus.Status},
					},
				})
			}
			return lastStatus, nil
		case <-ticker.C:
			status, err := s.Status(ctx, cidStr, false)
			if err != nil {
				return nil, err
			}

			if lastStatus == nil || lastStatus.Status != status.Status {
				s.output.PrintFields(FieldGroup{
					PadTop: 1,
					Fields: []Field{
						{"Status", status.Status},
					},
				})
				lastStatus = status
			}
		}
	}
}

// PinBatch pins multiple CIDs in parallel using workerpool.
func (s *PinningServiceDefault) PinBatch(ctx context.Context, cids []string, name string, opts BatchOptions) (*BatchResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	if len(cids) == 0 {
		return &BatchResult{}, nil
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	startTime := time.Now()

	result := &BatchResult{
		Total:     len(cids),
		Succeeded: make([]OperationResult, 0, len(cids)),
		Failed:    make([]OperationError, 0),
		Skipped:   make([]string, 0),
	}

	var mu sync.Mutex
	var firstError error

	// Create batch progress tracker if enabled and not in JSON/quiet mode
	shouldShowProgress := opts.Progress && !s.output.IsJSON() && !s.output.IsQuiet()
	var progress *BatchProgressTracker
	if shouldShowProgress {
		progress = NewBatchProgressTracker(len(cids), true, "Pinning CIDs")
		if err := progress.Start(); err != nil {
			return nil, err
		}
		defer func() { _ = progress.Stop() }()
	}

	wp := workerpool.New(parallel)
	defer wp.Stop()

	for _, cidStr := range cids {
		_cid := cidStr
		wp.Submit(func() {
			pinResult, err := s.Pin(ctx, _cid, name, opts.Wait)

			if progress != nil {
				progress.Increment()
			}

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if opts.ContinueOn {
					result.Failed = append(result.Failed, OperationError{
						CID:   _cid,
						Error: err.Error(),
					})
					return
				}
				if firstError == nil {
					firstError = err
				}
				return
			}

			if pinResult != nil {
				result.Succeeded = append(result.Succeeded, OperationResult{
					CID:       pinResult.CID,
					RequestID: pinResult.RequestID,
					Status:    pinResult.Status,
				})
			}
		})
	}

	wp.StopWait()
	result.Duration = time.Since(startTime)

	if firstError != nil {
		return result, firstError
	}

	return result, nil
}

// UnpinBatch unpins multiple CIDs in parallel using workerpool.
func (s *PinningServiceDefault) UnpinBatch(ctx context.Context, cids []string, opts BatchOptions) (*BatchResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	if len(cids) == 0 {
		return &BatchResult{}, nil
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	startTime := time.Now()

	result := &BatchResult{
		Total:     len(cids),
		Succeeded: make([]OperationResult, 0, len(cids)),
		Failed:    make([]OperationError, 0),
		Skipped:   make([]string, 0),
	}

	var mu sync.Mutex
	var firstError error

	shouldShowProgress := opts.Progress && !s.output.IsJSON() && !s.output.IsQuiet()
	var progress *BatchProgressTracker
	if shouldShowProgress {
		progress = NewBatchProgressTracker(len(cids), true, "Unpinning CIDs")
		if err := progress.Start(); err != nil {
			return nil, err
		}
		defer func() { _ = progress.Stop() }()
	}

	wp := workerpool.New(parallel)
	defer wp.Stop()

	for _, cidStr := range cids {
		cid := cidStr
		wp.Submit(func() {
			unpinResult, err := s.Unpin(ctx, cid, true)

			if progress != nil {
				progress.Increment()
			}

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if opts.ContinueOn {
					result.Failed = append(result.Failed, OperationError{
						CID:   cid,
						Error: err.Error(),
					})
					return
				}
				if firstError == nil {
					firstError = err
				}
				return
			}

			if unpinResult != nil {
				result.Succeeded = append(result.Succeeded, OperationResult{
					CID: unpinResult.CID,
				})
			}
		})
	}

	wp.StopWait()
	result.Duration = time.Since(startTime)

	if firstError != nil {
		return result, firstError
	}

	return result, nil
}

func (s *PinningServiceDefault) UnpinAll(ctx context.Context, statusFilter string, opts BatchOptions) (*BatchResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	pins, err := s.List(ctx, "", 0, statusFilter)
	if err != nil {
		return nil, err
	}

	if len(pins) == 0 {
		s.output.Printfln("No pins found")
		return &BatchResult{}, nil
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	startTime := time.Now()

	result := &BatchResult{
		Total:     len(pins),
		Succeeded: make([]OperationResult, 0, len(pins)),
		Failed:    make([]OperationError, 0),
		Skipped:   make([]string, 0),
	}

	var mu sync.Mutex
	var firstError error

	shouldShowProgress := opts.Progress && !s.output.IsJSON() && !s.output.IsQuiet()
	var progress *BatchProgressTracker
	if shouldShowProgress {
		progress = NewBatchProgressTracker(len(pins), true, "Unpinning pins")
		if err := progress.Start(); err != nil {
			return nil, err
		}
		defer func() { _ = progress.Stop() }()
	}

	wp := workerpool.New(parallel)
	defer wp.Stop()

	for _, pin := range pins {
		p := pin
		wp.Submit(func() {
			err := s.pinningClient.DeleteByID(ctx, p.RequestID)
			if err != nil {
				err = WrapAuthError("Unpin", err)
			}

			if progress != nil {
				progress.Increment()
			}

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if opts.ContinueOn {
					result.Failed = append(result.Failed, OperationError{
						CID:   p.CID,
						Error: err.Error(),
					})
					return
				}
				if firstError == nil {
					firstError = err
				}
				return
			}

			result.Succeeded = append(result.Succeeded, OperationResult{
				CID:       p.CID,
				RequestID: p.RequestID,
			})
		})
	}

	wp.StopWait()
	result.Duration = time.Since(startTime)

	if firstError != nil {
		return result, firstError
	}

	return result, nil
}
