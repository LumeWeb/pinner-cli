package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestBenchFilePath(t *testing.T) {
	tests := []struct {
		name     string
		index    int
		total    int
		depth    int
		expected string
	}{
		{
			name:     "single file no depth",
			index:    0,
			total:    1,
			depth:    0,
			expected: "bench_0000.dat",
		},
		{
			name:     "multiple files no depth",
			index:    2,
			total:    5,
			depth:    0,
			expected: "bench_0002.dat",
		},
		{
			name:     "single file with depth 1",
			index:    0,
			total:    1,
			depth:    1,
			expected: "level0/bench_0000.dat",
		},
		{
			name:     "single file with depth 3",
			index:    0,
			total:    1,
			depth:    3,
			expected: "level0/level1/level2/bench_0000.dat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := benchFilePath(tt.index, tt.total, tt.depth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

type mockBenchServiceForCLI struct {
	runFunc                func(ctx context.Context, opts BenchOptions) (*BenchResult, error)
	requireAuthenticatedFn func() error
}

func (m *mockBenchServiceForCLI) Run(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, opts)
	}
	return &BenchResult{}, nil
}

func (m *mockBenchServiceForCLI) RequireAuthenticated() error {
	if m.requireAuthenticatedFn != nil {
		return m.requireAuthenticatedFn()
	}
	return nil
}

func setupBenchHandlerTest(t *testing.T) (*mockBenchServiceForCLI, *configmocks.MockManager) {
	t.Helper()

	mockSvc := &mockBenchServiceForCLI{}
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
		MemoryLimit:  100,
	}).Maybe()

	origBenchFactory := defaultBenchServiceFactory
	origAuthFactory := benchAuthServiceFactory
	t.Cleanup(func() {
		defaultBenchServiceFactory = origBenchFactory
		benchAuthServiceFactory = origAuthFactory
	})

	mockAuthSvc := NewMockAuthService(t)
	mockAuthSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Maybe().Return(portalsdkmocks.NewMockAccountAPI(t), nil)

	benchAuthServiceFactory = func(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService {
		return mockAuthSvc
	}

	defaultBenchServiceFactory = func(cfgMgr config.Manager, output Output, uploadService UploadService, pinningService PinningService, accountClient portalsdk.AccountAPI) BenchService {
		return mockSvc
	}

	return mockSvc, cfgMgr
}

func defaultBenchCmd() *mockCommand {
	return newMockCommand().
		withString(FlagBenchSize, "1MB").
		withInt(FlagBenchFiles, 1).
		withInt(FlagBenchDepth, 0).
		withInt(FlagBenchIterations, 1).
		withInt(FlagParallel, 1).
		withBool(FlagBenchNoCleanup, false).
		withDuration(FlagBenchPollInterval, 500*time.Millisecond).
		withUint64(FlagMemoryLimit, 100).
		withBool(FlagDryRun, false)
}

func TestBenchHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupBenchHandlerTest(t)
	mockSvc.runFunc = func(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
		assert.Equal(t, int64(1048576), opts.SizeBytes)
		assert.Equal(t, 1, opts.Files)
		assert.Equal(t, 1, opts.Iterations)
		return &BenchResult{
			Input:      BenchInput{Type: "random", Size: 1048576, Files: 1},
			Iterations: []BenchIteration{{Number: 1, CID: "QmTest", Size: 1048576, Total: 100 * time.Millisecond}},
			Summary:    BenchSummary{TotalDuration: 100 * time.Millisecond, UploadDuration: 100 * time.Millisecond},
		}, nil
	}

	output := newTestOutput()
	cmd := defaultBenchCmd()
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestBenchHandler_InvalidSize(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withString(FlagBenchSize, "notasize")
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid size")
}

func TestBenchHandler_ZeroSizeNoPath(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withString(FlagBenchSize, "0B")
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size must be positive or provide a path")
}

func TestBenchHandler_ZeroFiles(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withInt(FlagBenchFiles, 0)
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "files must be at least 1")
}

func TestBenchHandler_ZeroIterations(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withInt(FlagBenchIterations, 0)
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iterations must be at least 1")
}

func TestBenchHandler_NegativeDepth(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withInt(FlagBenchDepth, -1)
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depth must be non-negative")
}

func TestBenchHandler_ZeroPollInterval(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withDuration(FlagBenchPollInterval, 0)
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll-interval must be positive")
}

