package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	statuspkg "go.lumeweb.com/pinner-cli/internal/core/status"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// The StatusService interface and its models are re-exported from core pinning
// (see pinning_types.go). The concrete impl lives in internal/core/status;
// pkg/cli keeps only the option wrappers and the constructor used by handlers.

// StatusServiceOption configures the core status service.
type StatusServiceOption = statuspkg.Option

// WithStatusAccountClient sets a pre-configured portal-sdk account client.
func WithStatusAccountClient(client portalsdk.AccountAPI) StatusServiceOption {
	return statuspkg.WithAccountClient(client)
}

// WithStatusPinningService injects the pinning service used for pin-status checks.
func WithStatusPinningService(ps PinningService) StatusServiceOption {
	return statuspkg.WithPinningService(ps)
}

// WithStatusAuthService injects the auth service used for operation lookup.
func WithStatusAuthService(as AuthService) StatusServiceOption {
	return statuspkg.WithAuthService(as)
}

// StatusServiceFactory builds a StatusService with the given dependencies.
type StatusServiceFactory func(cfgMgr config.Manager, output Output, pinningService PinningService, authService AuthService) StatusService

// NewStatusService creates a StatusService with the given dependencies
// (delegates to core). The output param is retained for signature compatibility
// with the factory but is unused by the core impl.
func NewStatusService(cfgMgr config.Manager, output Output, pinningService PinningService, authService AuthService, opts ...StatusServiceOption) StatusService {
	s := statuspkg.New(cfgMgr, pinningService, authService, nil)
	for _, opt := range opts {
		opt(s)
	}
	return s
}
