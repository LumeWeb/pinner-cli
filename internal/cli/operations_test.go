package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/opmesh"
	"go.lumeweb.com/pinner/catalogops"
	configmocks "go.lumeweb.com/pinner/core/config/mocks"
	"go.lumeweb.com/pinner/core/operations"
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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, cid, "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
		assert.Equal(t, 1, result.Operations[0].ID)
		assert.Equal(t, cid, result.Operations[0].CID)
		assert.Equal(t, "completed", result.Operations[0].Status)
		// List timestamps must match Get/Watch's RFC3339 (ISO 8601) format so an
		// agent feeding a list row into a later call never sees a format drift.
		assert.Equal(t, now.Format(time.RFC3339), result.Operations[0].StartedAt)
		assert.Equal(t, now.Format(time.RFC3339), result.Operations[0].UpdatedAt)
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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, cid1, "completed", "Pin", "IPFS", 100, now, nil, ""),
				makeOperation(2, cid2, "processing", "Upload", "IPFS", 50, now, nil, "processing"),
			},
			0,
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Operations, 2)
		assert.Equal(t, "processing", result.Operations[1].Status)
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
			mock.Anything,
		).Return(nil, 0, errors.New("server error"))

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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(3, "QmFailed", "failed", "Upload", "IPFS", 50, now, &errMsg, "upload failed"),
			},
			0,
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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmAuth", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmA", "completed", "Pin", "IPFS", 100, now, nil, ""),
			},
			0,
			nil,
		).Once()
		accountClient.EXPECT().ListOperations(
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(2, "QmB", "processing", "Upload", "IPFS", 50, now, nil, ""),
			},
			0,
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
			makeOperation(42, cid, "processing", "Upload", "IPFS", 75, now, nil, "processing"),
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		detail, err := svc.Get(context.Background(), 42)
		require.NoError(t, err)
		assert.Equal(t, 42, detail.ID)
		assert.Equal(t, cid, detail.CID)
		assert.Equal(t, "processing", detail.Status)
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
		{"processing is yellow", "processing"},
		{"failed is red", "failed"},
		{"duplicate is red", "duplicate"},
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
			mock.Anything,
		).Return(
			[]*portalsdk.Operation{
				makeOperation(1, "QmRunning", "processing", "Upload", "IPFS", 50, now, nil, ""),
			},
			0,
			nil,
		)

		svc := NewOperationsService(cfgMgr, output, nil, WithOperationsAccountClient(accountClient))

		result, err := svc.List(context.Background(), OperationsListOptions{StatusFilters: []string{"processing"}})
		require.NoError(t, err)
		require.Len(t, result.Operations, 1)
		assert.Equal(t, "processing", result.Operations[0].Status)
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
			Status:               "processing",
			StatusDisplayName:    "Processing",
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
		// Deterministic order: the catalog's Search sorts ops by name, so the
		// compiled subcommands are alphabetical (get < list).
		assert.Equal(t, "get", cmd.Commands[0].Name)
		assert.Equal(t, "list", cmd.Commands[1].Name)
	})
}

func TestNewOperationsListCommand(t *testing.T) {
	t.Run("catalog list subcommand has correct flags", func(t *testing.T) {
		root := newOperationsCommand()
		var cmd *cli.Command
		for _, c := range root.Commands {
			if c.Name == "list" {
				cmd = c
				break
			}
		}
		require.NotNil(t, cmd, "operations parent must expose a 'list' subcommand")
		assert.Equal(t, "list", cmd.Name)

		flagNames := make(map[string]bool)
		for _, f := range cmd.Flags {
			switch v := f.(type) {
			case *cli.StringFlag:
				flagNames[v.Name] = true
			case *cli.StringSliceFlag:
				flagNames[v.Name] = true
			case *cli.IntFlag:
				flagNames[v.Name] = true
			case *cli.BoolFlag:
				flagNames[v.Name] = true
			}
		}

		assert.True(t, flagNames[FlagStatus])
		assert.True(t, flagNames[FlagAll])
		assert.True(t, flagNames[FlagOperation])
		assert.True(t, flagNames[FlagProtocol])
		assert.True(t, flagNames[FlagCID])
		assert.True(t, flagNames[FlagSort])
		assert.True(t, flagNames[FlagPage])
		assert.True(t, flagNames[FlagPageSize])
		// NOTE: the legacy list command exposed --watch, but the core
		// operations.Service.List has no watch capability; list-watch is not
		// available (get --watch is preserved).
	})
}

