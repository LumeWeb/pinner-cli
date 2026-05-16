package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigKeyToEnvVar(t *testing.T) {
	testCases := []struct {
		name     string
		key      string
		expected string
	}{
		{"auth_token", "auth_token", "PINNER_AUTH_TOKEN"},
		{"base_endpoint", "base_endpoint", "PINNER_BASE_ENDPOINT"},
		{"max_retries", "max_retries", "PINNER_MAX_RETRIES"},
		{"memory_limit", "memory_limit", "PINNER_MEMORY_LIMIT"},
		{"secure", "secure", "PINNER_SECURE"},
		{"gateway_endpoint", "gateway_endpoint", "PINNER_GATEWAY_ENDPOINT"},
		{"hyphenated key", "some-key", "PINNER_SOME_KEY"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := configKeyToEnvVar(tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}
