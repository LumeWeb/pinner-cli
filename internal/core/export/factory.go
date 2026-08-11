package export

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// ServiceFactoryFunc creates a Service with dependencies.
type ServiceFactoryFunc func(cfgMgr config.Manager, secure bool, opts ...Option) Service

// ServiceFactory is a package-level factory hook, overridable for tests.
var ServiceFactory ServiceFactoryFunc = DefaultFactory

// DefaultFactory builds an Export Service using the config-derived endpoint.
func DefaultFactory(cfgMgr config.Manager, secure bool, opts ...Option) Service {
	apiEndpoint := cfgMgr.Config().GetMetaEndpointWithSecure(secure)
	return New(cfgMgr, apiEndpoint, nil, opts...)
}

// NewAuthenticated builds an Export Service with the supplied auth token,
// requiring that the resulting service is authenticated.
func NewAuthenticated(cfgMgr config.Manager, authToken string, secure bool, opts ...Option) (Service, error) {
	var svcOpts []Option
	if authToken != "" {
		svcOpts = append(svcOpts, WithAuthToken(authToken))
	}
	svcOpts = append(svcOpts, opts...)
	svc := ServiceFactory(cfgMgr, secure, svcOpts...)
	if err := svc.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return svc, nil
}
