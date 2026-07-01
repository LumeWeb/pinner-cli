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
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
	"go.lumeweb.com/queryutil"
)

func TestOperationsServiceDefault_List(t *testing.T) {
	t.Run("returns operations from account client", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		cid := "QmTest"
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, cid, "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
		assert.Equal(t, 1, result.Operations[0].ID)
		assert.Equal(t, cid, result.Operations[0].CID)
		assert.Equal(t, "completed", result.Operations[0].Status)
	})

	t.Run("returns multiple operations", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		cid1 := "QmTest1"
		cid2 := "QmTest2"
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, cid1, "completed", "Pin", "IPFS", 100, now, nil, ""),
				makeOperation(2, cid2, "running", "Upload", "IPFS", 50, now, nil, "processing"),
			},
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 2)
		assert.Equal(t, "running", result.Operations[1].Status)
		assert.Equal(t, "processing", result.Operations[1].StatusMessage)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		svc := NewOperationsService(cfgMgr, output, nil)

		_, err := svc.List(context.Background(), OperationsListOptions{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when list operations fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(nil, errors.New("server error"))

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		_, err := svc.List(context.Background(), OperationsListOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list operations")
	})

	t.Run("populates error field from operation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		errMsg := "insufficient quota"
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(3, "QmFailed", "failed", "Upload", "IPFS", 50, now, &errMsg, "upload failed"),
			},
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
		assert.Equal(t, "insufficient quota", result.Operations[0].Error)
		assert.Equal(t, "upload failed", result.Operations[0].StatusMessage)
	})

	t.Run("resolves account client via auth service", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil)

		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmAuth", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, authSvc)

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
	})

	t.Run("reuses account client on subsequent calls", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(accountClient, nil).Once()

		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmA", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			nil,
		).Once()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(2, "QmB", "running", "Upload", "IPFS", 50, now, nil, ""),
			},
			nil,
		).Once()

		svc := NewOperationsService(cfgMgr, output, authSvc)

		result1, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result1.Operations, 1)

		result2, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result2.Operations, 1)
	})
}

func TestOperationsServiceDefault_Get(t *testing.T) {
	t.Run("returns operation detail from account client", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		cid := "QmDetail"
		accountClient.EXPECT().GetOperation(mock.Anything, int64(42)).Return(
			makeOperation(42, cid, "running", "Upload", "IPFS", 75, now, nil, "processing"),
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		detail, err := svc.Get(context.Background(), 42)
		require.NoError(t, err)
		assert.Equal(t, 42, detail.ID)
		assert.Equal(t, cid, detail.CID)
		assert.Equal(t, "running", detail.Status)
		assert.Equal(t, float32(75), detail.ProgressPercent)
		assert.Equal(t, "processing", detail.StatusMessage)
	})

	t.Run("returns operation with error detail", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		errMsg := "disk full"
		accountClient.EXPECT().GetOperation(mock.Anything, int64(99)).Return(
			makeOperation(99, "QmErr", "failed", "Upload", "IPFS", 30, now, &errMsg, "upload failed"),
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		detail, err := svc.Get(context.Background(), 99)
		require.NoError(t, err)
		assert.Equal(t, "disk full", detail.Error)
	})

	t.Run("returns error when get operation fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		accountClient.EXPECT().GetOperation(mock.Anything, int64(404)).Return(
			nil, errors.New("not found"),
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		_, err := svc.Get(context.Background(), 404)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get operation 404")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		svc := NewOperationsService(cfgMgr, output, nil)

		_, err := svc.Get(context.Background(), 1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestOperationsServiceDefault_RequireAuthenticated(t *testing.T) {
	t.Run("returns error when auth service is nil", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		svc := NewOperationsService(cfgMgr, output, nil)

		err := svc.RequireAuthenticated()
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when auth client resolution fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		authSvc := NewMockAuthService(t)

		authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(nil, errors.New("auth failed"))

		svc := NewOperationsService(cfgMgr, output, authSvc)

		err := svc.RequireAuthenticated()
		require.Error(t, err)
	})

	t.Run("returns nil when authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}

func TestOperationsServiceDefault_Watch(t *testing.T) {
	t.Run("returns settled operation from WaitForOperation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		cid := "QmWatch"
		accountClient.EXPECT().WaitForOperation(
			mock.Anything,
			int64(7),
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(
			makeOperation(7, cid, "completed", "Pin", "IPFS", 100, now, nil, ""),
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		detail, err := svc.Watch(context.Background(), 7)
		require.NoError(t, err)
		assert.Equal(t, 7, detail.ID)
		assert.Equal(t, "completed", detail.Status)
	})

	t.Run("returns error when WaitForOperation fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		accountClient.EXPECT().WaitForOperation(
			mock.Anything,
			int64(999),
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(nil, portalsdk.ErrOperationTimeout)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		_, err := svc.Watch(context.Background(), 999)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for operation 999")
	})
}

func TestFormatOperationStatusWithColor(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"completed is green", "completed"},
		{"pending is yellow", "pending"},
		{"running is yellow", "running"},
		{"failed is red", "failed"},
		{"error is red", "error"},
		{"unknown passes through", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOperationStatusWithColor(tt.status)
			assert.NotEmpty(t, result)
		})
	}
}

func TestOperationsListOptions_Filters(t *testing.T) {
	t.Run("applies status filter", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmRunning", "running", "Upload", "IPFS", 50, now, nil, ""),
			},
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{StatusFilter: "running"})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
		assert.Equal(t, "running", result.Operations[0].Status)
	})
}

