package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
)

// The pinning domain's interfaces, models, and factory live in
// internal/core/pinning. pkg/cli re-exports them as aliases so the CLI impl
// (which still lives in pkg/cli) and command handlers can keep referencing the
// unqualified names without an import cycle or a large rename.

// Pin represents a pinned item.
type Pin = pinning.Pin

// PinStatus represents the status of a pin.
type PinStatus = pinning.PinStatus

// OperationStatusResult represents the status of an account operation.
type OperationStatusResult = pinning.OperationStatusResult

// PinResult represents the result of a pin operation.
type PinResult = pinning.PinResult

// UnpinResult represents the result of an unpin operation.
type UnpinResult = pinning.UnpinResult

// BatchOptions configures batch operations.
type BatchOptions = pinning.BatchOptions

// BatchResult represents the result of a batch operation.
type BatchResult = pinning.BatchResult

// OperationResult represents a successful operation in a batch.
type OperationResult = pinning.OperationResult

// OperationError represents a failed operation in a batch.
type OperationError = pinning.OperationError

// PinningService is the pinning domain interface (from internal/core/pinning).
type PinningService = pinning.PinningService

// StatusService is the status domain interface (from internal/core/pinning).
type StatusService = pinning.StatusService

// PinningServiceFactory creates a PinningService with dependencies.
// It is free of CLI presentation coupling (no Output formatter).
type PinningServiceFactory = pinning.PinningServiceFactory
