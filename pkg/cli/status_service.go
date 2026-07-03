package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil/filter"
)

type StatusServiceOption func(*StatusServiceDefault)

func WithStatusAccountClient(client portalsdk.AccountAPI) StatusServiceOption {
	return func(s *StatusServiceDefault) {
		s.accountClient = client
	}
}

func WithStatusPinningService(ps PinningService) StatusServiceOption {
	return func(s *StatusServiceDefault) {
		s.pinningService = ps
	}
}

func WithStatusAuthService(as AuthService) StatusServiceOption {
	return func(s *StatusServiceDefault) {
		s.authService = as
	}
}

type StatusServiceFactory func(cfgMgr config.Manager, output Output, pinningService PinningService, authService AuthService) StatusService

type StatusServiceDefault struct {
	pinningService PinningService
	accountClient  portalsdk.AccountAPI
	authService    AuthService
	configMgr      config.Manager
	output         Output
}

func NewStatusService(cfgMgr config.Manager, output Output, pinningService PinningService, authService AuthService, opts ...StatusServiceOption) StatusService {
	s := &StatusServiceDefault{
		pinningService: pinningService,
		authService:    authService,
		configMgr:      cfgMgr,
		output:         output,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *StatusServiceDefault) RequireAuthenticated() error {
	return s.pinningService.RequireAuthenticated()
}

func (s *StatusServiceDefault) Status(ctx context.Context, cid string, watch bool) (*PinStatus, *OperationStatusResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, nil, err
	}

	pinStatus, err := s.pinningService.Status(ctx, cid, watch)
	if err == nil {
		return pinStatus, nil, nil
	}

	if !errors.Is(err, ErrPinNotFound) {
		return nil, nil, err
	}

	opResult, opErr := s.lookupOperation(ctx, cid)
	if opErr != nil {
		return nil, nil, opErr
	}

	return nil, opResult, nil
}

func (s *StatusServiceDefault) resolveAccountClient(ctx context.Context) (portalsdk.AccountAPI, error) {
	if s.accountClient != nil {
		return s.accountClient, nil
	}

	if s.authService == nil {
		return nil, ErrPinNotFound
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate for operation lookup: %w", err)
	}

	s.accountClient = client
	return client, nil
}

func (s *StatusServiceDefault) lookupOperation(ctx context.Context, cid string) (*OperationStatusResult, error) {
	client, err := s.resolveAccountClient(ctx)
	if err != nil {
		return nil, err
	}

	s.output.PrintVerbosef("Pin not found, checking account operations for CID %s", cid)

	operations, _, err := client.ListOperations(ctx, portalsdk.WithFilters(filter.FieldEqual("cid", cid)))
	if err != nil {
		return nil, fmt.Errorf("failed to lookup operations: %w", err)
	}

	if len(operations) == 0 {
		return nil, ErrPinNotFound
	}

	op := operations[0]
	result := &OperationStatusResult{
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
		Source:               "operation",
	}

	if op.Cid != nil {
		result.CID = *op.Cid
	}

	if op.Error != nil && *op.Error != "" {
		result.Error = *op.Error
	}

	return result, nil
}
