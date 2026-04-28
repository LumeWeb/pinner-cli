package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAdminEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		expectedEndpoint string
	}{
		{
			name:             "uses configured base endpoint",
			baseEndpoint:     "pinner.xyz",
			expectedEndpoint: "https://admin.pinner.xyz",
		},
		{
			name:             "falls back to default when base endpoint not configured",
			baseEndpoint:     "",
			expectedEndpoint: "https://admin.pinner.xyz",
		},
		{
			name:             "uses custom domain",
			baseEndpoint:     "example.com",
			expectedEndpoint: "https://admin.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.baseEndpoint,
				Secure:       true,
			}

			result := cfg.GetAdminEndpoint()
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestGetAdminEndpointSecure(t *testing.T) {
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
			expectedEndpoint: "https://admin.pinner.xyz",
		},
		{
			name:             "uses http when secure is false",
			baseEndpoint:     "pinner.xyz",
			secure:           false,
			expectedEndpoint: "http://admin.pinner.xyz",
		},
		{
			name:             "falls back to default with secure",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://admin.pinner.xyz",
		},
		{
			name:             "falls back to default with insecure",
			baseEndpoint:     "",
			secure:           false,
			expectedEndpoint: "http://admin.pinner.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.baseEndpoint,
				Secure:       tt.secure,
			}

			result := cfg.GetAdminEndpointSecure()
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestGetAdminEndpointWithSecure(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		secureParam      bool
		expectedEndpoint string
	}{
		{
			name:             "uses https when secure param is true",
			baseEndpoint:     "pinner.xyz",
			secureParam:      true,
			expectedEndpoint: "https://admin.pinner.xyz",
		},
		{
			name:             "uses http when secure param is false",
			baseEndpoint:     "pinner.xyz",
			secureParam:      false,
			expectedEndpoint: "http://admin.pinner.xyz",
		},
		{
			name:             "overrides config secure setting",
			baseEndpoint:     "pinner.xyz",
			secureParam:      true,
			expectedEndpoint: "https://admin.pinner.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.baseEndpoint,
				Secure:       false, // Should be overridden by secureParam
			}

			result := cfg.GetAdminEndpointWithSecure(tt.secureParam)
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}