func TestBenchHandler_DryRun(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withBool(FlagDryRun, true)
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestBenchHandler_DryRunWithPath(t *testing.T) {
	_, cfgMgr := setupBenchHandlerTest(t)

	output := newTestOutput()
	cmd := defaultBenchCmd().withBool(FlagDryRun, true).withArgs("./testdata")
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestBenchHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupBenchHandlerTest(t)
	mockSvc.runFunc = func(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
		return nil, errors.New("benchmark failed")
	}

	output := newTestOutput()
	cmd := defaultBenchCmd()
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "benchmark failed")
}

func TestBenchHandler_AuthError(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
		MemoryLimit:  100,
	}).Maybe()

	origAuthFactory := benchAuthServiceFactory
	t.Cleanup(func() { benchAuthServiceFactory = origAuthFactory })

	mockAuthSvc := NewMockAuthService(t)
	mockAuthSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(nil, errors.New("auth failed"))

	benchAuthServiceFactory = func(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService {
		return mockAuthSvc
	}

	output := newTestOutput()
	cmd := defaultBenchCmd()
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to authenticate for operation polling")
}

func TestBenchHandler_JSONOutput(t *testing.T) {
	mockSvc, cfgMgr := setupBenchHandlerTest(t)
	mockSvc.runFunc = func(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
		return &BenchResult{
			Input:      BenchInput{Type: "random", Size: 1048576, Files: 1},
			Iterations: []BenchIteration{{Number: 1, CID: "QmTest", Size: 1048576, Total: 100 * time.Millisecond}},
			Summary:    BenchSummary{TotalDuration: 100 * time.Millisecond, UploadDuration: 100 * time.Millisecond},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := defaultBenchCmd()
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestBenchHandler_WithPath(t *testing.T) {
	mockSvc, cfgMgr := setupBenchHandlerTest(t)
	mockSvc.runFunc = func(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
		assert.Equal(t, "./testdata", opts.Path)
		return &BenchResult{
			Input:      BenchInput{Type: "path", Path: "./testdata"},
			Iterations: []BenchIteration{{Number: 1, CID: "QmTest", Size: 1024, Total: 50 * time.Millisecond}},
			Summary:    BenchSummary{TotalDuration: 50 * time.Millisecond, UploadDuration: 50 * time.Millisecond},
		}, nil
	}

	output := newTestOutput()
	cmd := defaultBenchCmd().withString(FlagBenchSize, "0B").withArgs("./testdata")
	err := bench(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestGenerateRandomData(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		opts := BenchOptions{
			SizeBytes: 1024, // 1KB
			Files:     1,
			Depth:     0,
		}

		fsys, name, cleanup, err := generateRandomData(opts)
		require.NoError(t, err)
		assert.Equal(t, "bench", name)
		assert.NotNil(t, fsys)
		if cleanup != nil {
			cleanup()
		}
	})

	t.Run("multiple files with depth", func(t *testing.T) {
		opts := BenchOptions{
			SizeBytes: 3000, // 3KB total, 1KB each
			Files:     3,
			Depth:     2,
		}

		fsys, name, cleanup, err := generateRandomData(opts)
		require.NoError(t, err)
		assert.Equal(t, "bench_dir", name)
		assert.NotNil(t, fsys)
		if cleanup != nil {
			cleanup()
		}
	})

	t.Run("disk fallback for large data", func(t *testing.T) {
		// Test the disk fallback path directly with a small size to avoid
		// filling the disk on systems with large amounts of RAM.
		opts := BenchOptions{
			SizeBytes: 1024, // 1KB — small size, but we're testing the disk path directly
			Files:     1,
			Depth:     0,
		}

		fsys, name, cleanup, err := generateRandomDataDisk(opts)
		require.NoError(t, err)
		assert.Equal(t, "bench", name)
		assert.NotNil(t, fsys)
		assert.NotNil(t, cleanup, "disk path should return a cleanup func")
		cleanup()
	})
}

func TestBenchStorageMode(t *testing.T) {
	avail := availableMemory()

	t.Run("small data uses memory", func(t *testing.T) {
		if avail <= 0 {
			t.Skip("cannot detect available memory")
		}
		assert.Equal(t, "memory", benchStorageMode(1024))
	})

	t.Run("data exceeding available memory uses disk", func(t *testing.T) {
		if avail <= 0 {
			t.Skip("cannot detect available memory")
		}
		assert.Equal(t, "disk", benchStorageMode(math.MaxInt64))
	})
}

func TestBuildSummary(t *testing.T) {
	t.Run("single iteration", func(t *testing.T) {
		iterations := []BenchIteration{
			{
				Number: 1,
				CID:    "QmTest1",
				Size:   1024,
				Stages: []BenchStage{
					{Name: "generate", Duration: 10 * time.Millisecond},
					{Name: "upload", Duration: 100 * time.Millisecond},
					{Name: "queued", Duration: 50 * time.Millisecond},
					{Name: "pinning", Duration: 200 * time.Millisecond},
				},
				Total: 360 * time.Millisecond,
			},
		}

		summary := buildSummary(iterations, 400*time.Millisecond)

		assert.Equal(t, 400*time.Millisecond, summary.TotalDuration)
		assert.Equal(t, 360*time.Millisecond, summary.UploadDuration)
		assert.Len(t, summary.Stages, 4)

		assert.Equal(t, 10*time.Millisecond, summary.Stages["generate"].Min)
		assert.Equal(t, 10*time.Millisecond, summary.Stages["generate"].Max)
		assert.Equal(t, 10*time.Millisecond, summary.Stages["generate"].Avg)
		assert.Equal(t, 10*time.Millisecond, summary.Stages["generate"].Median)
	})

	t.Run("multiple iterations", func(t *testing.T) {
		iterations := []BenchIteration{
			{
				Number: 1,
				CID:    "QmTest1",
				Size:   1024,
				Stages: []BenchStage{
					{Name: "upload", Duration: 100 * time.Millisecond},
					{Name: "pinning", Duration: 200 * time.Millisecond},
				},
				Total: 300 * time.Millisecond,
			},
			{
				Number: 2,
				CID:    "QmTest2",
				Size:   1024,
				Stages: []BenchStage{
					{Name: "upload", Duration: 150 * time.Millisecond},
					{Name: "pinning", Duration: 300 * time.Millisecond},
				},
				Total: 450 * time.Millisecond,
			},
			{
				Number: 3,
				CID:    "QmTest3",
				Size:   1024,
				Stages: []BenchStage{
					{Name: "upload", Duration: 120 * time.Millisecond},
					{Name: "pinning", Duration: 250 * time.Millisecond},
				},
				Total: 370 * time.Millisecond,
			},
		}

		summary := buildSummary(iterations, 1200*time.Millisecond)

		// Upload: 100, 120, 150 → min=100, max=150, avg=123.33, median=120
		uploadStats := summary.Stages["upload"]
		assert.Equal(t, 100*time.Millisecond, uploadStats.Min)
		assert.Equal(t, 150*time.Millisecond, uploadStats.Max)
		assert.Equal(t, 123*time.Millisecond, uploadStats.Avg.Round(time.Millisecond))
		assert.Equal(t, 120*time.Millisecond, uploadStats.Median)

		// Pinning: 200, 250, 300 → min=200, max=300, avg=250, median=250
		pinningStats := summary.Stages["pinning"]
		assert.Equal(t, 200*time.Millisecond, pinningStats.Min)
		assert.Equal(t, 300*time.Millisecond, pinningStats.Max)
		assert.Equal(t, 250*time.Millisecond, pinningStats.Avg)
		assert.Equal(t, 250*time.Millisecond, pinningStats.Median)
	})

	t.Run("skips failed iterations", func(t *testing.T) {
		iterations := []BenchIteration{
			{
				Number: 1,
				CID:    "QmTest1",
				Size:   1024,
				Stages: []BenchStage{
					{Name: "upload", Duration: 100 * time.Millisecond},
				},
				Total: 100 * time.Millisecond,
			},
			{
				Number: 2,
				Error:  &BenchError{Message: "upload failed"},
			},
		}

		summary := buildSummary(iterations, 200*time.Millisecond)
		assert.Len(t, summary.Stages, 1)
		assert.Equal(t, 100*time.Millisecond, summary.Stages["upload"].Avg)
	})
}

func TestBenchService_RequireAuthenticated(t *testing.T) {
	t.Run("fails when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "",
		})

		output := newTestOutput()
		pinningService := NewPinningService(cfgMgr, output, "https://api.test.com")
		uploadService := NewUploadService(cfgMgr, output)

		svc := NewBenchService(cfgMgr, output, uploadService, pinningService, portalsdkmocks.NewMockAccountAPI(t))
		err := svc.RequireAuthenticated()
		assert.Error(t, err)
	})
}

