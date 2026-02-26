package ipfsclient

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIPNSServiceWithClient(t *testing.T) {
	tests := []struct {
		name        string
		httpClient  *http.Client
		baseURL     string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with custom HTTP client",
			httpClient: &http.Client{},
			baseURL:    "https://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:       "success with nil HTTP client",
			httpClient: nil,
			baseURL:    "https://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:       "success with http URL",
			httpClient: &http.Client{},
			baseURL:    "http://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:        "error with invalid base URL",
			httpClient:  &http.Client{},
			baseURL:     ":invalid-url",
			wantErr:     true,
			errContains: "parse",
		},
		{
			name:       "success with trailing slash in base URL",
			httpClient: &http.Client{},
			baseURL:    "https://api.pinner.xyz/",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewIPNSServiceWithClient(tt.httpClient, tt.baseURL)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, service)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, service)
			_, ok := service.(IPNSService)
			assert.True(t, ok, "service should implement IPNSService interface")
		})
	}
}

func TestNewWebsitesServiceWithClient(t *testing.T) {
	tests := []struct {
		name        string
		httpClient  *http.Client
		baseURL     string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with custom HTTP client",
			httpClient: &http.Client{},
			baseURL:    "https://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:       "success with nil HTTP client",
			httpClient: nil,
			baseURL:    "https://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:       "success with http URL",
			httpClient: &http.Client{},
			baseURL:    "http://api.pinner.xyz",
			wantErr:    false,
		},
		{
			name:        "error with invalid base URL",
			httpClient:  &http.Client{},
			baseURL:     ":invalid-url",
			wantErr:     true,
			errContains: "parse",
		},
		{
			name:       "success with trailing slash in base URL",
			httpClient: &http.Client{},
			baseURL:    "https://api.pinner.xyz/",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewWebsitesServiceWithClient(tt.httpClient, tt.baseURL)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, service)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, service)
			_, ok := service.(WebsitesService)
			assert.True(t, ok, "service should implement WebsitesService interface")
		})
	}
}

func TestFactoryConsistency(t *testing.T) {
	// Test that both factory functions produce services that are not nil
	httpClient := &http.Client{}
	baseURL := "https://api.pinner.xyz"

	ipnsService, err := NewIPNSServiceWithClient(httpClient, baseURL)
	require.NoError(t, err)
	assert.NotNil(t, ipnsService)
	assert.IsType(t, &ipnsService{}, ipnsService)

	websitesService, err := NewWebsitesServiceWithClient(httpClient, baseURL)
	require.NoError(t, err)
	assert.NotNil(t, websitesService)
	assert.IsType(t, &websitesService{}, websitesService)
}