func TestRenderOperationDetail(t *testing.T) {
	t.Run("renders operation with steps", func(t *testing.T) {
		output := newTestOutput()
		currentStep := 2
		totalSteps := 5
		op := &OperationDetail{
			ID:                   1,
			CID:                  "QmTest",
			Status:               "running",
			StatusDisplayName:    "Running",
			Operation:            "upload",
			OperationDisplayName: "Upload",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      40,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:01:00Z",
			CurrentStep:          &currentStep,
			TotalSteps:           &totalSteps,
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})

	t.Run("renders operation with error and message", func(t *testing.T) {
		output := newTestOutput()
		op := &OperationDetail{
			ID:                   2,
			CID:                  "QmErr",
			Status:               "failed",
			StatusDisplayName:    "Failed",
			Operation:            "upload",
			OperationDisplayName: "Upload",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      50,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:01:00Z",
			StatusMessage:        "upload failed",
			Error:                "insufficient quota",
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})

	t.Run("renders minimal operation", func(t *testing.T) {
		output := newTestOutput()
		op := &OperationDetail{
			ID:                   3,
			CID:                  "",
			Status:               "pending",
			StatusDisplayName:    "Pending",
			Operation:            "pin",
			OperationDisplayName: "Pin",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      0,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:00:00Z",
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})
}

func TestOperationsServiceDefault_CIDPointer(t *testing.T) {
	t.Run("handles nil CID pointer from operation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		op := makeOperation(1, "QmTest", "completed", "Pin", "IPFS", 100, now, nil, "")
		op.Cid = nil

		accountClient.EXPECT().GetOperation(mock.Anything, int64(1)).Return(op, nil)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		detail, err := svc.Get(context.Background(), 1)
		require.NoError(t, err)
		assert.Equal(t, "", detail.CID)
	})
}

func TestNewOperationsCommand(t *testing.T) {
	t.Run("creates operations command with correct subcommands", func(t *testing.T) {
		cmd := newOperationsCommand()

		assert.Equal(t, "operations", cmd.Name)
		require.Len(t, cmd.Commands, 2)
		assert.Equal(t, "list", cmd.Commands[0].Name)
		assert.Equal(t, "get", cmd.Commands[1].Name)
	})
}

func TestNewOperationsListCommand(t *testing.T) {
	t.Run("creates list command with correct flags", func(t *testing.T) {
		cmd := newOperationsListCommand()

		assert.Equal(t, "list", cmd.Name)

		flagNames := make(map[string]bool)
		for _, f := range cmd.Flags {
			switch v := f.(type) {
			case *cli.StringFlag:
				flagNames[v.Name] = true
			case *cli.IntFlag:
				flagNames[v.Name] = true
			case *cli.BoolFlag:
				flagNames[v.Name] = true
			}
		}

		assert.True(t, flagNames[FlagStatus])
		assert.True(t, flagNames[FlagOperation])
		assert.True(t, flagNames[FlagProtocol])
		assert.True(t, flagNames[FlagCID])
		assert.True(t, flagNames[FlagSort])
		assert.True(t, flagNames[FlagLimit])
		assert.True(t, flagNames[FlagWatch])
	})
}

func TestNewOperationsGetCommand(t *testing.T) {
	t.Run("creates get command with correct flags", func(t *testing.T) {
		cmd := newOperationsGetCommand()

		assert.Equal(t, "get", cmd.Name)
		assert.Equal(t, "<operation-id>", cmd.ArgsUsage)
	})
}

func TestOperationsList(t *testing.T) {
	t.Run("successful list with results", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
			StatusFilter:    "",
			OperationFilter: "",
			ProtocolFilter:  "",
			CIDFilter:       "",
			Limit:           0,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{
				{ID: 1, CID: "QmTest", Status: "completed", Operation: "pin", OperationDisplayName: "Pin", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 100, StartedAt: "2024-01-01"},
			},
			Total: 1,
		}, nil)

		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("successful list with empty results", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).Return(nil, errors.New("server error"))

		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server error")
	})

	t.Run("passes filters to service", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
			StatusFilter:    "running",
			OperationFilter: "upload",
			ProtocolFilter:  "ipfs",
			CIDFilter:       "QmTest",
			Limit:           5,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := newMockCommand().
			withString(FlagStatus, "running").
			withString(FlagOperation, "upload").
			withString(FlagProtocol, "ipfs").
			withString(FlagCID, "QmTest").
			withInt(FlagLimit, 5)

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when cfgMgr factory fails", func(t *testing.T) {
		output := newTestOutput()
		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return nil, errors.New("config error") }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return nil }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config error")
	})
}

