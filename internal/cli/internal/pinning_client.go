package internal

import (
	"context"

	go_pinning_service_http_client "github.com/ipfs/boxo/pinning/remote/client"
	"github.com/ipfs/go-cid"
)

// PinningClient defines the interface for IPFS pinning service client operations.
type PinningClient interface {
	// Add pins a CID to the pinning service with optional options.
	Add(ctx context.Context, cid cid.Cid, opts ...go_pinning_service_http_client.AddOption) (go_pinning_service_http_client.PinStatusGetter, error)

	// LsSync lists pins synchronously with optional filters.
	LsSync(ctx context.Context, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error)

	// LsWithLimit lists pins but stops once `limit` results have been received,
	// canceling the underlying request. boxo's LsSync drains every page (the
	// Limit option is only a page size), so this is the only way to honor a
	// true total cap without over-fetching.
	LsWithLimit(ctx context.Context, limit int, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error)

	// GetStatusByID retrieves pin status by request ID.
	GetStatusByID(ctx context.Context, pinID string) (go_pinning_service_http_client.PinStatusGetter, error)

	// DeleteByID removes a pin by request ID.
	DeleteByID(ctx context.Context, pinID string) error

	// Replace replaces an existing pin with a new CID and options.
	Replace(ctx context.Context, pinID string, cid cid.Cid, opts ...go_pinning_service_http_client.AddOption) (go_pinning_service_http_client.PinStatusGetter, error)
}

// PinningClientFactory creates a PinningClient with the given endpoint and auth token.
type PinningClientFactory func(endpoint, authToken string) PinningClient