func TestNewOperationsGetCommand(t *testing.T) {
	t.Run("catalog get subcommand is present", func(t *testing.T) {
		root := newOperationsCommand()
		var cmd *cli.Command
		for _, c := range root.Commands {
			if c.Name == "get" {
				cmd = c
				break
			}
		}
		require.NotNil(t, cmd, "operations parent must expose a 'get' subcommand")
		assert.Equal(t, "get", cmd.Name)
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
				{Status: "processing"},
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
				{ID: 1, CID: "QmTest", Status: "processing", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
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
							{ID: 1, CID: "QmTest", Status: "processing", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
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
							{ID: 1, CID: "QmTest", Status: "processing", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
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
							{ID: 1, CID: "QmTest", Status: "processing", Operation: "upload", OperationDisplayName: "Upload", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 50, StartedAt: "2024-01-01"},
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

	t.Run("processing is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("processing"))
	})

	t.Run("completed is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("completed"))
	})

	t.Run("failed is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("failed"))
	})

	t.Run("duplicate is valid", func(t *testing.T) {
		assert.NoError(t, validateOperationStatus("duplicate"))
	})

	t.Run("invalid status returns error", func(t *testing.T) {
		err := validateOperationStatus("invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid status")
		assert.Contains(t, err.Error(), "pending, processing, completed, failed, duplicate")
	})

	t.Run("case sensitive", func(t *testing.T) {
		err := validateOperationStatus("Processing")

		require.Error(t, err)
	})
}

// TestWatchCatalogOperationsList_PaginationAndAuth verifies the wiring-level
// regressions that Kody flagged on the watch path: watchCatalogOperationsList
// must (1) require authentication before polling and (2) clamp unset/zero
// page/page-size to the legacy defaults (1/10) so PageSize=0 does not disable
// pagination and fetch the entire operations table on every poll tick.
func TestWatchCatalogOperationsList_PaginationAndAuth(t *testing.T) {
	opsSvc := NewMockOperationsService(t)
	opsSvc.EXPECT().RequireAuthenticated().Return(nil)
	opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{Start: 0, Limit: 10, IsWatch: true}).Return(&OperationsListResult{
		Operations: []OperationListItem{
			{ID: 1, CID: "QmTest", Status: "completed", Operation: "pin", OperationDisplayName: "Pin", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 100, StartedAt: "2024-01-01"},
		},
		Total: 1,
	}, nil)

	prev := operationsCatalogDepsVar
	operationsCatalogDepsVar = catalogops.OperationsDeps{
		Service: func(map[string]any) operations.Service { return opsSvc },
	}
	defer func() { operationsCatalogDepsVar = prev }()

	var buf bytes.Buffer
	cmd := &cli.Command{Writer: &buf}
	// No page/page-size in input -> must clamp to 1/10.
	err := watchCatalogOperationsList(context.Background(), cmd, nil, map[string]any{})
	require.NoError(t, err)
}

// TestWatchCatalogOperationsList_RequiresAuth verifies that an unauthenticated
// service short-circuits the watch path with the auth error before any List
// call (the polling loop must not run when the caller is not authenticated).
func TestWatchCatalogOperationsList_RequiresAuth(t *testing.T) {
	opsSvc := NewMockOperationsService(t)
	opsSvc.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	prev := operationsCatalogDepsVar
	operationsCatalogDepsVar = catalogops.OperationsDeps{
		Service: func(map[string]any) operations.Service { return opsSvc },
	}
	defer func() { operationsCatalogDepsVar = prev }()

	var buf bytes.Buffer
	cmd := &cli.Command{Writer: &buf}
	err := watchCatalogOperationsList(context.Background(), cmd, nil, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

// TestWatchCatalogOperationsList_ForwardsSearch verifies the watch path threads
// the `search` arg from the input map into the ListOptions handed to the
// service, so `operations_list --watch --search foo` does not silently drop the
// server-side search filter (Kody regression: it was reconstructed without
// Search).
func TestWatchCatalogOperationsList_ForwardsSearch(t *testing.T) {
	opsSvc := NewMockOperationsService(t)
	opsSvc.EXPECT().RequireAuthenticated().Return(nil)
	opsSvc.EXPECT().List(mock.Anything, OperationsListOptions{
		Search:  "foo",
		Start:   0,
		Limit:   10,
		IsWatch: true,
	}).Return(&OperationsListResult{
		Operations: []OperationListItem{
			{ID: 1, CID: "QmTest", Status: "completed", Operation: "pin", OperationDisplayName: "Pin", Protocol: "ipfs", ProtocolDisplayName: "IPFS", ProgressPercent: 100, StartedAt: "2024-01-01"},
		},
		Total: 1,
	}, nil)

	prev := operationsCatalogDepsVar
	operationsCatalogDepsVar = catalogops.OperationsDeps{
		Service: func(map[string]any) operations.Service { return opsSvc },
	}
	defer func() { operationsCatalogDepsVar = prev }()

	var buf bytes.Buffer
	cmd := &cli.Command{Writer: &buf}
	err := watchCatalogOperationsList(context.Background(), cmd, nil, map[string]any{"search": "foo"})
	require.NoError(t, err)
}

// TestWatchCatalogOperationGet_LiveWiring proves `operations get --watch` is
// handled by the shared catalog adapter (Kody regression: the PreNormalizeWatch
// hook only wired list, so the compiled get --watch flag was silently ignored
// and the plain one-shot Get ran instead). Running the operations_get leaf
// through the production catalogActionAdapter + operationsCatalogConfig drives
// the typed Get watch poll loop: the pipeline short-circuits into
// watchCatalogOperationGet, polls Get until the operation settles, and prints
// the watch status lines before the terminal render.
func TestWatchCatalogOperationGet_LiveWiring(t *testing.T) {
	opsSvc := NewMockOperationsService(t)
	opsSvc.EXPECT().RequireAuthenticated().Return(nil)
	opsSvc.EXPECT().Get(mock.Anything, int64(7)).Return(&OperationDetail{
		ID:                   7,
		CID:                  "QmTest",
		Status:               "completed",
		StatusDisplayName:    "Completed",
		Operation:            "pin",
		OperationDisplayName: "Pin",
		Protocol:             "ipfs",
		ProtocolDisplayName:  "IPFS",
		ProgressPercent:      100,
		StartedAt:            "2024-01-01",
		UpdatedAt:            "2024-01-01",
	}, nil)

	prev := operationsCatalogDepsVar
	operationsCatalogDepsVar = catalogops.OperationsDeps{
		Service: func(map[string]any) operations.Service { return opsSvc },
	}
	defer func() { operationsCatalogDepsVar = prev }()

	var getOp opmesh.Operation
	for _, o := range catalogops.OperationsOperations(operationsCatalogDepsVar) {
		if o.Name() == "operations_get" {
			getOp = o
			break
		}
	}
	require.NotNil(t, getOp, "operations_get op should be found")

	var buf bytes.Buffer
	cmd := newPipelineCommand(getOp, operationsCatalogConfig())
	cmd.Writer = &buf

	// The watcher settles on the first 2s tick (Get returns a completed op).
	err := runPipeline(cmd, "--watch", "7")
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Watching operation 7", "get --watch should enter the typed watch loop")
	assert.Contains(t, out, "Operation has reached terminal status", "get --watch should observe settlement")
}

// TestWatchCatalogOperationGet_RequiresAuth verifies that an unauthenticated
// service short-circuits the get watch path with the auth error before any Get
// call (mirroring the list watcher's auth guard).
func TestWatchCatalogOperationGet_RequiresAuth(t *testing.T) {
	opsSvc := NewMockOperationsService(t)
	opsSvc.EXPECT().RequireAuthenticated().Return(errors.New("not authenticated"))

	prev := operationsCatalogDepsVar
	operationsCatalogDepsVar = catalogops.OperationsDeps{
		Service: func(map[string]any) operations.Service { return opsSvc },
	}
	defer func() { operationsCatalogDepsVar = prev }()

	var buf bytes.Buffer
	cmd := &cli.Command{Writer: &buf}
	err := watchCatalogOperationGet(context.Background(), cmd, nil, map[string]any{"id": int64(7)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}
