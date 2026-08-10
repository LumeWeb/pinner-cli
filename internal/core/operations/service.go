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
	CurrentStep          *int
	TotalSteps           *int
}

// OperationDetail is the full detail of a single operation.
type OperationDetail struct {
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
	CurrentStep          *int
	TotalSteps           *int
}

// OperationsListResult is a paginated operations listing.
type OperationsListResult struct {
	Operations []OperationListItem
	Total      int
}

// ListOptions filters and paginates an operations listing.
type ListOptions struct {
	StatusFilter    string
	OperationFilter string
	ProtocolFilter  string
	CIDFilter       string
	Sort            string
	Page            int
	PageSize        int
}

// Service is the contract for inspecting account operations.
type Service interface {
	List(ctx context.Context, opts ListOptions) (*OperationsListResult, error)
	Get(ctx context.Context, id int64) (*OperationDetail, error)
	Watch(ctx context.Context, id int64) (*OperationDetail, error)
	RequireAuthenticated() error
}
