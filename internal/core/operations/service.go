// Package operations defines the OperationsService contract for inspecting
// account operations on the Pinner content-network, decoupled from any CLI/MCP
// presentation layer. It carries the service interface and result/option
// models; the concrete implementation (whose factory wires the CLI Output and
// portal-sdk account client) lives in pkg/cli.
package operations

import (
	"context"
)

// OperationListItem is a single row in an operations listing.
type OperationListItem struct {
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
	CurrentStep          *int    `json:"current_step"`
	TotalSteps           *int    `json:"total_steps"`
}

// OperationDetail is the full detail of a single operation.
type OperationDetail struct {
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
	CurrentStep          *int    `json:"current_step"`
	TotalSteps           *int    `json:"total_steps"`
}

// OperationsListResult is a paginated operations listing.
type OperationsListResult struct {
	Operations []OperationListItem `json:"operations"`
	Total      int                 `json:"total"`
}

// ListOptions filters and paginates an operations listing.
type ListOptions struct {
	// Search is a full-text search term evaluated server-side against the
	// operation's searchable fields. Empty disables it.
	Search          string
	// StatusFilters limits results to the given statuses (e.g. "pending",
	// "processing"). When nil and IncludeAll is false, the service defaults
	// to showing only active operations (pending, processing).
	StatusFilters   []string
	OperationFilter string
	ProtocolFilter  string
	CIDFilter       string
	Sort            string
	Page            int
	PageSize        int
	// IncludeAll disables the default active-status filter so the listing
	// returns operations in any status. Has no effect when StatusFilters
	// is explicitly provided.
	IncludeAll bool
	// IsWatch is set by the watch code path to skip the default active-status
	// filter. Without this, --watch only sees pending/processing operations,
	// so allOperationsSettled() can never observe the terminal transition and
	// the loop hangs until the active list drains.
	IsWatch bool
}

// Service is the contract for inspecting account operations.
type Service interface {
	List(ctx context.Context, opts ListOptions) (*OperationsListResult, error)
	Get(ctx context.Context, id int64) (*OperationDetail, error)
	Watch(ctx context.Context, id int64) (*OperationDetail, error)
	RequireAuthenticated() error
}
