// Package ipfsbase provides the shared auth/config base used by the
// IPFS-content-network services (DAG, DNS, IPNS, Websites, export). The mutex
// guards each concrete service's service/client fields so the config-watcher
// goroutine (SetAuthToken) and request goroutines are serialized.
package ipfsbase

import (
	"sync"

	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// Base provides the shared auth/config pattern used by the IPFS-content-network
// services. It is embedded (by value) by each concrete service.
type Base struct {
	mu        sync.RWMutex
	cfgMgr    config.Manager
	authToken string
}

// New creates a Base backed by the given config manager, applying any options.
func New(cfgMgr config.Manager, opts ...Option) *Base {
	b := &Base{cfgMgr: cfgMgr}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Option configures a Base at construction time.
type Option func(*Base)

// WithAuthToken sets the initial auth token override.
func WithAuthToken(token string) Option {
	return func(b *Base) { b.authToken = token }
}

// GetAuthToken returns the auth token to use, with override taking precedence
// over config.
func (b *Base) GetAuthToken() string {
	if b.authToken != "" {
		return b.authToken
	}
	return b.cfgMgr.Config().AuthToken
}

// RequireAuthenticated checks whether the user is authenticated.
func (b *Base) RequireAuthenticated() error {
	if b.GetAuthToken() == "" {
		return coreerrors.ErrNotAuthenticated
	}
	return nil
}

// SetAuthTokenOverride sets an auth token override that takes precedence over
// config.
func (b *Base) SetAuthTokenOverride(token string) {
	b.authToken = token
}

// CfgMgr returns the underlying config manager.
func (b *Base) CfgMgr() config.Manager {
	return b.cfgMgr
}

// Lock serializes access to the embedded service/client fields (config-watcher
// goroutine vs request goroutines).
func (b *Base) Lock() {
	b.mu.Lock()
}

// Unlock releases the write lock.
func (b *Base) Unlock() {
	b.mu.Unlock()
}

// RLock acquires the read lock for request goroutines.
func (b *Base) RLock() {
	b.mu.RLock()
}

// RUnlock releases the read lock.
func (b *Base) RUnlock() {
	b.mu.RUnlock()
}
