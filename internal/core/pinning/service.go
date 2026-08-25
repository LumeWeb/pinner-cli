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
	ID                   int     `json:"id"`
	Operation            string  `json:"operation"`
	OperationDisplayName string  `json:"operation_display_name"`
	Protocol             string  `json:"protocol"`
	ProtocolDisplayName  string  `json:"protocol_display_name"`
	Status               string  `json:"status"`
	StatusDisplayName    string  `json:"status_display_name"`
	StatusMessage        string  `json:"status_message"`
	CID                  string  `json:"cid"`
	ProgressPercent      float32 `json:"progress_percent"`
	StartedAt            string  `json:"started_at"`
	UpdatedAt            string  `json:"updated_at"`
	Error                string  `json:"error"`
	Source               string  `json:"source"`
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

	// List pinned content with the shared list options. search (when non-empty)
	// is a server-side name substring match (match=partial) composed with the
	// other filters; Name is an exact name match.
	List(ctx context.Context, opts ListOptions) ([]Pin, error)

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

// ListOptions filters and paginates a pin listing. Start/Limit follow the
// shared list protocol; the ipfs pinning-service spec has no server-side
// offset, so Start is applied client-side to the fetched page when Limit is
// set (see the CLI implementation).
type ListOptions struct {
	Start  int
	Limit  int
	Name   string // exact name match
	Status string // status filter (e.g. pinned, unpinned, failed)
	Search string // server-side substring name match
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
