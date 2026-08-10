package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// IPNSService is re-exported from core for CLI consumers and handler signatures.
type IPNSService = ipns.Service

// newIPNSAPI builds the IPNS service used by the CLI IPNS handlers. It is a
// package-level hook so tests can inject a mock service without touching core.
var newIPNSAPI = func(cfgMgr config.Manager, authToken string, secure bool) (IPNSService, error) {
	return ipns.NewAuthenticated(cfgMgr, authToken, secure)
}
