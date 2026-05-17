package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
