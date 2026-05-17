package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
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
		// Use a size larger than available memory to force disk path
		avail := availableMemory()
		if avail <= 0 {
			t.Skip("cannot detect available memory")
		}
		opts := BenchOptions{
			SizeBytes: avail + 1, // just over available memory
			Files:     1,
			Depth:     0,
		}

		fsys, name, cleanup, err := generateRandomData(opts)
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
		assert.Equal(t, "disk", benchStorageMode(avail+1))
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

		output := NewOutputFormatter(false, false, false, false)
		pinningService := NewPinningService(cfgMgr, output, "https://api.test.com")
		uploadService := NewUploadService(cfgMgr, output, "https://api.test.com")

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

		output := NewOutputFormatter(false, false, false, false)
		pinningService := NewPinningService(cfgMgr, output, "https://api.test.com")
		uploadService := NewUploadService(cfgMgr, output, "https://api.test.com")

		svc := NewBenchService(cfgMgr, output, uploadService, pinningService, portalsdkmocks.NewMockAccountAPI(t))
		_, err := svc.Run(context.Background(), BenchOptions{
			SizeBytes:  1024,
			Files:      1,
			Iterations: 1,
		})
		assert.Error(t, err)
	})
}

// mockBenchCommand is a test mock for benchCommandGetter.
type mockBenchCommand struct {
	size            string
	files           int
	depth           int
	iterations      int
	parallel        int
	noCleanup       bool
	pollInterval    time.Duration
	memoryLimit     uint64
	dryRun          bool
	path            string
	chunkSize       int64
	chunkerStrategy string
	maxLinks        int
}

func (m *mockBenchCommand) String(name string) string {
	switch name {
	case FlagBenchSize:
		return m.size
	case FlagChunker:
		return m.chunkerStrategy
	default:
		return ""
	}
}

func (m *mockBenchCommand) Int(name string) int {
	switch name {
	case FlagBenchFiles:
		return m.files
	case FlagBenchDepth:
		return m.depth
	case FlagBenchIterations:
		return m.iterations
	case FlagParallel:
		return m.parallel
	case FlagMaxLinks:
		return m.maxLinks
	default:
		return 0
	}
}

func (m *mockBenchCommand) Int64(name string) int64 {
	switch name {
	case FlagChunkSize:
		return m.chunkSize
	default:
		return 0
	}
}

func (m *mockBenchCommand) Bool(name string) bool {
	switch name {
	case FlagBenchNoCleanup:
		return m.noCleanup
	case FlagDryRun:
		return m.dryRun
	default:
		return false
	}
}

func (m *mockBenchCommand) Uint64(name string) uint64 {
	switch name {
	case FlagMemoryLimit:
		return m.memoryLimit
	default:
		return 0
	}
}

func (m *mockBenchCommand) Duration(name string) time.Duration {
	switch name {
	case FlagBenchPollInterval:
		return m.pollInterval
	default:
		return 0
	}
}

func (m *mockBenchCommand) Args() cli.Args {
	return &mockArgs{args: []string{m.path}}
}

// Ensure mock types satisfy interfaces at compile time.
var _ benchCommandGetter = (*mockBenchCommand)(nil)

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
		output := NewOutputFormatter(false, false, false, false)
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

		output := NewOutputFormatter(false, false, false, false)
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
