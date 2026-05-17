package internal

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/avast/retry-go/v4"
)

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxRetries uint
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		MaxDelay:   30 * time.Second,
	}
}

// NewRetryHTTPClient creates an HTTP client with retry logic for network failures.
// It retries on temporary network errors (timeouts, connection refused, DNS errors, etc.)
// with exponential backoff. Non-retryable errors (4xx client errors) are not retried.
func NewRetryHTTPClient(maxRetries uint) *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &retryTransport{
			maxRetries: maxRetries,
			maxDelay:   30 * time.Second,
			base:       http.DefaultTransport,
		},
	}
}

// retryTransport wraps an http.RoundTripper with retry logic.
type retryTransport struct {
	maxRetries uint
	maxDelay   time.Duration
	base       http.RoundTripper
}

// RoundTrip implements http.RoundTripper with retry logic.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	retryErr := retry.Do(
		func() error {
			resp, err = t.base.RoundTrip(req)
			if err != nil {
				if t.isRetryableError(err) {
					return err
				}
				return retry.Unrecoverable(err)
			}

			// Don't retry client errors (4xx) or server errors that shouldn't be retried
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return retry.Unrecoverable(nil)
			}

			return nil
		},
		retry.Attempts(t.maxRetries),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(t.maxDelay),
		retry.LastErrorOnly(true),
		retry.Context(req.Context()),
	)

	if retryErr != nil {
		return nil, retryErr
	}

	return resp, nil
}

// isRetryableError checks if an error is retryable.
func (t *retryTransport) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors that are retryable
	if netErr, ok := err.(net.Error); ok {
		// Retry on timeout or temporary errors
		return netErr.Timeout()
	}

	// Check for URL errors (connection errors) - unwrap and check recursively
	if urlErr, ok := err.(*url.Error); ok {
		return t.isRetryableError(urlErr.Unwrap())
	}

	// Check for DNS errors
	if _, ok := err.(*net.DNSError); ok {
		return true
	}

	// Check for connection errors - unwrap and check recursively
	if opErr, ok := err.(*net.OpError); ok {
		return t.isRetryableError(opErr.Unwrap())
	}

	return false
}
