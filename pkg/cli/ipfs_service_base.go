package cli

import (
	"sync"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// ipfsServiceBase provides the shared auth/config pattern used by DNS, IPNS, and Websites services.
// The mutex guards the concrete service's s.service / s.client fields so the
// config-watcher goroutine (SetAuthToken) and request goroutines are serialized.
type ipfsServiceBase struct {
	mu        sync.RWMutex
	cfgMgr    config.Manager
	authToken string
}

// getAuthToken returns the auth token to use, with override taking precedence over config.
func (b *ipfsServiceBase) getAuthToken() string {
	if b.authToken != "" {
		return b.authToken
	}
	return b.cfgMgr.Config().AuthToken
}

// RequireAuthenticated checks if the user is authenticated.
func (b *ipfsServiceBase) RequireAuthenticated() error {
	if b.getAuthToken() == "" {
		return ErrNotAuthenticated
	}
	return nil
}

// ipfsServiceOption applies a functional option to ipfsServiceBase.
type ipfsServiceOption func(*ipfsServiceBase)

// withAuthToken returns an ipfsServiceOption that sets the auth token override.
func withAuthToken(token string) ipfsServiceOption {
	return func(b *ipfsServiceBase) { b.authToken = token }
}