func TestBenchService_Run_NotAuthenticated(t *testing.T) {
	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "",
		})

		output := newTestOutput()
		pinningService := NewPinningService(cfgMgr, output, "https://api.test.com")
		uploadService := NewUploadService(cfgMgr, output)

		svc := NewBenchService(cfgMgr, output, uploadService, pinningService, portalsdkmocks.NewMockAccountAPI(t))
		_, err := svc.Run(context.Background(), BenchOptions{
			SizeBytes:  1024,
			Files:      1,
			Iterations: 1,
		})
		assert.Error(t, err)
	})
}



func TestFormatBenchResult(t *testing.T) {
	t.Run("single iteration with random data", func(t *testing.T) {
		result := &BenchResult{
			Input: BenchInput{
				Type:  "random",
				Size:  1048576,
				Files: 1,
				Depth: 0,
			},
			Iterations: []BenchIteration{
				{
					Number: 1,
					CID:    "QmTestCID",
					Size:   1048576,
					Stages: []BenchStage{
						{Name: "generate", Duration: 5 * time.Millisecond},
						{Name: "upload", Duration: 120 * time.Millisecond},
						{Name: "queued", Duration: 50 * time.Millisecond},
						{Name: "pinning", Duration: 200 * time.Millisecond},
					},
					Total: 375 * time.Millisecond,
				},
			},
			Summary: BenchSummary{
				TotalDuration:  400 * time.Millisecond,
				UploadDuration: 375 * time.Millisecond,
				Stages: map[string]StageStats{
					"generate": {Avg: 5 * time.Millisecond},
					"upload":   {Avg: 120 * time.Millisecond},
					"queued":   {Avg: 50 * time.Millisecond},
					"pinning":  {Avg: 200 * time.Millisecond},
				},
			},
		}

		// Just verify it doesn't panic and produces output
		output := newTestOutput()
		formatBenchResult(output, result)
	})

	t.Run("multiple iterations with cleanup", func(t *testing.T) {
		result := &BenchResult{
			Input: BenchInput{
				Type: "path",
				Path: "./testdata",
			},
			Iterations: []BenchIteration{
				{
					Number: 1,
					CID:    "QmTest1",
					Size:   1024,
					Stages: []BenchStage{
						{Name: "upload", Duration: 100 * time.Millisecond},
					},
					Total: 100 * time.Millisecond,
				},
				{
					Number: 2,
					Error:  &BenchError{Message: "upload failed"},
				},
			},
			Cleanup: BenchCleanup{
				Unpinned: []BenchCleanupFailure{{CID: "QmTest1"}},
				Duration: 50 * time.Millisecond,
			},
			Summary: BenchSummary{
				TotalDuration:   200 * time.Millisecond,
				UploadDuration:  100 * time.Millisecond,
				CleanupDuration: 50 * time.Millisecond,
				Stages: map[string]StageStats{
					"upload": {Min: 100 * time.Millisecond, Max: 100 * time.Millisecond, Avg: 100 * time.Millisecond, Median: 100 * time.Millisecond},
				},
			},
		}

		output := newTestOutput()
		formatBenchResult(output, result)
	})
}