func TestOperationsGet(t *testing.T) {
	t.Run("successful get operation", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().Get(mock.Anything, int64(42)).Return(&OperationDetail{
			ID:                   42,
			CID:                  "QmTest",
			Status:               "completed",
			StatusDisplayName:    "Completed",
			Operation:            "pin",
			OperationDisplayName: "Pin",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      100,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:00:00Z",
		}, nil)

		cmd := newMockCommand().withArgs("42")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when no operation ID provided", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand()

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrOperationNotFound))
	})

	t.Run("returns error for invalid operation ID", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().withArgs("not-a-number")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid operation ID")
	})

	t.Run("returns error when get fails", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().Get(mock.Anything, int64(999)).Return(nil, fmt.Errorf("not found"))

		cmd := newMockCommand().withArgs("999")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := newMockCommand().withArgs("1")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when cfgMgr factory fails", func(t *testing.T) {
		output := newTestOutput()
		cmd := newMockCommand().withArgs("1")

		cfgMgrFactory := func() (config.Manager, error) { return nil, errors.New("config error") }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return nil }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config error")
	})
}

func TestAllOperationsSettled(t *testing.T) {
	t.Run("returns true when all operations are settled", func(t *testing.T) {
		result := &OperationsListResult{
			Operations: []OperationListItem{
				{Status: "completed"},
				{Status: "failed"},
			},
		}
		assert.True(t, allOperationsSettled(result))
	})

	t.Run("returns false when some operations are not settled", func(t *testing.T) {
		result := &OperationsListResult{
			Operations: []OperationListItem{
				{Status: "completed"},
				{Status: "running"},
			},
		}
		assert.False(t, allOperationsSettled(result))
	})

	t.Run("returns true for empty results", func(t *testing.T) {
		result := &OperationsListResult{
			Operations: []OperationListItem{},
		}
		assert.True(t, allOperationsSettled(result))
	})

	t.Run("returns false for pending operation", func(t *testing.T) {
		result := &OperationsListResult{
			Operations: []OperationListItem{
				{Status: "pending"},
				{Status: "completed"},
			},
		}
		assert.False(t, allOperationsSettled(result))
	})
}

func TestBuildOperationRows(t *testing.T) {
	t.Run("builds rows from operations", func(t *testing.T) {
		result := &OperationsListResult{
			Operations: []OperationListItem{
				{ID: 1, OperationDisplayName: "Pin", ProtocolDisplayName: "IPFS", Status: "completed", CID: "QmTest", ProgressPercent: 100, StartedAt: "2024-01-01"},
			},
		}
		rows := buildOperationRows(result)
		require.Len(t, rows, 1)
		assert.Equal(t, "1", rows[0][0])
		assert.Equal(t, "Pin", rows[0][1])
		assert.Equal(t, "IPFS", rows[0][2])
	})
}

