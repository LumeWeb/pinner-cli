package internal

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/avast/retry-go/v4"
	go_pinning_service_http_client "github.com/ipfs/boxo/pinning/remote/client"
	"github.com/ipfs/go-cid"
)

// BoxoClientConfig holds configuration for BoxoPinningClient retry behavior.
type BoxoClientConfig struct {
	MaxRetries uint
	MaxDelay   time.Duration
	MaxJitter  time.Duration
}

// DefaultBoxoClientConfig returns the default retry configuration.
func DefaultBoxoClientConfig() BoxoClientConfig {
	return BoxoClientConfig{
		MaxRetries: 3,
		MaxDelay:   30 * time.Second,
		MaxJitter:  5 * time.Second,
	}
}

// BoxoClientOption configures a BoxoPinningClient.
type BoxoClientOption func(*BoxoClientConfig)

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(maxRetries uint) BoxoClientOption {
	return func(cfg *BoxoClientConfig) {
		cfg.MaxRetries = maxRetries
	}
}

// WithMaxDelay sets the maximum delay between retries.
func WithMaxDelay(maxDelay time.Duration) BoxoClientOption {
	return func(cfg *BoxoClientConfig) {
		cfg.MaxDelay = maxDelay
	}
}

// WithMaxJitter sets the maximum jitter to add to delays.
func WithMaxJitter(maxJitter time.Duration) BoxoClientOption {
	return func(cfg *BoxoClientConfig) {
		cfg.MaxJitter = maxJitter
	}
}

// BoxoPinningClient wraps the boxo pinning service HTTP client.
type BoxoPinningClient struct {
	client *go_pinning_service_http_client.Client
	config BoxoClientConfig
}

// NewBoxoPinningClient creates a new BoxoPinningClient wrapping the boxo client.
// Uses default retry configuration unless options are provided.
func NewBoxoPinningClient(endpoint, authToken string, opts ...BoxoClientOption) PinningClient {
	cfg := DefaultBoxoClientConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	client := go_pinning_service_http_client.NewClient(endpoint, authToken)
	return &BoxoPinningClient{client: client, config: cfg}
}

// Add implements PinningClient.Add.
func (c *BoxoPinningClient) Add(ctx context.Context, cid cid.Cid, opts ...go_pinning_service_http_client.AddOption) (go_pinning_service_http_client.PinStatusGetter, error) {
	var result go_pinning_service_http_client.PinStatusGetter
	err := retry.Do(
		func() error {
			var err error
			result, err = c.client.Add(ctx, cid, opts...)
			return err
		},
		retry.Attempts(c.config.MaxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(c.config.MaxJitter),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.RetryIf(func(err error) bool {
			return isRetryableError(err)
		}),
	)
	return result, err
}

// LsSync implements PinningClient.LsSync.
func (c *BoxoPinningClient) LsSync(ctx context.Context, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error) {
	var result []go_pinning_service_http_client.PinStatusGetter
	err := retry.Do(
		func() error {
			var err error
			result, err = c.client.LsSync(ctx, opts...)
			return err
		},
		retry.Attempts(c.config.MaxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(c.config.MaxJitter),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.RetryIf(func(err error) bool {
			return isRetryableError(err)
		}),
	)
	return result, err
}

// LsWithLimit implements PinningClient.LsWithLimit.
//
// boxo's Ls pages through every result regardless of the Limit option (limit
// is only the per-request page size), so calling LsSync with a small limit
// still returns the whole set. This instead drives boxo's streaming Ls and
// stops reading once `limit` results arrive, canceling the request. The stream
// can't be retried once partially consumed, so this uses a single attempt.
func (c *BoxoPinningClient) LsWithLimit(ctx context.Context, limit int, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error) {
	lsCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resCh, errCh := c.client.GoLs(lsCtx, opts...)
	var out []go_pinning_service_http_client.PinStatusGetter
	for r := range resCh {
		out = append(out, r)
		if len(out) >= limit {
			cancel()
			break
		}
	}

	// The background goroutine always sends exactly one value. Reaching the cap
	// cancels Ls, which reports context.Canceled - expected, not a failure.
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	return out, nil
}

// GetStatusByID implements PinningClient.GetStatusByID.
func (c *BoxoPinningClient) GetStatusByID(ctx context.Context, pinID string) (go_pinning_service_http_client.PinStatusGetter, error) {
	var result go_pinning_service_http_client.PinStatusGetter
	err := retry.Do(
		func() error {
			var err error
			result, err = c.client.GetStatusByID(ctx, pinID)
			return err
		},
		retry.Attempts(c.config.MaxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(c.config.MaxJitter),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.RetryIf(func(err error) bool {
			return isRetryableError(err)
		}),
	)
	return result, err
}

// DeleteByID implements PinningClient.DeleteByID.
func (c *BoxoPinningClient) DeleteByID(ctx context.Context, pinID string) error {
	return retry.Do(
		func() error {
			return c.client.DeleteByID(ctx, pinID)
		},
		retry.Attempts(c.config.MaxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(c.config.MaxJitter),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.RetryIf(func(err error) bool {
			return isRetryableError(err)
		}),
	)
}

// Replace implements PinningClient.Replace.
func (c *BoxoPinningClient) Replace(ctx context.Context, pinID string, cid cid.Cid, opts ...go_pinning_service_http_client.AddOption) (go_pinning_service_http_client.PinStatusGetter, error) {
	var result go_pinning_service_http_client.PinStatusGetter
	err := retry.Do(
		func() error {
			var err error
			result, err = c.client.Replace(ctx, pinID, cid, opts...)
			return err
		},
		retry.Attempts(c.config.MaxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(c.config.MaxJitter),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.RetryIf(func(err error) bool {
			return isRetryableError(err)
		}),
	)
	return result, err
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation or timeout - don't retry these
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Check for network errors that are retryable
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return true
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsTemporary {
			return true
		}
		return false
	}

	// Check for URL errors (connection errors) - unwrap and check recursively
	if urlErr, ok := err.(*url.Error); ok {
		return isRetryableError(urlErr.Unwrap())
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsTemporary {
		return true
	}

	// Check for connection errors - unwrap and check recursively
	if opErr, ok := err.(*net.OpError); ok {
		// Check if the underlying error is retryable
		return isRetryableError(opErr.Unwrap())
	}

	return false
}
