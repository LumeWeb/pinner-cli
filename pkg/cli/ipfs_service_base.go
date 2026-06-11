package cli

import (
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// ipfsServiceBase provides the shared auth/config pattern used by DNS, IPNS, and Websites services.
type ipfsServiceBase struct {
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
