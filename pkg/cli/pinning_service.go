package cli

import (
	"context"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/config"
)

// Pin represents a pinned item
type Pin struct {
	CID      string
	Name     string
	Status   string
	Created  string
	Metadata map[string]string
}

// PinStatus represents the status of a pin
type PinStatus struct {
	CID       string
	Status    string
	Delegates []string
	Created   string
}

// PinResult represents the result of a pin operation
type PinResult struct {
	CID       string
	RequestID string
	Status    string
}

// UnpinResult represents the result of an unpin operation
type UnpinResult struct {
	CID string
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

	// RequireAuthenticated checks if the service is authenticated.
	RequireAuthenticated() error
}

// PinningServiceFactory creates a PinningService with dependencies
type PinningServiceFactory func(cfgMgr config.Manager, output Output) PinningService
