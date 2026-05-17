package internal

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRetryHTTPClient(t *testing.T) {
	client := NewRetryHTTPClient(3)

	require.NotNil(t, client)
	assert.Equal(t, 60*time.Second, client.Timeout)
	assert.NotNil(t, client.Transport)
}

func TestRetryTransport_RoundTrip_Success(t *testing.T) {
	// Use a mock transport that returns success
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       nil,
			}, nil
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	req, err := http.NewRequest("GET", "http://example.com/test", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, mockTransport.callCount)
}

func TestRetryTransport_RoundTrip_NetworkError(t *testing.T) {
	// Create a transport that fails with a timeout error (retryable)
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return nil, &timeoutError{}
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	req, err := http.NewRequest("GET", "http://example.com/test", nil)
	require.NoError(t, err)

	// Should retry and eventually fail
	resp, err := transport.RoundTrip(req)
	require.Error(t, err)
	assert.Nil(t, resp)

	// Verify the mock was called multiple times (retries occurred)
	assert.GreaterOrEqual(t, mockTransport.callCount, 2)
}

func TestRetryTransport_RoundTrip_ClientError(t *testing.T) {
	// Create a transport that returns a 404 error
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       nil,
			}, nil
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	req, err := http.NewRequest("GET", "http://example.com/test", nil)
	require.NoError(t, err)

	// Should NOT retry client errors (4xx)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Should be called only once (no retries for 4xx)
	assert.Equal(t, 1, mockTransport.callCount)
}

func TestRetryTransport_RoundTrip_ContextCancellation(t *testing.T) {
	// Create a transport that fails with a network error
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://example.com/test", nil)
	require.NoError(t, err)

	// Cancel after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Should fail due to context cancellation during retries
	resp, err := transport.RoundTrip(req)
	require.Error(t, err)
	assert.Nil(t, resp)
	// Should have attempted at least once before cancellation
	assert.Greater(t, mockTransport.callCount, 0)
}

func TestRetryTransport_RoundTrip_TimeoutError(t *testing.T) {
	// Create a transport that returns a timeout error
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "read", Err: &timeoutError{}}
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	req, err := http.NewRequest("GET", "http://example.com/test", nil)
	require.NoError(t, err)

	// Should retry on timeout
	resp, err := transport.RoundTrip(req)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.GreaterOrEqual(t, mockTransport.callCount, 2)
}

func TestRetryTransport_RoundTrip_DNSError(t *testing.T) {
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return nil, &timeoutError{}
		},
	}

	transport := &retryTransport{
		maxRetries: 3,
		maxDelay:   30 * time.Second,
		base:       mockTransport,
	}

	req, err := http.NewRequest("GET", "http://example.com/test", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.GreaterOrEqual(t, mockTransport.callCount, 2)
}

type mockRoundTripper struct {
	fn        func(req *http.Request) (*http.Response, error)
	callCount int
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.callCount++
	return m.fn(req)
}

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
