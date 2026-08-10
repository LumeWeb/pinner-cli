// Package errors defines the shared sentinel errors used across the pinner
// core domain layer. Core packages must not import pkg/cli, so the sentinels
// live here where any core or interface package can reference them without a
// layering violation.
package errors

import (
	"errors"
)

var (
	// ErrNotAuthenticated is returned when an operation requires authentication
	// but no auth token is configured.
	ErrNotAuthenticated = errors.New("not authenticated: no auth token configured")

	// ErrServiceUnavailable is returned when a backing service is unavailable
	// (e.g. an IPFS client could not be created or is nil).
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrPinNotFound is returned when a CID has no pin record.
	ErrPinNotFound = errors.New("pin not found")
)
