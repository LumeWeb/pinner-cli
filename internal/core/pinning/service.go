// Package pinning provides the pinning domain for pinner-cli.
//
// It is deliberately free of CLI presentation coupling: the PinningService and
// StatusService interfaces and their data models live here, and the
// PinningServiceFactory constructor takes no Output formatter. Callers — the
// CLI command handlers, the MCP adapter, or any future consumer — depend on the
// interfaces and are responsible for rendering the returned results.
package pinning

import (
	"context"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// Pin represents a pinned item
type Pin struct {
	CID       string            `json:"cid"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Created   string            `json:"created"`
	RequestID string            `json:"request_id"`
	Metadata  map[string]string `json:"metadata"`
}

// PinStatus represents the status of a pin
type PinStatus struct {
	CID       string   `json:"cid"`
	Status    string   `json:"status"`
	Delegates []string `json:"delegates"`
	Created   string   `json:"created"`
}

// OperationStatusResult represents the status of an account operation.
type OperationStatusResult struct {
	ID                   int
	Operation            string
	OperationDisplayName string
	Protocol             string
	ProtocolDisplayName  string
	Status               string
	StatusDisplayName    string
	StatusMessage        string
	CID                  string
	ProgressPercent      float32
	StartedAt            string
	UpdatedAt            string
	Error                string
	Source               string
}

// PinResult represents the result of a pin operation
type PinResult struct {
	CID       string `json:"cid"`
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// UnpinResult represents the result of an unpin operation
type UnpinResult struct {
	CID string `json:"cid"`
}

// BatchOptions configures batch operations
type BatchOptions struct {
	Parallel   int
	ContinueOn bool
	Wait       bool
	Progress   bool
}

// BatchResult represents the result of a batch operation
type BatchResult struct {
	Total     int
	Succeeded []OperationResult
	Failed    []OperationError
	Skipped   []string
	Duration  time.Duration
}

// OperationResult represents a successful operation in a batch
type OperationResult struct {
	CID       string
	RequestID string
	Status    string
}

// OperationError represents a failed operation in a batch
type OperationError struct {
	CID   string
	Error string
}

// PinningService defines the interface for pinning operations on existing IPFS content.
type PinningService interface {
	// Pin existing content by CID
	Pin(ctx context.Context, cid, name string, wait bool) (*PinResult, error)

	// Pin multiple CIDs in batch
	PinBatch(ctx context.Context, cids []string, name string, opts BatchOptions) (*BatchResult, error)

	// List pinned content with optional filters
	List(ctx context.Context, nameFilter string, limit int, statusFilter string) ([]Pin, error)

	// Get status of a pin
	Status(ctx context.Context, cid string, watch bool) (*PinStatus, error)

	// Remove a pin
	Unpin(ctx context.Context, cid string, confirm bool) (*UnpinResult, error)

	// Unpin multiple CIDs in batch
	UnpinBatch(ctx context.Context, cids []string, opts BatchOptions) (*BatchResult, error)

	// UnpinAll unpins all pins, optionally filtered by status
	UnpinAll(ctx context.Context, statusFilter string, opts BatchOptions) (*BatchResult, error)

	// Update metadata for a pin
	UpdateMetadata(ctx context.Context, cid string, set []string, clear bool) error

	// UpdatePin updates name and/or metadata for a pin.
	// name: new name (empty = no change)
	// meta: metadata key-value pairs to set (alternating key, value)
	// clearMeta: if true, clears all existing metadata before applying meta
	UpdatePin(ctx context.Context, cid string, name string, meta []string, clearMeta bool) error

	// RequireAuthenticated checks if the service is authenticated.
	RequireAuthenticated() error
}

// StatusService defines the interface for checking CID status with pin and operation fallback.
type StatusService interface {
	// Status checks pin status, falling back to account operations if pin not found
	Status(ctx context.Context, cid string, watch bool) (*PinStatus, *OperationStatusResult, error)

	// RequireAuthenticated checks if the service is authenticated.
	RequireAuthenticated() error
}

// PinningServiceFactory creates a PinningService with dependencies.
// It is free of CLI presentation coupling (no Output formatter).
type PinningServiceFactory func(cfgMgr config.Manager, secure bool) PinningService
