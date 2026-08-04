package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMetaEndpointWithSecure(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "uses https when secure is true",
			baseEndpoint:     "pinner.xyz",
			secure:           true,
			expectedEndpoint: "https://meta.pinner.xyz",
		},
		{
			name:             "uses http when secure is false",
			baseEndpoint:     "pinner.xyz",
			secure:           false,
			expectedEndpoint: "http://meta.pinner.xyz",
		},
		{
			name:             "falls back to default with secure",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://meta.pinner.xyz",
		},
		{
			name:             "uses custom domain",
			baseEndpoint:     "example.com",
			secure:           true,
			expectedEndpoint: "https://meta.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.baseEndpoint,
				Secure:       tt.secure,
			}

			result := cfg.GetMetaEndpointWithSecure(tt.secure)
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}
