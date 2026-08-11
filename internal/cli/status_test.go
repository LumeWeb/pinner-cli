package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestStatus(t *testing.T) {
	tests := []struct {
		name        string
		cid         string
		watchFlag   bool
		setupMocks  func(*configmocks.MockManager, *MockPinningService, *MockStatusService)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful pin status check",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmXxx", false).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "successful pin status check with watch",
			cid:       "QmXxx",
			watchFlag: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmXxx", true).Return(
					&PinStatus{
						CID:     "QmXxx",
						Status:  "pinned",
						Created: "2024-01-01T00:00:00Z",
					},
					nil,
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "fallback to operation status when pin not found",
			cid:       "QmYyy",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmYyy", false).Return(
					nil,
					&OperationStatusResult{
						CID:                  "QmYyy",
						Status:               "completed",
						StatusDisplayName:    "Completed",
						Operation:            "pin",
						OperationDisplayName: "Pin",
						Protocol:             "ipfs",
						ProtocolDisplayName:  "IPFS",
						ProgressPercent:      100,
						StartedAt:            "2024-01-01T00:00:00Z",
						Source:               "operation",
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "operation status with error details",
			cid:       "QmZzz",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmZzz", false).Return(
					nil,
					&OperationStatusResult{
						CID:                  "QmZzz",
						Status:               "failed",
						StatusDisplayName:    "Failed",
						Operation:            "pin",
						OperationDisplayName: "Pin",
						Protocol:             "ipfs",
						ProtocolDisplayName:  "IPFS",
						ProgressPercent:      50,
						StartedAt:            "2024-01-01T00:00:00Z",
						Error:                "upload failed",
						Source:               "operation",
					},
					nil,
				)
			},
			wantErr: false,
		},
		{
			name:      "returns error when status check fails",
			cid:       "QmXxx",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmXxx", false).Return(
					nil,
					nil,
					errors.New("status check failed"),
				)
			},
			wantErr:     true,
			errContains: "status check failed",
		},
		{
			name:      "returns pin not found when no operation exists either",
			cid:       "QmMissing",
			watchFlag: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, pinSvc *MockPinningService, statusSvc *MockStatusService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
				})
				pinSvc.EXPECT().RequireAuthenticated().Return(nil)
				statusSvc.EXPECT().Status(mock.Anything, "QmMissing", false).Return(
					nil,
					nil,
					ErrPinNotFound,
				)
			},
			wantErr:     true,
			errContains: "pin not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			pinningSvc := NewMockPinningService(t)
			statusSvc := NewMockStatusService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, pinningSvc, statusSvc)
			}

			cmd := newMockCommand().
				withCID(tt.cid).
				withBool(FlagWatch, tt.watchFlag)

			pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
				return pinningSvc
			}

			statusServiceFactory := func(cm config.Manager, out Output, ps PinningService, as AuthService) StatusService {
				return statusSvc
			}

			err := status(context.Background(), cmd, output, cfgMgr, "", true, PinningServiceFactory(pinningServiceFactory), statusServiceFactory)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewStatusCommand(t *testing.T) {
	t.Run("creates status command with correct configuration", func(t *testing.T) {
		cmd := newStatusCommand()

		assert.Equal(t, "status", cmd.Name)
		assert.Equal(t, "<cid>", cmd.ArgsUsage)

		flags := cmd.Flags
		assert.Len(t, flags, 1)

		watchFlag, ok := flags[0].(*cli.BoolFlag)
		require.True(t, ok)
		assert.Equal(t, "watch", watchFlag.Name)
	})
}

func TestRenderPinStatus(t *testing.T) {
	t.Run("renders pin status without delegates", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		pinStatus := &PinStatus{
			CID:     "QmXxx",
			Status:  "pinned",
			Created: "2024-01-01T00:00:00Z",
		}

		err := renderPinStatus(output, pinStatus)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, "QmXxx")
		assert.Contains(t, result, "pinned")
		assert.Contains(t, result, "2024-01-01T00:00:00Z")
	})

	t.Run("renders pin status with delegates", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		pinStatus := &PinStatus{
			CID:       "QmXxx",
			Status:    "pinned",
			Created:   "2024-01-01T00:00:00Z",
			Delegates: []string{"delegate1", "delegate2"},
		}

		err := renderPinStatus(output, pinStatus)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, "QmXxx")
		assert.Contains(t, result, "Delegates:")
		assert.Contains(t, result, "delegate1")
		assert.Contains(t, result, "delegate2")
	})

	t.Run("renders pin status as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		pinStatus := &PinStatus{
			CID:     "QmXxx",
			Status:  "pinned",
			Created: "2024-01-01T00:00:00Z",
		}

		err := renderPinStatus(output, pinStatus)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, `"CID"`)
		assert.Contains(t, result, `"QmXxx"`)
		assert.Contains(t, result, `"pinned"`)
	})
}

func TestRenderOperationStatus(t *testing.T) {
	t.Run("renders operation status", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		op := &OperationStatusResult{
			CID:                  "QmYyy",
			StatusDisplayName:    "Completed",
			OperationDisplayName: "Pin",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      100,
			StartedAt:            "2024-01-01T00:00:00Z",
		}

		err := renderOperationStatus(output, op)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, "QmYyy")
		assert.Contains(t, result, "Completed")
		assert.Contains(t, result, "Pin")
		assert.Contains(t, result, "IPFS")
		assert.Contains(t, result, "100%")
		assert.Contains(t, result, "2024-01-01T00:00:00Z")
	})

	t.Run("renders operation status with message and error", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		op := &OperationStatusResult{
			CID:                  "QmZzz",
			StatusDisplayName:    "Failed",
			OperationDisplayName: "Pin",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      50,
			StartedAt:            "2024-01-01T00:00:00Z",
			StatusMessage:        "processing stalled",
			Error:                "upload failed",
		}

		err := renderOperationStatus(output, op)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, "processing stalled")
		assert.Contains(t, result, "upload failed")
	})

	t.Run("renders operation status as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		op := &OperationStatusResult{
			CID:                  "QmYyy",
			StatusDisplayName:    "Completed",
			OperationDisplayName: "Pin",
			ProtocolDisplayName:  "IPFS",
			ProgressPercent:      100,
			StartedAt:            "2024-01-01T00:00:00Z",
		}

		err := renderOperationStatus(output, op)
		require.NoError(t, err)

		result := buf.String()
		assert.Contains(t, result, `"CID"`)
		assert.Contains(t, result, `"QmYyy"`)
		assert.Contains(t, result, `"Completed"`)
	})
}
