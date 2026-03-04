package ipfsclient

import (
	"context"
	"fmt"

	"github.com/avast/retry-go/v4"
)

// WebsitesService defines the interface for managing gateway websites.
// Websites allow users to map custom domain names to IPFS content, making
// it accessible via traditional web browsers with friendly URLs.
type WebsitesService interface {
	// List retrieves all websites for the authenticated user.
	// Returns a slice of WebsiteItem containing website details.
	List(ctx context.Context) ([]WebsiteItem, error)

	// Create creates a new website mapping for a custom domain.
	// The domain parameter specifies the custom domain to map.
	// The targetHash parameter specifies the IPFS CID to point to.
	// The targetType parameter specifies the type of target (e.g., "ipfs", "ipns").
	// Returns the created website details.
	Create(ctx context.Context, domain, targetHash, targetType string) (*WebsiteResponse, error)

	// Get retrieves a specific website by its ID.
	// The id parameter is the website identifier.
	// Returns the website details if found.
	Get(ctx context.Context, id string) (*WebsiteResponse, error)

	// Update updates an existing website mapping.
	// The id parameter specifies which website to update.
	// The domain parameter specifies the new custom domain.
	// The targetHash parameter specifies the new IPFS CID.
	// The targetType parameter specifies the new target type.
	// Returns the updated website details.
	Update(ctx context.Context, id, domain, targetHash, targetType string) (*WebsiteResponse, error)

	// Delete removes a website mapping by its ID.
	// This operation is irreversible and will stop serving the domain.
	Delete(ctx context.Context, id string) error

	// Validate validates a website's DNS configuration.
	// This is a critical operation for power users to verify that their
	// custom domain's DNS records are correctly configured to point to the gateway.
	// The id parameter specifies which website to validate.
	// Returns the validation result with domain, validity status, and message.
	Validate(ctx context.Context, id string) (*WebsiteValidateResponse, error)

	// GetSSLStatus retrieves SSL certificate status for a website domain.
	// The domain parameter specifies the custom domain to query.
	// Returns the website response containing SSL status information including
	// certificate status, issuance date, last update time, and any errors.
	GetSSLStatus(ctx context.Context, domain string) (*WebsiteResponse, error)
}

// websitesClientWithResponsesInterface defines the methods needed from ClientWithResponses
type websitesClientWithResponsesInterface interface {
	GetApiWebsitesWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*GetApiWebsitesResponse, error)
	PostApiWebsitesWithResponse(ctx context.Context, body PostApiWebsitesJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiWebsitesResponse, error)
	GetApiWebsitesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*GetApiWebsitesIdResponse, error)
	PutApiWebsitesIdWithResponse(ctx context.Context, id string, body PutApiWebsitesIdJSONRequestBody, reqEditors ...RequestEditorFn) (*PutApiWebsitesIdResponse, error)
	DeleteApiWebsitesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*DeleteApiWebsitesIdResponse, error)
	PostApiWebsitesIdValidateWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*PostApiWebsitesIdValidateResponse, error)
	GetApiWebsitesDomainSslStatusWithResponse(ctx context.Context, domain string, reqEditors ...RequestEditorFn) (*GetApiWebsitesDomainSslStatusResponse, error)
}

// websitesService wraps the generated HTTP client to implement WebsitesService.
type websitesService struct {
	client websitesClientWithResponsesInterface
}

// NewWebsitesService creates a new WebsitesService wrapping the generated client.
func NewWebsitesService(client websitesClientWithResponsesInterface) WebsitesService {
	return &websitesService{client: client}
}

// formatErrorResponse formats an error response from the API.
func formatErrorResponse(statusCode int, errResp *ErrorResponse) error {
	if errResp != nil && errResp.Error != "" {
		return fmt.Errorf("API error (%d): %s", statusCode, errResp.Error)
	}
	return fmt.Errorf("API error (%d)", statusCode)
}

// List retrieves all websites for the authenticated user.
// Note: The API returns WebsiteItemResponse with a single WebsiteItem in the Data field.
// This wraps that single item in a slice to match the interface contract.
func (s *websitesService) List(ctx context.Context) ([]WebsiteItem, error) {
	var result []WebsiteItem

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiWebsitesWithResponse(ctx)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = []WebsiteItem{resp.JSON200.Data}
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}

// GetSSLStatus retrieves SSL certificate status for a website domain.
func (s *websitesService) GetSSLStatus(ctx context.Context, domain string) (*WebsiteResponse, error) {
	var result *WebsiteResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiWebsitesDomainSslStatusWithResponse(ctx, domain)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = resp.JSON200
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}

// Create creates a new website mapping for a custom domain.
func (s *websitesService) Create(ctx context.Context, domain, targetHash, targetType string) (*WebsiteResponse, error) {
	var result *WebsiteResponse

	request := WebsiteRequest{
		Domain:     domain,
		TargetHash: targetHash,
		TargetType: targetType,
	}

	err := retry.Do(
		func() error {
			resp, err := s.client.PostApiWebsitesWithResponse(ctx, request)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 201:
				if resp.JSON201 == nil {
					return fmt.Errorf("nil response body")
				}
				result = resp.JSON201
				return nil
			case 200:
				// Per the API spec, HTTP 200 with ErrorResponse indicates a failure condition
				// (e.g., target is broken). This is documented in the OpenAPI specification.
				return retry.Unrecoverable(formatErrorResponse(200, resp.JSON200))
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}

// Get retrieves a specific website by its ID.
func (s *websitesService) Get(ctx context.Context, id string) (*WebsiteResponse, error) {
	var result *WebsiteResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiWebsitesIdWithResponse(ctx, id)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = resp.JSON200
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}

// Update updates an existing website mapping.
func (s *websitesService) Update(ctx context.Context, id, domain, targetHash, targetType string) (*WebsiteResponse, error) {
	var result *WebsiteResponse

	request := WebsiteRequest{
		Domain:     domain,
		TargetHash: targetHash,
		TargetType: targetType,
	}

	err := retry.Do(
		func() error {
			resp, err := s.client.PutApiWebsitesIdWithResponse(ctx, id, request)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = resp.JSON200
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}

// Delete removes a website mapping by its ID.
func (s *websitesService) Delete(ctx context.Context, id string) error {
	err := retry.Do(
		func() error {
			resp, err := s.client.DeleteApiWebsitesIdWithResponse(ctx, id)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return err
}

// Validate validates a website's DNS configuration.
func (s *websitesService) Validate(ctx context.Context, id string) (*WebsiteValidateResponse, error) {
	var result *WebsiteValidateResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.PostApiWebsitesIdValidateWithResponse(ctx, id)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = resp.JSON200
				return nil
			case 400:
				return retry.Unrecoverable(formatErrorResponse(400, resp.JSON400))
			case 401:
				return retry.Unrecoverable(formatErrorResponse(401, resp.JSON401))
			case 403:
				return retry.Unrecoverable(formatErrorResponse(403, resp.JSON403))
			case 404:
				return retry.Unrecoverable(formatErrorResponse(404, resp.JSON404))
			case 500:
				return formatErrorResponse(500, resp.JSON500)
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retryOptions(ctx)...,
	)

	return result, err
}
