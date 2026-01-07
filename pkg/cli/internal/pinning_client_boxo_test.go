package internal

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAuthToken = "test-token"

func TestIsRetryableError_NetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "timeout error",
			err:  &timeoutError{},
			want: true,
		},
		{
			name: "temporary error",
			err:  &temporaryError{},
			want: true,
		},
		{
			name: "DNS error with Temporary()",
			err:  &net.DNSError{Err: "no such host", IsTemporary: true},
			want: true,
		},
		{
			name: "connection error with Temporary()",
			err:  &net.OpError{Op: "dial", Err: &temporaryError{}},
			want: true,
		},
		{
			name: "URL error with network error",
			err:  &url.Error{Err: &temporaryError{}},
			want: true,
		},
		{
			name: "URL error with timeout",
			err:  &url.Error{Err: &timeoutError{}},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewBoxoPinningClient(t *testing.T) {
	client := NewBoxoPinningClient("https://api.example.com", testAuthToken)

	require.NotNil(t, client)
	boxoClient, ok := client.(*BoxoPinningClient)
	require.True(t, ok)
	assert.Equal(t, uint(3), boxoClient.config.MaxRetries)
	assert.Equal(t, 30*time.Second, boxoClient.config.MaxDelay)
	assert.Equal(t, 5*time.Second, boxoClient.config.MaxJitter)
}

func TestNewBoxoPinningClientWithMaxRetries(t *testing.T) {
	client := NewBoxoPinningClient("https://api.example.com", testAuthToken, WithMaxRetries(5))

	require.NotNil(t, client)
	boxoClient, ok := client.(*BoxoPinningClient)
	require.True(t, ok)
	assert.Equal(t, uint(5), boxoClient.config.MaxRetries)
	assert.Equal(t, 30*time.Second, boxoClient.config.MaxDelay)
	assert.Equal(t, 5*time.Second, boxoClient.config.MaxJitter)
}

func TestNewBoxoPinningClientWithCustomConfig(t *testing.T) {
	client := NewBoxoPinningClient(
		"https://api.example.com",
		testAuthToken,
		WithMaxRetries(7),
		WithMaxDelay(60*time.Second),
		WithMaxJitter(10*time.Second),
	)

	require.NotNil(t, client)
	boxoClient, ok := client.(*BoxoPinningClient)
	require.True(t, ok)
	assert.Equal(t, uint(7), boxoClient.config.MaxRetries)
	assert.Equal(t, 60*time.Second, boxoClient.config.MaxDelay)
	assert.Equal(t, 10*time.Second, boxoClient.config.MaxJitter)
}

func TestDefaultBoxoClientConfig(t *testing.T) {
	cfg := DefaultBoxoClientConfig()
	assert.Equal(t, uint(3), cfg.MaxRetries)
	assert.Equal(t, 30*time.Second, cfg.MaxDelay)
	assert.Equal(t, 5*time.Second, cfg.MaxJitter)
}

type temporaryError struct{}

func (e *temporaryError) Error() string   { return "temporary" }
func (e *temporaryError) Timeout() bool   { return false }
func (e *temporaryError) Temporary() bool { return true }
