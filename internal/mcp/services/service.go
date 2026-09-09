//go:build !no_tunnel

package services

// This file defines the mcp↔service bridge surface. The lifecycle backend
// lives in go.lumeweb.com/pinner/services (Service interface, System registry, per-platform
// backends, status); this package consumes it via services.New. The install
// command, env-file collection, tunnel wizard, and provider validation stay in
// this package.
//
// services.Service is the lifecycle seam formerly known as ManagedService.
// services.Status is the backend-independent state (Installed/Active/Ready/Summary).
