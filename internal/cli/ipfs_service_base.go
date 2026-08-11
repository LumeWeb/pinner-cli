package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
)

// ipfsServiceBase is the CLI-local alias for the shared IPFS-content-network
// base, now owned by internal/core/ipfsbase. It is embedded by the services
// that remain in pkg/cli (dag, export).
type ipfsServiceBase = ipfsbase.Base

// ipfsServiceOption applies a functional option to the shared base.
type ipfsServiceOption func(*ipfsServiceBase)

// withAuthToken returns an ipfsServiceOption that sets the auth token override.
func withAuthToken(token string) ipfsServiceOption {
	return func(b *ipfsServiceBase) { b.SetAuthTokenOverride(token) }
}
