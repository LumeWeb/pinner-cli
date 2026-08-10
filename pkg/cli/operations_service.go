package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/operations"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
)

// Operation models and the OperationsService contract are re-exported from core.
type OperationListItem = operations.OperationListItem
type OperationDetail = operations.OperationDetail
type OperationsListResult = operations.OperationsListResult
type OperationsListOptions = operations.ListOptions
type OperationsService = operations.Service

// OperationsServiceOption mutates the CLI's concrete impl (portalsdk-coupled).
type OperationsServiceOption func(*OperationsServiceDefault)

func WithOperationsAccountClient(client portalsdk.AccountAPI) OperationsServiceOption {
	return func(s *OperationsServiceDefault) {
		s.accountClient = client
	}
}

func WithOperationsAuthService(as AuthService) OperationsServiceOption {
	return func(s *OperationsServiceDefault) {
		s.authService = as
	}
}

// OperationsServiceFactory builds a Service with the CLI Output and auth service.
type OperationsServiceFactory func(cfgMgr config.Manager, output Output, authService AuthService) OperationsService

type OperationsServiceDefault struct {
	accountClient portalsdk.AccountAPI
	authService   AuthService
	configMgr     config.Manager
	output        Output
}

func NewOperationsService(cfgMgr config.Manager, output Output, authService AuthService, opts ...OperationsServiceOption) OperationsService {
	s := &OperationsServiceDefault{
		authService: authService,
		configMgr:   cfgMgr,
		output:      output,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *OperationsServiceDefault) RequireAuthenticated() error {
	if s.accountClient != nil {
		return nil
	}
	if s.authService == nil {
		return ErrNotAuthenticated
	}
	_, err := s.resolveAccountClient(context.Background())
	if err != nil {
		return err
	}
	return nil
}

func (s *OperationsServiceDefault) resolveAccountClient(ctx context.Context) (portalsdk.AccountAPI, error) {
	if s.accountClient != nil {
		return s.accountClient, nil
	}

	if s.authService == nil {
		return nil, ErrNotAuthenticated
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	s.accountClient = client
	return client, nil
}

func (s *OperationsServiceDefault) List(ctx context.Context, opts OperationsListOptions) (*OperationsListResult, error) {
	client, err := s.resolveAccountClient(ctx)
	if err != nil {
		return nil, err
	}

	var listOpts []portalsdk.ListOption

	var filters []queryutil.CrudFilter
	if opts.StatusFilter != "" {
		filters = append(filters, filter.FieldEqual("status", opts.StatusFilter))
	}
	if opts.OperationFilter != "" {
		filters = append(filters, filter.FieldEqual("operation", opts.OperationFilter))
	}
	if opts.ProtocolFilter != "" {
		filters = append(filters, filter.FieldEqual("protocol", opts.ProtocolFilter))
	}
	if opts.CIDFilter != "" {
		filters = append(filters, filter.FieldEqual("cid", opts.CIDFilter))
	}
	if len(filters) > 0 {
		listOpts = append(listOpts, portalsdk.WithFilters(filters...))
	}

	if opts.Sort != "" {
		sorts := parseSortOptions(opts.Sort)
		if len(sorts) > 0 {
			listOpts = append(listOpts, portalsdk.WithSorts(sorts...))
		}
	}

	if opts.PageSize > 0 {
		page := opts.Page
		if page < 1 {
			page = 1
		}
		start := (page - 1) * opts.PageSize
		listOpts = append(listOpts, portalsdk.WithPagination(&queryutil.Pagination{
			Start:    start,
			End:      start + opts.PageSize,
			PageSize: opts.PageSize,
		}))
	}

	operations, total, err := client.ListOperations(ctx, listOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to list operations: %w", err)
	}

	result := &OperationsListResult{
		Operations: make([]OperationListItem, 0, len(operations)),
		Total:      total,
	}

	for _, op := range operations {
		item := OperationListItem{
			ID:                   op.Id,
			Operation:            op.Operation,
			OperationDisplayName: op.OperationDisplayName,
			Protocol:             op.Protocol,
			ProtocolDisplayName:  op.ProtocolDisplayName,
			Status:               op.Status,
			StatusDisplayName:    op.StatusDisplayName,
			StatusMessage:        op.StatusMessage,
			ProgressPercent:      op.ProgressPercent,
			StartedAt:            op.StartedAt.Format(time.DateTime),
			UpdatedAt:            op.UpdatedAt.Format(time.DateTime),
			CurrentStep:          op.CurrentStep,
			TotalSteps:           op.TotalSteps,
		}

		if op.Cid != nil {
			item.CID = *op.Cid
		}
		if op.Error != nil {
			item.Error = *op.Error
		}

		result.Operations = append(result.Operations, item)
	}

	return result, nil
}

func (s *OperationsServiceDefault) Get(ctx context.Context, id int64) (*OperationDetail, error) {
	client, err := s.resolveAccountClient(ctx)
	if err != nil {
		return nil, err
	}

	op, err := client.GetOperation(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get operation %d: %w", id, err)
	}

	detail := &OperationDetail{
		ID:                   op.Id,
		Operation:            op.Operation,
		OperationDisplayName: op.OperationDisplayName,
		Protocol:             op.Protocol,
		ProtocolDisplayName:  op.ProtocolDisplayName,
		Status:               op.Status,
		StatusDisplayName:    op.StatusDisplayName,
		StatusMessage:        op.StatusMessage,
		ProgressPercent:      op.ProgressPercent,
		StartedAt:            op.StartedAt.Format(time.RFC3339),
		UpdatedAt:            op.UpdatedAt.Format(time.RFC3339),
		CurrentStep:          op.CurrentStep,
		TotalSteps:           op.TotalSteps,
	}

	if op.Cid != nil {
		detail.CID = *op.Cid
	}
	if op.Error != nil && *op.Error != "" {
		detail.Error = *op.Error
	}

	return detail, nil
}

var validOperationStatuses = map[portalsdk.OperationStatus]bool{
	portalsdk.OperationStatusPending:   true,
	portalsdk.OperationStatusRunning:   true,
	portalsdk.OperationStatusCompleted: true,
	portalsdk.OperationStatusFailed:    true,
	portalsdk.OperationStatusError:     true,
}

func validateOperationStatus(status string) error {
	if status == "" {
		return nil
	}
	if !validOperationStatuses[portalsdk.OperationStatus(status)] {
		return fmt.Errorf("invalid status %q: must be one of pending, running, completed, failed, error", status)
	}
	return nil
}

func parseSortOptions(sortStr string) []queryutil.Sort {
	var sorts []queryutil.Sort
	for _, part := range strings.Split(sortStr, ",") {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		order := queryutil.OrderDesc

		if idx := strings.Index(part, ":"); idx > 0 {
			field = strings.TrimSpace(part[:idx])
			orderStr := strings.ToLower(strings.TrimSpace(part[idx+1:]))
			if orderStr == "asc" {
				order = queryutil.OrderAsc
			} else if orderStr == "desc" {
				order = queryutil.OrderDesc
			}
		}

		sorts = append(sorts, queryutil.Sort{Field: field, Order: order})
	}
	return sorts
}

func (s *OperationsServiceDefault) Watch(ctx context.Context, id int64) (*OperationDetail, error) {
	client, err := s.resolveAccountClient(ctx)
	if err != nil {
		return nil, err
	}

	op, err := client.WaitForOperation(ctx, id,
		portalsdk.WithPollInterval(2*time.Second),
		portalsdk.WithPollTimeout(5*time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for operation %d: %w", id, err)
	}

	detail := &OperationDetail{
		ID:                   op.Id,
		Operation:            op.Operation,
		OperationDisplayName: op.OperationDisplayName,
		Protocol:             op.Protocol,
		ProtocolDisplayName:  op.ProtocolDisplayName,
		Status:               op.Status,
		StatusDisplayName:    op.StatusDisplayName,
		StatusMessage:        op.StatusMessage,
		ProgressPercent:      op.ProgressPercent,
		StartedAt:            op.StartedAt.Format(time.RFC3339),
		UpdatedAt:            op.UpdatedAt.Format(time.RFC3339),
		CurrentStep:          op.CurrentStep,
		TotalSteps:           op.TotalSteps,
	}

	if op.Cid != nil {
		detail.CID = *op.Cid
	}
	if op.Error != nil && *op.Error != "" {
		detail.Error = *op.Error
	}

	return detail, nil
}
