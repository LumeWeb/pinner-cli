package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSubdomainEndpointWithProtocol_Localhost(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		subdomain        string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "account subdomain on localhost secure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainAccount,
			secure:           true,
			expectedEndpoint: "https://account.localhost",
		},
		{
			name:             "account subdomain on localhost insecure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainAccount,
			secure:           false,
			expectedEndpoint: "http://account.localhost",
		},
		{
			name:             "ipfs subdomain on localhost secure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainIPFS,
			secure:           true,
			expectedEndpoint: "https://ipfs.localhost",
		},
		{
			name:             "ipfs subdomain on localhost insecure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainIPFS,
			secure:           false,
			expectedEndpoint: "http://ipfs.localhost",
		},
		{
			name:             "admin subdomain on localhost secure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainAdmin,
			secure:           true,
			expectedEndpoint: "https://admin.localhost",
		},
		{
			name:             "admin subdomain on localhost insecure",
			baseEndpoint:     "http://localhost",
			subdomain:        SubdomainAdmin,
			secure:           false,
			expectedEndpoint: "http://admin.localhost",
		},
		{
			name:             "localhost without scheme",
			baseEndpoint:     "localhost",
			subdomain:        SubdomainAccount,
			secure:           true,
			expectedEndpoint: "https://account.localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubdomainEndpointWithProtocol(tt.baseEndpoint, tt.subdomain, tt.secure)
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestGetSubdomainEndpointWithProtocol_LocalhostWithPort(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		subdomain        string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "account subdomain on localhost:8080 secure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainAccount,
			secure:           true,
			expectedEndpoint: "https://account.localhost:8080",
		},
		{
			name:             "account subdomain on localhost:8080 insecure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainAccount,
			secure:           false,
			expectedEndpoint: "http://account.localhost:8080",
		},
		{
			name:             "ipfs subdomain on localhost:8080 secure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainIPFS,
			secure:           true,
			expectedEndpoint: "https://ipfs.localhost:8080",
		},
		{
			name:             "ipfs subdomain on localhost:8080 insecure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainIPFS,
			secure:           false,
			expectedEndpoint: "http://ipfs.localhost:8080",
		},
		{
			name:             "admin subdomain on localhost:8080 secure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainAdmin,
			secure:           true,
			expectedEndpoint: "https://admin.localhost:8080",
		},
		{
			name:             "admin subdomain on localhost:8080 insecure",
			baseEndpoint:     "http://localhost:8080",
			subdomain:        SubdomainAdmin,
			secure:           false,
			expectedEndpoint: "http://admin.localhost:8080",
		},
		{
			name:             "localhost:8080 without scheme",
			baseEndpoint:     "localhost:8080",
			subdomain:        SubdomainAccount,
			secure:           false,
			expectedEndpoint: "http://account.localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubdomainEndpointWithProtocol(tt.baseEndpoint, tt.subdomain, tt.secure)
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestConfigEndpointMethods_Localhost(t *testing.T) {
	cfg := &Config{
		BaseEndpoint: "http://localhost",
		Secure:       false,
	}

	assert.Equal(t, "http://account.localhost", cfg.GetAccountEndpoint())
	assert.Equal(t, "http://ipfs.localhost", cfg.GetIPFSEndpoint())
	assert.Equal(t, "http://admin.localhost", cfg.GetAdminEndpoint())
	assert.Equal(t, "http://ipfs.localhost/api/upload", cfg.GetUploadEndpoint())
	assert.Equal(t, "http://ipfs.localhost/api/upload/tus", cfg.GetTUSEndpoint())
	assert.Equal(t, "http://ipfs.localhost/ipfs/", cfg.GetGatewayEndpoint())
}

func TestConfigEndpointMethods_LocalhostWithPort(t *testing.T) {
	cfg := &Config{
		BaseEndpoint: "http://localhost:8080",
		Secure:       false,
	}

	assert.Equal(t, "http://account.localhost:8080", cfg.GetAccountEndpoint())
	assert.Equal(t, "http://ipfs.localhost:8080", cfg.GetIPFSEndpoint())
	assert.Equal(t, "http://admin.localhost:8080", cfg.GetAdminEndpoint())
	assert.Equal(t, "http://ipfs.localhost:8080/api/upload", cfg.GetUploadEndpoint())
	assert.Equal(t, "http://ipfs.localhost:8080/api/upload/tus", cfg.GetTUSEndpoint())
	assert.Equal(t, "http://ipfs.localhost:8080/ipfs/", cfg.GetGatewayEndpoint())
}

func TestConfigEndpointMethods_LocalhostSecure(t *testing.T) {
	cfg := &Config{
		BaseEndpoint: "http://localhost",
		Secure:       true,
	}

	assert.Equal(t, "https://account.localhost", cfg.GetAccountEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost", cfg.GetIPFSEndpointSecure())
	assert.Equal(t, "https://admin.localhost", cfg.GetAdminEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost/api/upload", cfg.GetUploadEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost/api/upload/tus", cfg.GetTUSEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost/ipfs/", cfg.GetGatewayEndpointSecure())
}

func TestConfigEndpointMethods_LocalhostWithPortSecure(t *testing.T) {
	cfg := &Config{
		BaseEndpoint: "http://localhost:8080",
		Secure:       true,
	}

	assert.Equal(t, "https://account.localhost:8080", cfg.GetAccountEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost:8080", cfg.GetIPFSEndpointSecure())
	assert.Equal(t, "https://admin.localhost:8080", cfg.GetAdminEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost:8080/api/upload", cfg.GetUploadEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost:8080/api/upload/tus", cfg.GetTUSEndpointSecure())
	assert.Equal(t, "https://ipfs.localhost:8080/ipfs/", cfg.GetGatewayEndpointSecure())
}

func TestConfigEndpointWithSecure_LocalhostOverrides(t *testing.T) {
	cfg := &Config{
		BaseEndpoint: "http://localhost:8080",
		Secure:       false,
	}

	assert.Equal(t, "https://account.localhost:8080", cfg.GetAccountEndpointWithSecure(true))
	assert.Equal(t, "http://account.localhost:8080", cfg.GetAccountEndpointWithSecure(false))
	assert.Equal(t, "https://ipfs.localhost:8080", cfg.GetIPFSEndpointWithSecure(true))
	assert.Equal(t, "http://ipfs.localhost:8080", cfg.GetIPFSEndpointWithSecure(false))
	assert.Equal(t, "https://admin.localhost:8080", cfg.GetAdminEndpointWithSecure(true))
	assert.Equal(t, "http://admin.localhost:8080", cfg.GetAdminEndpointWithSecure(false))
}

func TestGetSubdomainEndpointWithProtocol_IP(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		subdomain        string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "127.0.0.1 no subdomain insecure",
			baseEndpoint:     "http://127.0.0.1",
			subdomain:        SubdomainAccount,
			secure:           false,
			expectedEndpoint: "http://127.0.0.1",
		},
		{
			name:             "127.0.0.1 no subdomain secure",
			baseEndpoint:     "http://127.0.0.1",
			subdomain:        SubdomainIPFS,
			secure:           true,
			expectedEndpoint: "https://127.0.0.1",
		},
		{
			name:             "127.0.0.1 with port no subdomain insecure",
			baseEndpoint:     "http://127.0.0.1:8080",
			subdomain:        SubdomainAccount,
			secure:           false,
			expectedEndpoint: "http://127.0.0.1:8080",
		},
		{
			name:             "127.0.0.1 with port no subdomain secure",
			baseEndpoint:     "http://127.0.0.1:8080",
			subdomain:        SubdomainIPFS,
			secure:           true,
			expectedEndpoint: "https://127.0.0.1:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubdomainEndpointWithProtocol(tt.baseEndpoint, tt.subdomain, tt.secure)
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}

func TestConfigGetGatewayEndpoint_LocalhostFallback(t *testing.T) {
	tests := []struct {
		name             string
		baseEndpoint     string
		secure           bool
		expectedEndpoint string
	}{
		{
			name:             "localhost fallback to ipfs subdomain insecure",
			baseEndpoint:     "http://localhost",
			secure:           false,
			expectedEndpoint: "http://ipfs.localhost/ipfs/",
		},
		{
			name:             "localhost:8080 fallback to ipfs subdomain insecure",
			baseEndpoint:     "http://localhost:8080",
			secure:           false,
			expectedEndpoint: "http://ipfs.localhost:8080/ipfs/",
		},
		{
			name:             "localhost:8080 fallback to ipfs subdomain secure",
			baseEndpoint:     "http://localhost:8080",
			secure:           true,
			expectedEndpoint: "https://ipfs.localhost:8080/ipfs/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.baseEndpoint,
				Secure:       tt.secure,
			}
			result := cfg.GetGatewayEndpointSecure()
			assert.Equal(t, tt.expectedEndpoint, result)
		})
	}
}