func TestWatchOperationsList(t *testing.T) {
	t.Run("exits cleanly when context cancelled after initial list", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).Return(&OperationsListResult{
			Operations: []OperationListItem{
				{ID: 1, CID: "QmTest", Status: "running", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
			},
			Total: 1,
		}, nil)

		ctx, cancel := context.WithCancel(context.Background())
		// Cancel context shortly after the initial list call completes,
		// so the ticker loop picks up ctx.Done() before the 2s ticker fires.
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		err := watchOperationsList(ctx, opsSvc, output, OperationsListOptions{})
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("returns error when initial list fails", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).Return(nil, errors.New("server error"))

		err := watchOperationsList(context.Background(), opsSvc, output, OperationsListOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server error")
	})

	t.Run("exits when all operations are settled on initial list", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).Return(&OperationsListResult{
			Operations: []OperationListItem{
				{ID: 1, CID: "QmTest", Status: "completed", Operation: "pin", OperationDisplayName: "Pin", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 100, StartedAt: "2024-01-01"},
			},
			Total: 1,
		}, nil)

		err := watchOperationsList(context.Background(), opsSvc, output, OperationsListOptions{})
		require.NoError(t, err)
	})

	t.Run("returns error when list fails during ticker loop", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		callCount := 0
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).RunAndReturn(
			func(ctx context.Context, opts OperationsListOptions) (*OperationsListResult, error) {
				callCount++
				if callCount == 1 {
					return &OperationsListResult{
						Operations: []OperationListItem{
							{ID: 1, CID: "QmTest", Status: "running", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
						},
						Total: 1,
					}, nil
				}
				return nil, errors.New("network error")
			},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperationsList(ctx, opsSvc, output, OperationsListOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
	})

	t.Run("exits when ticker loop finds empty operations", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		callCount := 0
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).RunAndReturn(
			func(ctx context.Context, opts OperationsListOptions) (*OperationsListResult, error) {
				callCount++
				if callCount == 1 {
					return &OperationsListResult{
						Operations: []OperationListItem{
							{ID: 1, CID: "QmTest", Status: "running", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
						},
						Total: 1,
					}, nil
				}
				return &OperationsListResult{
					Operations: []OperationListItem{},
					Total:      0,
				}, nil
			},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperationsList(ctx, opsSvc, output, OperationsListOptions{})
		require.NoError(t, err)
	})

	t.Run("exits when ticker loop finds all operations settled", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		callCount := 0
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{}).RunAndReturn(
			func(ctx context.Context, opts OperationsListOptions) (*OperationsListResult, error) {
				callCount++
				if callCount == 1 {
					return &OperationsListResult{
						Operations: []OperationListItem{
							{ID: 1, CID: "QmTest", Status: "running", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
						},
						Total: 1,
					}, nil
				}
				return &OperationsListResult{
					Operations: []OperationListItem{
						{ID: 1, CID: "QmTest", Status: "completed", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 100, StartedAt: "2024-01-01"},
					},
					Total: 1,
				}, nil
			},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperationsList(ctx, opsSvc, output, OperationsListOptions{})
		require.NoError(t, err)
	})
}

func TestWatchOperation(t *testing.T) {
	t.Run("exits cleanly when context cancelled", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		ctx, cancel := context.WithCancel(context.Background())
		// Cancel quickly so the ticker loop exits via ctx.Done()
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		err := watchOperation(ctx, opsSvc, output, 42)
		require.NoError(t, err)
	})

	t.Run("exits when operation is complete on first ticker check", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().Get(mock.Anything, int64(7)).Return(&OperationDetail{
			ID:                   7,
			CID:                  "QmDone",
			Status:               "completed",
			StatusDisplayName:    "Completed",
			Operation:            "pin",
			OperationDisplayName: "Pin",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      100,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:01:00Z",
		}, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperation(ctx, opsSvc, output, 7)
		require.NoError(t, err)
	})

	t.Run("returns error when get fails during ticker loop", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().Get(mock.Anything, int64(99)).Return(nil, errors.New("not found"))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperation(ctx, opsSvc, output, 99)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("exits when operation reaches failed status", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		opsSvc.EXPECT().Get(mock.Anything, int64(5)).Return(&OperationDetail{
			ID:                   5,
			CID:                  "QmFail",
			Status:               "failed",
			StatusDisplayName:    "Failed",
			Operation:            "upload",
			OperationDisplayName: "Upload",
			Protocol:             "ipfs",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      30,
			StartedAt:            "2024-01-01T00:00:00Z",
			UpdatedAt:            "2024-01-01T00:01:00Z",
			Error:                "disk full",
		}, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := watchOperation(ctx, opsSvc, output, 5)
		require.NoError(t, err)
	})

	t.Run("detects status change between checks", func(t *testing.T) {
		opsSvc := NewMockOperationsService(t)
		output := newTestOutput()

		callCount := 0
		opsSvc.EXPECT().Get(mock.Anything, int64(10)).RunAndReturn(
			func(ctx context.Context, id int64) (*OperationDetail, error) {
				callCount++
				if callCount == 1 {
					return &OperationDetail{
						ID:                   10,
						CID:                  "QmProgress",
						Status:               "running",
						StatusDisplayName:    "Running",
						Operation:            "upload",
						OperationDisplayName: "Upload",
						Protocol:             "ipfs",
						ProtocolDisplayName:  "IPFS",
						ProgressPercent:      50,
						StartedAt:            "2024-01-01T00:00:00Z",
						UpdatedAt:            "2024-01-01T00:01:00Z",
					}, nil
				}
				return &OperationDetail{
					ID:                   10,
					CID:                  "QmProgress",
					Status:               "completed",
					StatusDisplayName:    "Completed",
					Operation:            "upload",
					OperationDisplayName: "Upload",
					Protocol:             "ipfs",
					ProtocolDisplayName:  "IPFS",
					ProgressPercent:      100,
					StartedAt:            "2024-01-01T00:00:00Z",
					UpdatedAt:            "2024-01-01T00:02:00Z",
				}, nil
			},
		)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := watchOperation(ctx, opsSvc, output, 10)
		require.NoError(t, err)
	})
}

