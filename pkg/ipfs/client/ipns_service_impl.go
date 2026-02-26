package ipfsclient

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
)

// ipnsService wraps the generated HTTP client to implement IPNSService.
type ipnsService struct {
	client *ClientWithResponses
}

// NewIPNSService creates a new IPNSService wrapping the generated client.
func NewIPNSService(client *ClientWithResponses) IPNSService {
	return &ipnsService{client: client}
}

// ListKeys retrieves all IPNS keys for the authenticated user.
func (s *ipnsService) ListKeys(ctx context.Context) ([]IPNSKeyResponse, error) {
	var result []IPNSKeyResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiIpnsKeysWithResponse(ctx)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				if resp.JSON200 == nil {
					return fmt.Errorf("nil response body")
				}
				result = *resp.JSON200
				return nil
			case 400:
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return result, err
}

// CreateKey generates a new IPNS key with the given name.
func (s *ipnsService) CreateKey(ctx context.Context, name string, key *string) (*IPNSKeyResponse, error) {
	var result *IPNSKeyResponse

	request := IPNSKeyRequest{
		Name: name,
		Key:  key,
	}

	err := retry.Do(
		func() error {
			resp, err := s.client.PostApiIpnsKeysWithResponse(ctx, request)
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
				if resp.JSON200 != nil {
					return fmt.Errorf("API error (200): %s", resp.JSON200.Error)
				}
				return fmt.Errorf("API error (200)")
			case 400:
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return result, err
}

// GetKey retrieves a specific IPNS key by its ID.
func (s *ipnsService) GetKey(ctx context.Context, id string) (*IPNSKeyResponse, error) {
	var result *IPNSKeyResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiIpnsKeysIdWithResponse(ctx, id)
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
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return result, err
}

// DeleteKey removes an IPNS key by its ID.
func (s *ipnsService) DeleteKey(ctx context.Context, id string) error {
	err := retry.Do(
		func() error {
			resp, err := s.client.DeleteApiIpnsKeysIdWithResponse(ctx, id)
			if err != nil {
				return err
			}

			switch resp.StatusCode() {
			case 200:
				return nil
			case 400:
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return err
}

// Publish publishes a CID to an IPNS key.
func (s *ipnsService) Publish(ctx context.Context, cid string, keyId int, ttl *string) (*IPNSPublishResponse, error) {
	var result *IPNSPublishResponse

	request := IPNSPublishRequest{
		Cid:   cid,
		KeyId: keyId,
		Ttl:   ttl,
	}

	err := retry.Do(
		func() error {
			resp, err := s.client.PostApiIpnsPublishWithResponse(ctx, request)
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
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return result, err
}

// Resolve resolves an IPNS name to its target CID.
func (s *ipnsService) Resolve(ctx context.Context, name string) (*IPNSResolveResponse, error) {
	var result *IPNSResolveResponse

	err := retry.Do(
		func() error {
			resp, err := s.client.GetApiIpnsResolveNameWithResponse(ctx, name)
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
				if resp.JSON400 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (400): %s", resp.JSON400.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (400)"))
			case 401:
				if resp.JSON401 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (401): %s", resp.JSON401.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (401)"))
			case 403:
				if resp.JSON403 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (403): %s", resp.JSON403.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (403)"))
			case 404:
				if resp.JSON404 != nil {
					return retry.Unrecoverable(fmt.Errorf("API error (404): %s", resp.JSON404.Error))
				}
				return retry.Unrecoverable(fmt.Errorf("API error (404)"))
			case 500:
				if resp.JSON500 != nil {
					return fmt.Errorf("API error (500): %s", resp.JSON500.Error)
				}
				return fmt.Errorf("API error (500)")
			default:
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
			}
		},
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5*time.Second),
		retry.MaxDelay(30*time.Second),
	)

	return result, err
}


