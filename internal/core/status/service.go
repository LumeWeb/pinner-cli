// Package status provides the StatusService implementation for checking CID
// status with pin and account-operation fallback, decoupled from any CLI/MCP
// presentation layer. It depends on the core pinning and auth services and is
// Output-free.
package status

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil/filter"
	"go.uber.org/zap"
)

// Option configures a StatusService implementation.
type Option func(*service)

// WithAccountClient sets a pre-configured portal-sdk account client.
func WithAccountClient(client portalsdk.AccountAPI) Option {
	return func(s *service) {
		s.accountClient = client
	}
}

// WithPinningService injects the pinning service used for pin-status checks.
func WithPinningService(ps pinning.PinningService) Option {
	return func(s *service) {
		s.pinningService = ps
	}
}

// WithAuthService injects the auth service used for operation lookup.
func WithAuthService(as auth.AuthService) Option {
	return func(s *service) {
		s.authService = as
	}
}

// service implements the status operation with pin + operation fallback.
type service struct {
	pinningService pinning.PinningService
	accountClient  portalsdk.AccountAPI
	authService    auth.AuthService
	configMgr      config.Manager
	log            *zap.Logger
}

// New creates a StatusService with the given dependencies.
func New(cfgMgr config.Manager, pinningService pinning.PinningService, authService auth.AuthService, logger *zap.Logger, opts ...Option) *service {
	s := &service{
		pinningService: pinningService,
		authService:    authService,
		configMgr:      cfgMgr,
		log:            logger,
	}
	if s.log == nil {
		s.log = zap.NewNop()
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RequireAuthenticated checks that the pinning service is authenticated.
func (s *service) RequireAuthenticated() error {
	return s.pinningService.RequireAuthenticated()
}

// Status checks pin status, falling back to account operations if the pin is
// not found.
func (s *service) Status(ctx context.Context, cid string, watch bool) (*pinning.PinStatus, *pinning.OperationStatusResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, nil, err
	}

	pinStatus, err := s.pinningService.Status(ctx, cid, watch)
	if err == nil {
		return pinStatus, nil, nil
	}

	if !errors.Is(err, coreerrors.ErrPinNotFound) {
		return nil, nil, err
	}

	opResult, opErr := s.lookupOperation(ctx, cid)
	if opErr != nil {
		return nil, nil, opErr
	}

	return nil, opResult, nil
}

func (s *service) resolveAccountClient(ctx context.Context) (portalsdk.AccountAPI, error) {
	if s.accountClient != nil {
		return s.accountClient, nil
	}

	if s.authService == nil {
		return nil, coreerrors.ErrPinNotFound
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate for operation lookup: %w", err)
	}

	s.accountClient = client
	return client, nil
}

func (s *service) lookupOperation(ctx context.Context, cid string) (*pinning.OperationStatusResult, error) {
	client, err := s.resolveAccountClient(ctx)
	if err != nil {
		return nil, err
	}

	s.log.Debug("pin not found, checking account operations for CID", zap.String("cid", cid))

	operations, _, err := client.ListOperations(ctx, portalsdk.WithFilters(filter.FieldEqual("cid", cid)))
	if err != nil {
		return nil, fmt.Errorf("failed to lookup operations: %w", err)
	}

	if len(operations) == 0 {
		return nil, coreerrors.ErrPinNotFound
	}

	op := operations[0]
	result := &pinning.OperationStatusResult{
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
