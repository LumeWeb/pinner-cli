package ipfsclient

import (
	"net/http"
)

// NewIPNSServiceWithClient creates a new IPNSService with the provided HTTP client and base URL.
// This factory function creates a ClientWithResponses instance configured with the given
// HTTP client and base URL, then wraps it in an IPNSService.
//
// Parameters:
//   - httpClient: The HTTP client to use for API requests. If nil, a default client will be used.
//   - baseURL: The base URL of the IPFS API server (e.g., "https://api.pinner.xyz").
//
// Returns:
//   - IPNSService: The configured IPNS service instance.
//   - error: An error if the client cannot be created.
func NewIPNSServiceWithClient(httpClient *http.Client, baseURL string) (IPNSService, error) {
	opts := []ClientOption{WithBaseURL(baseURL)}
	if httpClient != nil {
		opts = append(opts, WithHTTPClient(httpClient))
	}

	client, err := NewClientWithResponses("", opts...)
	if err != nil {
		return nil, err
	}

	return NewIPNSService(client), nil
}

// NewWebsitesServiceWithClient creates a new WebsitesService with the provided HTTP client and base URL.
// This factory function creates a ClientWithResponses instance configured with the given
// HTTP client and base URL, then wraps it in a WebsitesService.
//
// Parameters:
//   - httpClient: The HTTP client to use for API requests. If nil, a default client will be used.
//   - baseURL: The base URL of the IPFS API server (e.g., "https://api.pinner.xyz").
//
// Returns:
//   - WebsitesService: The configured websites service instance.
//   - error: An error if the client cannot be created.
func NewWebsitesServiceWithClient(httpClient *http.Client, baseURL string) (WebsitesService, error) {
	opts := []ClientOption{WithBaseURL(baseURL)}
	if httpClient != nil {
		opts = append(opts, WithHTTPClient(httpClient))
	}

	client, err := NewClientWithResponses("", opts...)
	if err != nil {
		return nil, err
	}

	return NewWebsitesService(client), nil
}