func TestStageStatsRow(t *testing.T) {
	stats := StageStats{
		Min:    10 * time.Millisecond,
		Max:    200 * time.Millisecond,
		Avg:    100 * time.Millisecond,
		Median: 90 * time.Millisecond,
	}

	t.Run("single iteration", func(t *testing.T) {
		row := stageStatsRow("upload", stats, false)
		assert.Equal(t, []string{"upload", "100ms"}, row)
	})

	t.Run("multiple iterations", func(t *testing.T) {
		row := stageStatsRow("upload", stats, true)
		assert.Equal(t, []string{"upload", "100ms", "10ms", "200ms", "90ms"}, row)
	})
}

func TestNewBenchError(t *testing.T) {
	t.Run("generic error", func(t *testing.T) {
		err := fmt.Errorf("something went wrong")
		benchErr := newBenchError(err)
		assert.Equal(t, "something went wrong", benchErr.Message)
		assert.Nil(t, benchErr.Detail)
	})

	t.Run("HTTPError", func(t *testing.T) {
		err := NewHTTPError(429, `{"error":"Upload quota exceeded"}`)
		benchErr := newBenchError(err)
		assert.Equal(t, "upload failed", benchErr.Message)
		assert.Equal(t, 429, benchErr.Detail["http_status"])
		assert.Equal(t, "Upload quota exceeded", benchErr.Detail["body"])
	})

	t.Run("wrapped HTTPError", func(t *testing.T) {
		httpErr := NewHTTPError(500, "internal server error")
		err := fmt.Errorf("upload failed: %w", httpErr)
		benchErr := newBenchError(err)
		assert.Equal(t, "upload failed", benchErr.Message)
		assert.Equal(t, 500, benchErr.Detail["http_status"])
		assert.Equal(t, "internal server error", benchErr.Detail["body"])
	})
}

