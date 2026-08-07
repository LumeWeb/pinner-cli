package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	portalsdk "go.lumeweb.com/portal-sdk"
)

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "JSON with error field",
			body:     `{"error":"Upload quota exceeded"}`,
			expected: "Upload quota exceeded",
		},
		{
			name:     "JSON with nested error details",
			body:     `{"error":"Upload quota exceeded. Please try again later.: Failed to process upload: upload quota validation failed: upload quota exceeded"}`,
			expected: "Upload quota exceeded. Please try again later.: Failed to process upload: upload quota validation failed: upload quota exceeded",
		},
		{
			name:     "JSON without error field",
			body:     `{"message":"something went wrong"}`,
			expected: `{"message":"something went wrong"}`,
		},
		{
			name:     "JSON with empty error field",
			body:     `{"error":""}`,
			expected: `{"error":""}`,
		},
		{
			name:     "JSON with nested object error - details",
			body:     `{"error":{"reason":"ValidationFailed","details":"DNS validation failed"}}`,
			expected: "DNS validation failed",
		},
		{
			name:     "JSON with nested object error - details empty, reason used",
			body:     `{"error":{"reason":"ValidationFailed"}}`,
			expected: "ValidationFailed",
		},
		{
			name:     "JSON with nested object error - both empty",
			body:     `{"error":{"reason":"","details":""}}`,
			expected: `{"error":{"reason":"","details":""}}`,
		},
		{
			name:     "plain text",
			body:     "internal server error",
			expected: "internal server error",
		},
		{
			name:     "empty body",
			body:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractErrorMessage(tt.body))
		})
	}
}

func TestHTTPError(t *testing.T) {
	t.Run("Error method", func(t *testing.T) {
		err := NewHTTPError(429, `{"error":"quota exceeded"}`)
		assert.Equal(t, "HTTP 429: quota exceeded", err.Error())
	})

	t.Run("non-JSON body", func(t *testing.T) {
		err := NewHTTPError(500, "internal server error")
		assert.Equal(t, "HTTP 500: internal server error", err.Error())
	})

	t.Run("trailing whitespace trimmed", func(t *testing.T) {
		err := NewHTTPError(429, `{"error":"quota exceeded"}`+"\n")
		assert.Equal(t, "HTTP 429: quota exceeded", err.Error())
	})
}

func TestFormatError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		verbose  bool
		contains string
	}{
		{"nil error", nil, false, ""},
		{"known error", ErrNotAuthenticated, false, "Not authenticated"},
		{"known error verbose", ErrNotAuthenticated, true, "Details:"},
		{"wrapped known error", fmt.Errorf("wrap: %w", ErrPinningFailed), false, "Pinning operation failed"},
		{"unknown error", errors.New("something weird"), false, "something weird"},
		{"unknown error verbose", errors.New("something weird"), true, "Details:"},
		{"context canceled", context.Canceled, false, "cancelled"},
		{"context deadline", context.DeadlineExceeded, false, "timed out"},
		{"sdk unauthorized", portalsdk.ErrUnauthorized, false, "re-authenticate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatError(tt.err, tt.verbose)
			if tt.err == nil {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

type testNetErr struct {
	timeout bool
}

func (e *testNetErr) Error() string   { return "network error" }
func (e *testNetErr) Timeout() bool   { return e.timeout }
func (e *testNetErr) Temporary() bool { return e.timeout }

func TestIsNetworkError(t *testing.T) {
	t.Run("timeout net.Error", func(t *testing.T) {
		var netErr net.Error = &testNetErr{timeout: true}
		assert.True(t, isNetworkError(netErr))
	})

	t.Run("non-timeout net.Error", func(t *testing.T) {
		var netErr net.Error = &testNetErr{timeout: false}
		assert.False(t, isNetworkError(netErr))
	})

	t.Run("non-network error", func(t *testing.T) {
		assert.False(t, isNetworkError(errors.New("not network")))
	})
}

func TestFormatError_NetworkError(t *testing.T) {
	var netErr net.Error = &testNetErr{timeout: true}
	result := FormatError(netErr, false)
	assert.Contains(t, result, "Connection error")
}

func TestWrapAuthError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.NoError(t, WrapAuthError("test", nil))
	})

	t.Run("non-auth error", func(t *testing.T) {
		err := WrapAuthError("upload", errors.New("disk full"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload failed")
		assert.NotContains(t, err.Error(), "authentication")
	})

	t.Run("sdk unauthorized error", func(t *testing.T) {
		err := WrapAuthError("pin", portalsdk.ErrUnauthorized)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication expired")
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestWrapFileError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.NoError(t, WrapFileError("read", "file.txt", nil))
	})

	t.Run("file not found", func(t *testing.T) {
		err := WrapFileError("read", "missing.txt", os.ErrNotExist)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrFileNotFound))
	})

	t.Run("permission denied", func(t *testing.T) {
		err := WrapFileError("write", "protected.txt", os.ErrPermission)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrPermissionDenied))
	})

	t.Run("other error", func(t *testing.T) {
		err := WrapFileError("read", "file.txt", errors.New("io error"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read failed")
	})
}