func TestParseSortOptions(t *testing.T) {
	t.Run("single field default desc", func(t *testing.T) {
		sorts := parseSortOptions("id")
		require.Len(t, sorts, 1)
		assert.Equal(t, "id", sorts[0].Field)
		assert.Equal(t, queryutil.OrderDesc, sorts[0].Order)
	})

	t.Run("field with explicit asc", func(t *testing.T) {
		sorts := parseSortOptions("started:asc")
		require.Len(t, sorts, 1)
		assert.Equal(t, "started", sorts[0].Field)
		assert.Equal(t, queryutil.OrderAsc, sorts[0].Order)
	})

	t.Run("field with explicit desc", func(t *testing.T) {
		sorts := parseSortOptions("id:desc")
		require.Len(t, sorts, 1)
		assert.Equal(t, "id", sorts[0].Field)
		assert.Equal(t, queryutil.OrderDesc, sorts[0].Order)
	})

	t.Run("multiple sorts comma-separated", func(t *testing.T) {
		sorts := parseSortOptions("status:asc,id:desc")
		require.Len(t, sorts, 2)
		assert.Equal(t, "status", sorts[0].Field)
		assert.Equal(t, queryutil.OrderAsc, sorts[0].Order)
		assert.Equal(t, "id", sorts[1].Field)
		assert.Equal(t, queryutil.OrderDesc, sorts[1].Order)
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		sorts := parseSortOptions("")
		assert.Nil(t, sorts)
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		sorts := parseSortOptions("  id : desc  ")
		require.Len(t, sorts, 1)
		assert.Equal(t, "id", sorts[0].Field)
		assert.Equal(t, queryutil.OrderDesc, sorts[0].Order)
	})

	t.Run("invalid order defaults to desc", func(t *testing.T) {
		sorts := parseSortOptions("id:invalid")
		require.Len(t, sorts, 1)
		assert.Equal(t, "id", sorts[0].Field)
		assert.Equal(t, queryutil.OrderDesc, sorts[0].Order)
	})
}

func TestValidateOperationStatus(t *testing.T) {
	t.Run("empty status is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus(""))
	})

	t.Run("pending is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("pending"))
	})

	t.Run("running is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("running"))
	})

	t.Run("completed is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("completed"))
	})

	t.Run("failed is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("failed"))
	})

	t.Run("error is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("error"))
	})

	t.Run("invalid status returns error", func(t *testing.T) {
		err := validateOperationStatus("invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid status")
		assert.Contains(t, err.Error(), "pending, running, completed, failed, error")
	})

	t.Run("case sensitive", func(t *testing.T) {
		err := validateOperationStatus("Running")
		require.Error(t, err)
	})
}

func TestOperationsList_SortFlag(t *testing.T) {
	t.Run("passes sort to service", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
			Sort:  "id:desc",
			Limit: 0,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := newMockCommand().
			withString(FlagSort, "id:desc")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("passes custom sort to service", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
			Sort:  "started:asc",
			Limit: 0,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := newMockCommand().
			withString(FlagSort, "started:asc")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})
}

func TestOperationsList_StatusValidation(t *testing.T) {
	t.Run("rejects invalid status", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().
			withString(FlagStatus, "invalid")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid status")
	})

	t.Run("accepts valid status", func(t *testing.T) {
		cfgMgr := newTestConfigMgr(t)
		output := newTestOutput()
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
			StatusFilter: "running",
			Limit:        0,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := newMockCommand().
			withString(FlagStatus, "running")

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})
}