func TestIsUnrecoverableError(t *testing.T) {
	t.Run("ErrUnauthorized is unrecoverable", func(t *testing.T) {
		assert.True(t, isUnrecoverableError(portalsdk.ErrUnauthorized))
	})

	t.Run("ErrForbidden is unrecoverable", func(t *testing.T) {
		assert.True(t, isUnrecoverableError(portalsdk.ErrForbidden))
	})

	t.Run("wrapped ErrUnauthorized is unrecoverable", func(t *testing.T) {
		err := fmt.Errorf("request failed: %w", portalsdk.ErrUnauthorized)
		assert.True(t, isUnrecoverableError(err))
	})

	t.Run("wrapped ErrForbidden is unrecoverable", func(t *testing.T) {
		err := fmt.Errorf("access denied: %w", portalsdk.ErrForbidden)
		assert.True(t, isUnrecoverableError(err))
	})

	t.Run("generic error is recoverable", func(t *testing.T) {
		assert.False(t, isUnrecoverableError(errors.New("something went wrong")))
	})

	t.Run("HTTPError 500 is recoverable", func(t *testing.T) {
		assert.False(t, isUnrecoverableError(NewHTTPError(500, "internal server error")))
	})

	t.Run("HTTPError 401 is recoverable (not a sentinel error)", func(t *testing.T) {
		assert.False(t, isUnrecoverableError(NewHTTPError(401, "unauthorized")))
	})

	t.Run("nil error is recoverable", func(t *testing.T) {
		assert.False(t, isUnrecoverableError(nil))
	})
}

func TestBenchError_String(t *testing.T) {
	err := &BenchError{Message: "test error message"}
	assert.Equal(t, "test error message", err.String())
}

