//go:build !no_tunnel

package services

// This file defines the mcp↔service bridge surface. The lifecycle backend
// lives in internal/service (Service interface, System registry, per-platform
// backends, status); this package consumes it via service.New. The install
// command, env-file collection, tunnel wizard, and provider validation stay in
// this package.
//
// service.Service is the lifecycle seam formerly known as ManagedService.
// service.Status is the backend-independent state (Installed/Active/Ready/Summary).
