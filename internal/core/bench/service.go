// Package bench defines the BenchService contract for running benchmark
// pipelines against the Pinner content-network, decoupled from any CLI/MCP
// presentation layer. It carries the result models and the service interface.
// The concrete implementation is inherently CLI-coupled (interactive progress
// reporting, signal handling, random data generation) and stays in pkg/cli.
package bench

import (
	"context"
	"time"
)

// BenchStage represents a single stage in the benchmark pipeline.
type BenchStage struct {
	Name      string        `json:"name"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
}

// BenchError represents a structured error for JSON serialization.
type BenchError struct {
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// String returns the message for human-readable output.
func (e *BenchError) String() string {
	return e.Message
}

// BenchIteration tracks all stages for a single upload iteration.
type BenchIteration struct {
	Number int           `json:"iteration"`
	CID    string        `json:"cid,omitempty"`
	Size   int64         `json:"size"`
	Stages []BenchStage  `json:"stages"`
	Error  *BenchError   `json:"error,omitempty"`
	Total  time.Duration `json:"total"`

	// Err is the original Go error for formatted output (not serialized).
	Err error `json:"-"`
}

// BenchCleanup tracks the cleanup (unpin) phase.
type BenchCleanup struct {
	Unpinned []BenchCleanupFailure `json:"unpinned"`
	Failed   []BenchCleanupFailure `json:"failed"`
	Duration time.Duration         `json:"duration"`
}

// BenchCleanupFailure records a CID that failed to unpin.
type BenchCleanupFailure struct {
	CID   string `json:"cid"`
	Error string `json:"error"`
}

// BenchInput describes the benchmark input configuration.
type BenchInput struct {
	Type    string `json:"type"` // "random" or "path"
	Size    int64  `json:"size"` // total bytes
	Files   int    `json:"files"`
	Depth   int    `json:"depth"`
	Path    string `json:"path,omitempty"`
	Storage string `json:"storage,omitempty"` // "memory" or "disk" (only for random type)
}

// BenchSummary contains aggregated statistics across iterations.
type BenchSummary struct {
	TotalDuration   time.Duration         `json:"total_duration"`
	UploadDuration  time.Duration         `json:"upload_duration"`
	CleanupDuration time.Duration         `json:"cleanup_duration"`
	Stages          map[string]StageStats `json:"stages"`
}

// StageStats contains per-stage aggregate statistics.
type StageStats struct {
	Min    time.Duration `json:"min"`
	Max    time.Duration `json:"max"`
	Avg    time.Duration `json:"avg"`
	Median time.Duration `json:"median"`
}

// BenchResult is the top-level result of a benchmark run.
type BenchResult struct {
	Input      BenchInput       `json:"input"`
	Iterations []BenchIteration `json:"iterations"`
	Cleanup    BenchCleanup     `json:"cleanup"`
	Summary    BenchSummary     `json:"summary"`
}

// BenchOptions configures the benchmark run.
type BenchOptions struct {
	SizeBytes       int64
	Files           int
	Depth           int
	Iterations      int
	Parallel        int
	NoCleanup       bool
	PollInterval    time.Duration
	MemoryLimit     uint64
	DryRun          bool
	Path            string
	ChunkSize       int64
	ChunkerStrategy string
	MaxLinks        int
}

// Service defines the interface for running benchmarks.
type Service interface {
	Run(ctx context.Context, opts BenchOptions) (*BenchResult, error)
	RequireAuthenticated() error
}
