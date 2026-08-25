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
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/cli/internal"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
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

// WithSDKPinningService sets the ipfs-sdk pinning service used for server-side
// list search (useful for testing the match=partial path without a live endpoint).
func WithSDKPinningService(svc ipfs.PinningService) PinningServiceOption {
	return func(s *PinningServiceDefault) {
		s.sdkPinningSvc = svc
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
	// sdkPinningSvc lists pins via the ipfs-sdk pinning service, which can send
	// the spec's match=partial substring name filter server-side (boxo cannot).
	sdkPinningSvc ipfs.PinningService
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
		s.sdkPinningSvc = newSDKPinningService(apiEndpoint, authToken)
	}

	return s
}

// newSDKPinningService builds the ipfs-sdk pinning service used for server-side
// list search. On failure it returns nil; the service then falls back to the
// boxo client's list which still works but without match=partial search.
func newSDKPinningService(apiEndpoint, authToken string) ipfs.PinningService {
	client, err := ipfs.NewClient(apiEndpoint, authToken)
	if err != nil {
		return nil
	}
	return client.Pinning()
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
		// ErrInvalidCID already reads "invalid CID"; wrapping it with a
		// "invalid CID: " prefix duplicated the message on the wire.
		return nil, ErrInvalidCID
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

// List returns a list of pinned content with the shared list options. Name is
// an exact name match; Search is a server-side substring name match
// (match=partial) composed with the other filters. Both filters, plus status,
// are evaluated server-side via the ipfs-sdk pinning service so results are
// never post-filtered client-side. The ipfs pinning-service spec has no
// server-side offset, so Start paging is applied client-side.
func (s *PinningServiceDefault) List(ctx context.Context, opts pinning.ListOptions) ([]Pin, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	if s.sdkPinningSvc != nil {
		pins, err := s.listViaSDK(ctx, opts)
		if err != nil {
			return nil, wrapPinningError("List pins", err, ErrPinningFailed)
		}
		return pins, nil
	}

	// No SDK pinning service (auth/build failure): fall back to boxo, which
	// cannot send match=partial, so search is unavailable on this path. Keep the
	// exact name/status filters server-side.
	s.output.PrintVerbosef("ipfs-sdk pinning service unavailable; listing via boxo without substring search")
	pins, err := s.listViaBoxo(ctx, opts)
	if err != nil {
		return nil, wrapPinningError("List pins", err, ErrPinningFailed)
	}
	return pins, nil
}

// listViaSDK lists pins through the ipfs-sdk pinning service, sending the
// name/status/search filters as server-side query params (name, status,
// match=partial for search).
func (s *PinningServiceDefault) listViaSDK(ctx context.Context, opts pinning.ListOptions) ([]Pin, error) {
	o := []ipfs.ListOption{}
	if opts.Search != "" {
		// Server-side substring name search (IPFS Pinning Services spec's
		// match=partial), via the SDK's name-partial helper so the match
		// strategy type stays encapsulated.
		o = append(o, ipfs.WithFilterNamePartial(opts.Search))
	} else if opts.Name != "" {
		// Exact name match, sent with an explicit match=exact strategy. The
		// suffix-search path above pins match=partial; without a declared match
		// the name would ride bare and pinning-service backends that require an
		// explicit strategy would ignore it (returning the full list) even
		// though status/limit still filter.
		o = append(o, ipfs.WithFilterName(opts.Name), ipfs.WithFilterMatch(ipfs.MatchExact))
	}
	if opts.Status != "" {
		o = append(o, ipfs.WithFilterStatus(ipfs.PinStatusEnum(opts.Status)))
	}
	// The pinning-service spec has no offset, so when paging we fetch
	// Start+Limit rows and slice off the first Start client-side. Only set a
	// server limit when paging with a positive Limit; a Start with no Limit
	// must fetch the full list and rely on the client-side slice in pagePins
	// (otherwise we would fetch only Start rows and then truncate to empty).
	if opts.Limit > 0 {
		o = append(o, ipfs.WithPinningLimit(int32(opts.Start+opts.Limit)))
	}

	statuses, err := s.sdkPinningSvc.ListPins(ctx, o...)
	if err != nil {
		return nil, err
	}

	pins := lo.Map(statuses, func(ps ipfs.PinStatus, _ int) Pin {
		name := ""
		if ps.Pin.Name != nil {
			name = *ps.Pin.Name
		}
		return Pin{
			CID:       ps.Pin.Cid,
			Name:      name,
			Status:    string(ps.PinStatusEnum),
			Created:   ps.Created.Format(time.RFC3339),
			RequestID: ps.Requestid,
			Metadata:  mapMeta(ps.Pin.Meta),
		}
	})
	return pagePins(pins, opts.Start, opts.Limit), nil
}

// pagePins applies the client-side Start/Limit slice when the backend has no
// server-side offset (ipfs pinning service). A zero Limit means "keep all
// rows from Start onward".
func pagePins(pins []Pin, start, limit int) []Pin {
	if start > 0 {
		if start >= len(pins) {
			return []Pin{}
		}
		pins = pins[start:]
	}
	if limit > 0 && len(pins) > limit {
		pins = pins[:limit]
	}
	return pins
}

// mapMeta converts the SDK's *PinMeta metadata to a plain map.
func mapMeta(meta *ipfs.PinMeta) map[string]string {
	if meta == nil {
		return map[string]string{}
	}
	return map[string]string(*meta)
}

// listViaBoxo lists pins through the boxo client. It preserves the historical
// behavior (server-side name/status filters; no match=partial substring search)
// as a fallback when the SDK pinning service could not be constructed.
func (s *PinningServiceDefault) listViaBoxo(ctx context.Context, opts pinning.ListOptions) ([]Pin, error) {
	o := []go_pinning_service_http_client.LsOption{}
	if opts.Name != "" {
		o = append(o, go_pinning_service_http_client.PinOpts.FilterName(opts.Name))
	}
	if opts.Status != "" {
		o = append(o, go_pinning_service_http_client.PinOpts.FilterStatus(go_pinning_service_http_client.Status(opts.Status)))
	}

	var (
		results []go_pinning_service_http_client.PinStatusGetter
		err     error
	)
	// Only cap the boxo fetch with a positive Limit; a Start with no Limit must
	// fetch the full list so pagePins can offset without truncating to empty
	// (see listViaSDK for the same reasoning).
	if opts.Limit > 0 {
		results, err = s.pinningClient.LsWithLimit(ctx, opts.Start+opts.Limit, o...)
	} else {
		results, err = s.pinningClient.LsSync(ctx, o...)
	}
	if err != nil {
		return nil, err
	}

	pins := lo.Map(results, func(r go_pinning_service_http_client.PinStatusGetter, _ int) Pin {
		pin := r.GetPin()
		return Pin{
			CID:       pin.GetCid().String(),
			Name:      pin.GetName(),
			Status:    r.GetStatus().String(),
			Created:   r.GetCreated().Format(time.RFC3339),
			RequestID: r.GetRequestId(),
			Metadata:  pin.GetMeta(),
		}
	})
	return pagePins(pins, opts.Start, opts.Limit), nil
}

// Status returns the status of a pin.
func (s *PinningServiceDefault) Status(ctx context.Context, cidStr string, watch bool) (*PinStatus, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	parsedCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, ErrInvalidCID
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
		return nil, ErrInvalidCID
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

			// Terminate once the pin settles instead of polling forever. An
			// MCP agent cannot press Ctrl+C, so without this a watch on an
			// already-pinned CID blocks indefinitely. Mirrors the request-level
			// watch below (StatusPinned/StatusFailed exit).
			switch go_pinning_service_http_client.Status(status.Status) {
			case go_pinning_service_http_client.StatusPinned:
				return status, nil
			case go_pinning_service_http_client.StatusFailed:
				return nil, fmt.Errorf("pin failed for CID %s: %w", cidStr, ErrPinningFailed)
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

	pins, err := s.List(ctx, pinning.ListOptions{Status: statusFilter})
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
