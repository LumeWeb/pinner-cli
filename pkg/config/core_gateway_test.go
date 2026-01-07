package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetGatewayEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		gatewayEndpoint  string
		baseEndpoint     string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "uses configured gateway endpoint",
			gatewayEndpoint:  "https://dweb.link",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://dweb.link/ipfs/",
		},
		{
			name:             "adds /ipfs/ suffix if missing",
			gatewayEndpoint:  "https://dweb.link",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://dweb.link/ipfs/",
		},
		{
			name:             "preserves existing /ipfs/ suffix",
			gatewayEndpoint:  "https://dweb.link/ipfs/",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://dweb.link/ipfs/",
		},
		{
			name:             "adds /ipfs/ when only trailing slash missing",
			gatewayEndpoint:  "https://dweb.link/",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://dweb.link/ipfs/",
		},
		{
			name:             "falls back to ipfs subdomain when gateway not configured",
			gatewayEndpoint:  "",
			baseEndpoint:     "pinner.xyz",
			secure:           true,
			expectedEndpoint: "https://ipfs.pinner.xyz/ipfs/",
		},
		{
			name:             "falls back to default when nothing configured",
			gatewayEndpoint:  "",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://ipfs.pinner.xyz/ipfs/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GatewayEndpoint: tt.gatewayEndpoint,
				BaseEndpoint:    tt.baseEndpoint,
				Secure:          tt.secure,
			}

			result := cfg.GetGatewayEndpoint()
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestGetGatewayEndpointSecure(t *testing.T) {
	tests := []struct {
		name             string
		gatewayEndpoint  string
		baseEndpoint     string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "uses configured gateway endpoint (secure)",
			gatewayEndpoint:  "https://dweb.link",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://dweb.link/ipfs/",
		},
		{
			name:             "uses configured gateway endpoint (insecure)",
			gatewayEndpoint:  "http://localhost:8080",
			baseEndpoint:     "",
			secure:           false,
			expectedEndpoint: "http://localhost:8080/ipfs/",
		},
		{
			name:             "falls back to ipfs subdomain when gateway not configured (secure)",
			gatewayEndpoint:  "",
			baseEndpoint:     "pinner.xyz",
			secure:           true,
			expectedEndpoint: "https://ipfs.pinner.xyz/ipfs/",
		},
		{
			name:             "falls back to ipfs subdomain when gateway not configured (insecure)",
			gatewayEndpoint:  "",
			baseEndpoint:     "pinner.xyz",
			secure:           false,
			expectedEndpoint: "http://ipfs.pinner.xyz/ipfs/",
		},
		{
			name:             "falls back to default when nothing configured (secure)",
			gatewayEndpoint:  "",
			baseEndpoint:     "",
			secure:           true,
			expectedEndpoint: "https://ipfs.pinner.xyz/ipfs/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GatewayEndpoint: tt.gatewayEndpoint,
				BaseEndpoint:    tt.baseEndpoint,
				Secure:          tt.secure,
			}

			result := cfg.GetGatewayEndpointSecure()
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}
