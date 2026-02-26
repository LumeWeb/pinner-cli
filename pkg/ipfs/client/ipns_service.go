package ipfsclient

import "context"

// IPNSService defines the interface for IPNS (InterPlanetary Name System) operations.
// IPNS provides a mutable address scheme for content on IPFS, allowing users to
// publish content under a stable name that can be updated to point to new CIDs.
type IPNSService interface {
	// ListKeys retrieves all IPNS keys for the authenticated user.
	// Returns a slice of IPNSKeyResponse containing key details.
	ListKeys(ctx context.Context) ([]IPNSKeyResponse, error)

	// CreateKey generates a new IPNS key with the given name.
	// The key parameter can optionally specify an existing private key to import.
	// Returns the created key details.
	CreateKey(ctx context.Context, name string, key *string) (*IPNSKeyResponse, error)

	// GetKey retrieves a specific IPNS key by its ID.
	// Returns the key details if found.
	GetKey(ctx context.Context, id string) (*IPNSKeyResponse, error)

	// DeleteKey removes an IPNS key by its ID.
	// This operation is irreversible.
	DeleteKey(ctx context.Context, id string) error

	// Publish publishes a CID to an IPNS key, making it resolvable via the IPNS name.
	// The keyId parameter specifies which key to publish to.
	// The ttl parameter is an optional time-to-live for the record.
	// Returns the publish result with the IPNS name and sequence number.
	Publish(ctx context.Context, cid string, keyId int, ttl *string) (*IPNSPublishResponse, error)

	// Resolve resolves an IPNS name to its target CID.
	// The name parameter is the IPNS name (e.g., k51qzi5uqu5djx...).
	// Returns the resolve result with the target CID and metadata.
	Resolve(ctx context.Context, name string) (*IPNSResolveResponse, error)
}
