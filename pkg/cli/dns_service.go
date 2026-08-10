package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/dns"
)

// DNSService is re-exported from core for CLI consumers and handler signatures.
type DNSService = dns.Service

// newDNSAPI builds the DNS service used by the CLI DNS handlers. It is a
// package-level hook so tests can inject a mock service without touching core.
var newDNSAPI = func(cfgMgr config.Manager, authToken string, secure bool) (DNSService, error) {
	return dns.NewAuthenticated(cfgMgr, authToken, secure)
}
