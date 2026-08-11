package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/apikeys"
)

// APIKeyService is re-exported from core for CLI consumers and handler signatures.
type APIKeyService = apikeys.Service

// APIKeyServiceFactory creates an API key service with dependencies.
type APIKeyServiceFactory = apikeys.ServiceFactoryFunc

// NewAPIKeyService creates a new API key service instance (delegates to core).
func NewAPIKeyService(authService AuthService, authToken string) APIKeyService {
	return apikeys.New(authService, authToken)
}

// defaultAPIKeyServiceFactory is the factory used by the API key handlers.
func defaultAPIKeyServiceFactory(authService AuthService, authToken string) APIKeyService {
	return apikeys.New(authService, authToken)
}

// isUUIDString reports whether s looks like a UUID (36 chars with dashes).
func isUUIDString(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
