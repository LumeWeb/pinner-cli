package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// WebsitesService is re-exported from core for CLI consumers and handler signatures.
type WebsitesService = websites.Service

// newWebsitesAPI builds the website service used by the CLI websites handlers.
// It is a package-level hook so tests can inject a mock service without touching core.
var newWebsitesAPI = func(cfgMgr config.Manager, authToken string, secure bool) (WebsitesService, error) {
	return websites.NewAuthenticated(cfgMgr, authToken, secure)
}
