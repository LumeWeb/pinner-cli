package admin

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// AdminTokenProvider handles token exchange for admin services.
// It detects API-purpose tokens and exchanges them for login-purpose tokens as needed.
type AdminTokenProvider struct {
	cfgMgr        config.Manager
	apiEndpoint   string
	adminEndpoint string
	baseToken     string
	loginToken    string
	mu            sync.RWMutex
}

// NewAdminTokenProvider creates a new AdminTokenProvider.
func NewAdminTokenProvider(cfgMgr config.Manager) *AdminTokenProvider {
	return &AdminTokenProvider{
		cfgMgr:        cfgMgr,
		apiEndpoint:   cfgMgr.Config().GetAPIEndpoint(),
		adminEndpoint: cfgMgr.Config().GetAdminEndpoint(),
	}
}

// GetLoginToken returns a login-purpose JWT token, exchanging an API token if necessary.
// The exchanged token is cached for subsequent calls.
func (p *AdminTokenProvider) GetLoginToken(ctx context.Context) (string, error) {
	// Fast path: check cached token first
	p.mu.RLock()
	baseToken := p.cfgMgr.Config().AuthToken
	if p.loginToken != "" && p.baseToken == baseToken {
		p.mu.RUnlock()
		return p.loginToken, nil
	}
	p.mu.RUnlock()

	// Check if exchange is needed
	purpose, err := auth.GetJWTPurpose(baseToken)
	if err != nil || purpose != "api" {
		// No exchange needed, use base token as-is
		p.mu.Lock()
		p.loginToken = baseToken
		p.baseToken = baseToken
		p.mu.Unlock()
		return p.loginToken, nil
	}

	// Exchange API key for login token
	client := portalsdk.NewClient(portalsdk.WithEndpoint(p.apiEndpoint))
	loginToken, err := client.LoginWithAPIKey(ctx, baseToken)
	if err != nil {
		return "", fmt.Errorf("exchanging API key for admin access: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.loginToken = loginToken
	p.baseToken = baseToken
	return p.loginToken, nil
}

// Invalidate clears the cached login token, forcing re-exchange on next access.
func (p *AdminTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loginToken = ""
	p.baseToken = ""
}
