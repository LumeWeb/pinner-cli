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
)

func TestOperationsServiceDefault_List(t *testing.T) {
	t.Run("returns operations from account client", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)

		svc := NewOperationsService(cfgMgr, output, nil)

		_, err := svc.List(context.Background(), OperationsListOptions{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when list operations fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		authSvc.EXPECT().GetAuthenticatedClient(context.Background()).Return(accountClient, nil)

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
		output := NewOutputFormatter(false, false, false, false)
		authSvc := NewMockAuthService(t)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		authSvc.EXPECT().GetAuthenticatedClient(context.Background()).Return(accountClient, nil).Once()

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
		output := NewOutputFormatter(false, false, false, false)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		cid := "QmDetail"
		accountClient.EXPECT().GetOperation(context.Background(), int64(42)).Return(
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
		output := NewOutputFormatter(false, false, false, false)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		errMsg := "disk full"
		accountClient.EXPECT().GetOperation(context.Background(), int64(99)).Return(
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
		output := NewOutputFormatter(false, false, false, false)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		accountClient.EXPECT().GetOperation(context.Background(), int64(404)).Return(
			nil, errors.New("not found"),
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		_, err := svc.Get(context.Background(), 404)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get operation 404")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)

		svc := NewOperationsService(cfgMgr, output, nil)

		_, err := svc.Get(context.Background(), 1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestOperationsServiceDefault_RequireAuthenticated(t *testing.T) {
	t.Run("returns error when auth service is nil", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)

		svc := NewOperationsService(cfgMgr, output, nil)

		err := svc.RequireAuthenticated()
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when auth client resolution fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
		authSvc := NewMockAuthService(t)

		authSvc.EXPECT().GetAuthenticatedClient(context.Background()).Return(nil, errors.New("auth failed"))

		svc := NewOperationsService(cfgMgr, output, authSvc)

		err := svc.RequireAuthenticated()
		require.Error(t, err)
	})

	t.Run("returns nil when authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}

func TestOperationsServiceDefault_Watch(t *testing.T) {
	t.Run("returns settled operation from WaitForOperation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
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
		output := NewOutputFormatter(false, false, false, false)
		currentStep := 2
		totalSteps := 5
		op := &OperationDetail{
			ID:                    1,
			CID:                   "QmTest",
			Status:                "running",
			StatusDisplayName:     "Running",
			Operation:             "upload",
			OperationDisplayName:  "Upload",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			ProgressPercent:       40,
			StartedAt:             "2024-01-01T00:00:00Z",
			UpdatedAt:             "2024-01-01T00:01:00Z",
			CurrentStep:           &currentStep,
			TotalSteps:            &totalSteps,
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})

	t.Run("renders operation with error and message", func(t *testing.T) {
		output := NewOutputFormatter(false, false, false, false)
		op := &OperationDetail{
			ID:                    2,
			CID:                   "QmErr",
			Status:                "failed",
			StatusDisplayName:     "Failed",
			Operation:             "upload",
			OperationDisplayName:  "Upload",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			ProgressPercent:       50,
			StartedAt:             "2024-01-01T00:00:00Z",
			UpdatedAt:             "2024-01-01T00:01:00Z",
			StatusMessage:         "upload failed",
			Error:                 "insufficient quota",
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})

	t.Run("renders minimal operation", func(t *testing.T) {
		output := NewOutputFormatter(false, false, false, false)
		op := &OperationDetail{
			ID:                    3,
			CID:                   "",
			Status:                "pending",
			StatusDisplayName:     "Pending",
			Operation:             "pin",
			OperationDisplayName:  "Pin",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			ProgressPercent:       0,
			StartedAt:             "2024-01-01T00:00:00Z",
			UpdatedAt:             "2024-01-01T00:00:00Z",
		}

		err := renderOperationDetail(output, op)
		require.NoError(t, err)
	})
}

func TestOperationsServiceDefault_CIDPointer(t *testing.T) {
	t.Run("handles nil CID pointer from operation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := NewOutputFormatter(false, false, false, false)
		accountClient := portalsdkmocks.NewMockAccountAPI(t)

		now := time.Now()
		op := makeOperation(1, "QmTest", "completed", "Pin", "IPFS", 100, now, nil, "")
		op.Cid = nil

		accountClient.EXPECT().GetOperation(context.Background(), int64(1)).Return(op, nil)

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

type mockOperationsCommand struct {
	status    string
	operation string
	protocol  string
	cid       string
	limit     int
	watch     bool
	args      []string
}

func (m *mockOperationsCommand) String(name string) string {
	switch name {
	case FlagStatus:
		return m.status
	case FlagOperation:
		return m.operation
	case FlagProtocol:
		return m.protocol
	case FlagCID:
		return m.cid
	default:
		return ""
	}
}

func (m *mockOperationsCommand) Int(name string) int {
	switch name {
	case FlagLimit:
		return m.limit
	default:
		return 0
	}
}

func (m *mockOperationsCommand) Bool(name string) bool {
	switch name {
	case FlagWatch:
		return m.watch
	default:
		return false
	}
}

func (m *mockOperationsCommand) Args() cli.Args {
	return &mockArgs{args: m.args}
}

func setupMockCfgMgr(t *testing.T) *configmocks.MockManager {
	t.Helper()
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
	}).Maybe()
	return cfgMgr
}

func TestOperationsList(t *testing.T) {
	t.Run("successful list with results", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(context.Background(), OperationsListOptions{
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

		cmd := &mockOperationsCommand{}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("successful list with empty results", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(context.Background(), OperationsListOptions{}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := &mockOperationsCommand{}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := &mockOperationsCommand{}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(context.Background(), OperationsListOptions{}).Return(nil, errors.New("server error"))

		cmd := &mockOperationsCommand{}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server error")
	})

	t.Run("passes filters to service", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().List(context.Background(), OperationsListOptions{
			StatusFilter:    "running",
			OperationFilter: "upload",
			ProtocolFilter:  "ipfs",
			CIDFilter:       "QmTest",
			Limit:           5,
		}).Return(&OperationsListResult{
			Operations: []OperationListItem{},
			Total:      0,
		}, nil)

		cmd := &mockOperationsCommand{
			status:    "running",
			operation: "upload",
			protocol:  "ipfs",
			cid:       "QmTest",
			limit:     5,
		}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsList(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when cfgMgr factory fails", func(t *testing.T) {
		output := NewOutputFormatter(false, false, false, false)
		cmd := &mockOperationsCommand{}

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
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().Get(context.Background(), int64(42)).Return(&OperationDetail{
			ID:                    42,
			CID:                   "QmTest",
			Status:                "completed",
			StatusDisplayName:     "Completed",
			Operation:             "pin",
			OperationDisplayName:  "Pin",
			Protocol:              "ipfs",
			ProtocolDisplayName:   "IPFS",
			ProgressPercent:       100,
			StartedAt:             "2024-01-01T00:00:00Z",
			UpdatedAt:             "2024-01-01T00:00:00Z",
		}, nil)

		cmd := &mockOperationsCommand{args: []string{"42"}}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.NoError(t, err)
	})

	t.Run("returns error when no operation ID provided", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)

		cmd := &mockOperationsCommand{args: []string{}}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrOperationNotFound))
	})

	t.Run("returns error for invalid operation ID", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)

		cmd := &mockOperationsCommand{args: []string{"not-a-number"}}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid operation ID")
	})

	t.Run("returns error when get fails", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(nil)
		opsSvc.EXPECT().Get(context.Background(), int64(999)).Return(nil, fmt.Errorf("not found"))

		cmd := &mockOperationsCommand{args: []string{"999"}}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := setupMockCfgMgr(t)
		output := NewOutputFormatter(false, false, false, false)
		opsSvc := NewMockOperationsService(t)

		opsSvc.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := &mockOperationsCommand{args: []string{"1"}}

		cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
		authSvcFactory := func(cm config.Manager, out Output, endpoint string) AuthService { return nil }
		svcFactory := func(cm config.Manager, out Output, as AuthService) OperationsService { return opsSvc }

		err := operationsGet(context.Background(), cmd, output, cfgMgrFactory, authSvcFactory, svcFactory)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("returns error when cfgMgr factory fails", func(t *testing.T) {
		output := NewOutputFormatter(false, false, false, false)
		cmd := &mockOperationsCommand{args: []string{"1"}}

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