func TestBenchServiceDefault_RunIteration(t *testing.T) {
	ctx := context.Background()

	newCompletedOp := func(id int) *portalsdk.Operation {
		op := &portalsdk.Operation{}
		op.Id = id
		op.Status = string(portalsdk.OperationStatusCompleted)
		op.Operation = "Pin"
		op.Protocol = "IPFS"
		op.ProgressPercent = 100
		op.StartedAt = time.Now()
		op.UpdatedAt = time.Now()
		return op
	}

	t.Run("successful upload and pin", func(t *testing.T) {
		uploadSvc := NewMockUploadService(t)
		pinningSvc := NewMockPinningService(t)
		accountAPI := portalsdkmocks.NewMockAccountAPI(t)
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		uploadSvc.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, false).
			Return(&UploadResult{CID: "QmTestCID", Size: 1024}, nil)

		accountAPI.EXPECT().ListOperations(mock.Anything, mock.Anything).
			Return([]*portalsdk.Operation{newCompletedOp(1)}, nil)
		accountAPI.EXPECT().GetOperation(mock.Anything, int64(1)).
			Return(newCompletedOp(1), nil)

		svc := &BenchServiceDefault{
			configMgr:      cfgMgr,
			output:         output,
			uploadService:  uploadSvc,
			pinningService: pinningSvc,
			accountClient:  accountAPI,
		}

		opts := BenchOptions{
			SizeBytes:   1024,
			Files:       1,
			Iterations:  1,
			PollInterval: 100 * time.Millisecond,
		}

		iter := svc.runIteration(ctx, opts, 0)
		assert.Equal(t, 1, iter.Number)
		assert.Equal(t, "QmTestCID", iter.CID)
		assert.Equal(t, int64(1024), iter.Size)
		assert.Nil(t, iter.Error)
		assert.GreaterOrEqual(t, iter.Total, time.Duration(0))

		stageNames := make([]string, len(iter.Stages))
		for i, s := range iter.Stages {
			stageNames[i] = s.Name
		}
		assert.Contains(t, stageNames, "generate")
		assert.Contains(t, stageNames, "upload")
	})

	t.Run("upload failure returns error", func(t *testing.T) {
		uploadSvc := NewMockUploadService(t)
		pinningSvc := NewMockPinningService(t)
		accountAPI := portalsdkmocks.NewMockAccountAPI(t)
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		uploadSvc.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, false).
			Return(nil, errors.New("network error"))

		svc := &BenchServiceDefault{
			configMgr:      cfgMgr,
			output:         output,
			uploadService:  uploadSvc,
			pinningService: pinningSvc,
			accountClient:  accountAPI,
		}

		opts := BenchOptions{
			SizeBytes:   1024,
			Files:       1,
			Iterations:  1,
			PollInterval: 100 * time.Millisecond,
		}

		iter := svc.runIteration(ctx, opts, 0)
		assert.Equal(t, 1, iter.Number)
		assert.Empty(t, iter.CID)
		assert.NotNil(t, iter.Error)
		assert.Equal(t, "network error", iter.Error.Message)
		assert.NotNil(t, iter.err)

		stageNames := make([]string, len(iter.Stages))
		for i, s := range iter.Stages {
			stageNames[i] = s.Name
		}
		assert.Contains(t, stageNames, "generate")
		assert.Contains(t, stageNames, "upload")
	})

	t.Run("upload failure with HTTPError has structured detail", func(t *testing.T) {
		uploadSvc := NewMockUploadService(t)
		pinningSvc := NewMockPinningService(t)
		accountAPI := portalsdkmocks.NewMockAccountAPI(t)
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		httpErr := NewHTTPError(429, "rate limit exceeded")
		uploadSvc.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, false).
			Return(nil, httpErr)

		svc := &BenchServiceDefault{
			configMgr:      cfgMgr,
			output:         output,
			uploadService:  uploadSvc,
			pinningService: pinningSvc,
			accountClient:  accountAPI,
		}

		opts := BenchOptions{
			SizeBytes:   1024,
			Files:       1,
			Iterations:  1,
			PollInterval: 100 * time.Millisecond,
		}

		iter := svc.runIteration(ctx, opts, 0)
		assert.NotNil(t, iter.Error)
		assert.Equal(t, "upload failed", iter.Error.Message)
		assert.Equal(t, 429, iter.Error.Detail["http_status"])
	})

	t.Run("upload failure with auth error is unrecoverable", func(t *testing.T) {
		uploadSvc := NewMockUploadService(t)
		pinningSvc := NewMockPinningService(t)
		accountAPI := portalsdkmocks.NewMockAccountAPI(t)
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		uploadSvc.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, false).
			Return(nil, portalsdk.ErrUnauthorized)

		svc := &BenchServiceDefault{
			configMgr:      cfgMgr,
			output:         output,
			uploadService:  uploadSvc,
			pinningService: pinningSvc,
			accountClient:  accountAPI,
		}

		opts := BenchOptions{
			SizeBytes:   1024,
			Files:       1,
			Iterations:  1,
			PollInterval: 100 * time.Millisecond,
		}

		iter := svc.runIteration(ctx, opts, 0)
		assert.NotNil(t, iter.Error)
		assert.True(t, isUnrecoverableError(iter.err))
	})

	t.Run("iteration number is 1-indexed", func(t *testing.T) {
		uploadSvc := NewMockUploadService(t)
		pinningSvc := NewMockPinningService(t)
		accountAPI := portalsdkmocks.NewMockAccountAPI(t)
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		uploadSvc.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, false).
			Return(&UploadResult{CID: "QmTestCID", Size: 1024}, nil)

		accountAPI.EXPECT().ListOperations(mock.Anything, mock.Anything).
			Return([]*portalsdk.Operation{newCompletedOp(1)}, nil)
		accountAPI.EXPECT().GetOperation(mock.Anything, int64(1)).
			Return(newCompletedOp(1), nil)

		svc := &BenchServiceDefault{
			configMgr:      cfgMgr,
			output:         output,
			uploadService:  uploadSvc,
			pinningService: pinningSvc,
			accountClient:  accountAPI,
		}

		opts := BenchOptions{
			SizeBytes:   1024,
			Files:       1,
			Iterations:  1,
			PollInterval: 100 * time.Millisecond,
		}

		iter := svc.runIteration(ctx, opts, 4)
		assert.Equal(t, 5, iter.Number)
	})
}
