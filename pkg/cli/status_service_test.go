package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func makeOperation(id int, cid string, status string, opName string, protocol string, progress float32, startedAt time.Time, errMsg *string, statusMsg string) *portalsdk.Operation {
	op := &portalsdk.Operation{}
	op.Id = id
	op.Cid = &cid
	op.Operation = opName
	op.OperationDisplayName = opName
	op.Protocol = protocol
	op.ProtocolDisplayName = protocol
	op.Status = status
	op.StatusDisplayName = status
	op.StatusMessage = statusMsg
	op.ProgressPercent = progress
	op.StartedAt = startedAt
	op.UpdatedAt = startedAt
	op.Error = errMsg
	return op
}

func TestStatusServiceDefault_Status(t *testing.T) {
	t.Run("returns pin status when pin is found", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmXxx", false).Return(
			&PinStatus{CID: "QmXxx", Status: "pinned", Created: "2024-01-01"},
			nil,
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, nil)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmXxx", false)

		require.NoError(t, err)
		assert.NotNil(t, pinStatus)
		assert.Nil(t, opStatus)
		assert.Equal(t, "pinned", pinStatus.Status)
	})

	t.Run("falls back to operation when pin not found", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmYyy", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil)

		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmYyy", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
			nil,
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, authSvc)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmYyy", false)

		require.NoError(t, err)
		assert.Nil(t, pinStatus)
		assert.NotNil(t, opStatus)
		assert.Equal(t, "completed", opStatus.Status)
		assert.Equal(t, "operation", opStatus.Source)
		assert.Equal(t, "QmYyy", opStatus.CID)
	})

	t.Run("returns pin not found when no operation exists either", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmMissing", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil)

		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return([]*portalsdk.Operation{}, 0, nil)

		svc := NewStatusService(cfgMgr, output, pinningSvc, authSvc)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmMissing", false)

		require.Error(t, err)
		assert.Nil(t, pinStatus)
		assert.Nil(t, opStatus)
		assert.True(t, errors.Is(err, ErrPinNotFound))
	})

	t.Run("returns pin not found when auth service is nil", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmMissing", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, nil)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmMissing", false)

		require.Error(t, err)
		assert.Nil(t, pinStatus)
		assert.Nil(t, opStatus)
		assert.True(t, errors.Is(err, ErrPinNotFound))
	})

	t.Run("returns error when pin status fails with non-ErrPinNotFound error", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmXxx", false).Return(
			nil, errors.New("network error"),
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, nil)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmXxx", false)

		require.Error(t, err)
		assert.Nil(t, pinStatus)
		assert.Nil(t, opStatus)
		assert.Contains(t, err.Error(), "network error")
	})

	t.Run("populates error field from operation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmFailed", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil)

		errMsg := "upload failed: insufficient quota"
		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(2, "QmFailed", "failed", "Pin", "IPFS", 50, now, &errMsg, "operation failed"),
			},
			0,
			nil,
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, authSvc)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmFailed", false)

		require.NoError(t, err)
		assert.Nil(t, pinStatus)
		assert.NotNil(t, opStatus)
		assert.Equal(t, "failed", opStatus.Status)
		assert.Equal(t, "upload failed: insufficient quota", opStatus.Error)
		assert.Equal(t, "operation failed", opStatus.StatusMessage)
	})

	t.Run("returns error when auth service fails to get authenticated client", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		authSvc := NewMockAuthService(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmXxx", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(nil, errors.New("auth failed"))

		svc := NewStatusService(cfgMgr, output, pinningSvc, authSvc)
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmXxx", false)

		require.Error(t, err)
		assert.Nil(t, pinStatus)
		assert.Nil(t, opStatus)
		assert.Contains(t, err.Error(), "failed to authenticate for operation lookup")
	})

	t.Run("reuses account client on subsequent lookups", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil).Twice()
		pinningSvc.EXPECT().Status(mock.Anything, "QmA", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)
		pinningSvc.EXPECT().Status(mock.Anything, "QmB", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil).Once()

		cidA := "QmA"
		cidB := "QmB"
		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, cidA, "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
			nil,
		).Once()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(2, cidB, "running", "Pin", "IPFS", 50, now, nil, ""),
			},
			0,
			nil,
		).Once()

		svc := NewStatusService(cfgMgr, output, pinningSvc, authSvc)

		pinStatus, opStatus, err := svc.Status(context.Background(), "QmA", false)
		require.NoError(t, err)
		assert.Nil(t, pinStatus)
		assert.Equal(t, "QmA", opStatus.CID)

		pinStatus, opStatus, err = svc.Status(context.Background(), "QmB", false)
		require.NoError(t, err)
		assert.Nil(t, pinStatus)
		assert.Equal(t, "QmB", opStatus.CID)
	})

	t.Run("uses pre-injected account client when available", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		pinningSvc := NewMockPinningService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		pinningSvc.EXPECT().RequireAuthenticated().Return(nil)
		pinningSvc.EXPECT().Status(mock.Anything, "QmYyy", false).Return(
			nil, fmt.Errorf("pin not found: %w", ErrPinNotFound),
		)

		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmYyy", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
			nil,
		)

		svc := NewStatusService(cfgMgr, output, pinningSvc, nil, WithStatusAccountClient(accountClient))
		pinStatus, opStatus, err := svc.Status(context.Background(), "QmYyy", false)

		require.NoError(t, err)
		assert.Nil(t, pinStatus)
		assert.NotNil(t, opStatus)
		assert.Equal(t, "completed", opStatus.Status)
	})
}
