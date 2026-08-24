package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/docker/go-units"
	benchpkg "go.lumeweb.com/pinner-cli/internal/core/bench"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.lumeweb.com/queryutil/filter"
	"testing/fstest"
)

// Bench models and the BenchService contract are re-exported from core. The
// impl (BenchServiceDefault) stays in pkg/cli because it is irreducibly
// presentation-coupled (interactive progress reporting, signal handling).

type BenchStage = benchpkg.BenchStage
type BenchError = benchpkg.BenchError
type BenchIteration = benchpkg.BenchIteration
type BenchCleanup = benchpkg.BenchCleanup
type BenchCleanupFailure = benchpkg.BenchCleanupFailure
type BenchInput = benchpkg.BenchInput
type BenchSummary = benchpkg.BenchSummary
type StageStats = benchpkg.StageStats
type BenchResult = benchpkg.BenchResult
type BenchOptions = benchpkg.BenchOptions
type BenchService = benchpkg.Service

// BenchServiceFactory creates a BenchService with dependencies.
type BenchServiceFactory func(cfgMgr config.Manager, output Output, uploadService UploadService, pinningService PinningService, accountClient portalsdk.AccountAPI) BenchService

var defaultBenchServiceFactory BenchServiceFactory = NewBenchService

// BenchServiceDefault provides benchmark operations.
type BenchServiceDefault struct {
	configMgr      config.Manager
	output         Output
	uploadService  UploadService
	pinningService PinningService
	accountClient  portalsdk.AccountAPI
}

// NewBenchService creates a new BenchService with the given dependencies.
func NewBenchService(cfgMgr config.Manager, output Output, uploadService UploadService, pinningService PinningService, accountClient portalsdk.AccountAPI) BenchService {
	return &BenchServiceDefault{
		configMgr:      cfgMgr,
		output:         output,
		uploadService:  uploadService,
		pinningService: pinningService,
		accountClient:  accountClient,
	}
}

// RequireAuthenticated checks if the upload and pinning services are authenticated.
func (s *BenchServiceDefault) RequireAuthenticated() error {
	if err := s.pinningService.RequireAuthenticated(); err != nil {
		return err
	}
	return nil
}

// Run executes the benchmark with the given options.
func (s *BenchServiceDefault) Run(ctx context.Context, opts BenchOptions) (*BenchResult, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	result := &BenchResult{
		Input: BenchInput{
			Type:    "random",
			Size:    opts.SizeBytes,
			Files:   opts.Files,
			Depth:   opts.Depth,
			Storage: benchStorageMode(opts.SizeBytes),
		},
		Iterations: make([]BenchIteration, 0, opts.Iterations),
		Cleanup: BenchCleanup{
			Unpinned: make([]BenchCleanupFailure, 0),
			Failed:   make([]BenchCleanupFailure, 0),
		},
	}

	if opts.Path != "" {
		result.Input.Type = "path"
		result.Input.Path = opts.Path
	}

	var uploadedCIDs []string

	// Ensure cleanup runs no matter what
	cleanup := func() {
		if opts.NoCleanup || len(uploadedCIDs) == 0 {
			return
		}
		cleanupStart := time.Now()

		batchOpts := BatchOptions{
			Parallel:   opts.Parallel,
			ContinueOn: true,
		}

		// Retry unpin; the pin may take a moment to propagate to the
		// pinning API after the operation completes.
		var batchResult *BatchResult
		err := retry.New(
			retry.Context(ctx),
			retry.Attempts(5),
			retry.Delay(time.Second),
			retry.DelayType(retry.BackOffDelay),
			retry.MaxDelay(10*time.Second),
			retry.LastErrorOnly(true),
		).Do(func() error {
			var err error
			batchResult, err = s.pinningService.UnpinBatch(ctx, uploadedCIDs, batchOpts)
			if err != nil {
				if isUnrecoverableError(err) {
					return retry.Unrecoverable(err)
				}
				return err
			}
			// If all CIDs failed to unpin, retry (pin may not be visible yet)
			if batchResult != nil && len(batchResult.Succeeded) == 0 && len(batchResult.Failed) > 0 {
				return fmt.Errorf("all %d unpins failed", len(batchResult.Failed))
			}
			return nil
		})
		if err != nil {
			if !s.output.IsJSON() {
				s.output.Printfln("Cleanup error: %v", err)
			}
		}

		result.Cleanup.Duration = time.Since(cleanupStart)

		if batchResult != nil {
			for _, op := range batchResult.Succeeded {
				result.Cleanup.Unpinned = append(result.Cleanup.Unpinned, BenchCleanupFailure{CID: op.CID})
			}
			for _, opErr := range batchResult.Failed {
				result.Cleanup.Failed = append(result.Cleanup.Failed, BenchCleanupFailure(opErr))
			}
		}
	}
	defer cleanup()

	// Handle SIGINT for graceful cleanup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		s.output.Printf("\nInterrupted, cleaning up...")
		cleanup()
		os.Exit(130)
	}()
	defer signal.Stop(sigChan)

	// Run iterations
	totalStart := time.Now()

	for i := 0; i < opts.Iterations; i++ {
		iteration := s.runIteration(ctx, opts, i)
		result.Iterations = append(result.Iterations, iteration)

		if iteration.CID != "" {
			uploadedCIDs = append(uploadedCIDs, iteration.CID)
		}
	}

	// Build summary
	result.Summary = buildSummary(result.Iterations, time.Since(totalStart))

	return result, nil
}

// runIteration executes a single benchmark iteration.
func (s *BenchServiceDefault) runIteration(ctx context.Context, opts BenchOptions, index int) BenchIteration {
	iteration := BenchIteration{Number: index + 1}

	// Stage: generate data
	genStart := time.Now()
	var filesystem fs.FS
	var name string

	if opts.Path != "" {
		input, err := resolveUploadInput(opts.Path, "")
		if err != nil {
			iteration.Error = newBenchError(err)
			return iteration
		}
		defer func() { _ = input.Close() }()
		filesystem = input.Filesystem
		name = input.Name
	} else {
		var cleanup func()
		var err error
		filesystem, name, cleanup, err = generateRandomData(opts)
		if err != nil {
			iteration.Error = newBenchError(err)
			return iteration
		}
		if cleanup != nil {
			defer cleanup()
		}
	}

	iteration.Stages = append(iteration.Stages, BenchStage{
		Name:      "generate",
		StartedAt: genStart,
		EndedAt:   time.Now(),
		Duration:  time.Since(genStart),
	})

	// Stage: upload (SDK handles CAR generation + HTTP transfer)
	uploadStart := time.Now()
	uploadResult, err := s.uploadService.Upload(ctx, filesystem, name, false, false)
	uploadDuration := time.Since(uploadStart)

	if err != nil {
		iteration.Error = newBenchError(err)
		iteration.Err = err
		iteration.Stages = append(iteration.Stages, BenchStage{
			Name:      "upload",
			StartedAt: uploadStart,
			EndedAt:   time.Now(),
			Duration:  uploadDuration,
		})
		iteration.Total = time.Since(genStart)
		return iteration
	}

	iteration.Stages = append(iteration.Stages, BenchStage{
		Name:      "upload",
		StartedAt: uploadStart,
		EndedAt:   time.Now(),
		Duration:  uploadDuration,
	})

	iteration.CID = uploadResult.CID
	iteration.Size = uploadResult.Size

	// Stage: poll for server-side processing stages (queued → processing)
	pollStages, opErr := s.pollOperationStages(ctx, uploadResult.CID, opts.PollInterval)
	iteration.Stages = append(iteration.Stages, pollStages...)
	if opErr != "" {
		iteration.Error = &BenchError{Message: opErr}
		iteration.Err = fmt.Errorf("%s", opErr)
	}

	iteration.Total = time.Since(genStart)
	return iteration
}

// pollOperationStages polls the account operations API for stage transitions.
// It tracks pending → running → completed transitions with timing.
// Returns the stages and an error message if the operation failed.
func (s *BenchServiceDefault) pollOperationStages(ctx context.Context, cid string, pollInterval time.Duration) ([]BenchStage, string) {
	var stages []BenchStage
	lastStatus := ""
	stageStart := time.Now()
	var operationID int64
	var opErrorMsg string

	// Find the operation for this CID with retry (operation may not be visible immediately)
	s.output.PrintVerbosef("  Bench: looking up operation for CID %s", cid)
	err := retry.New(
		retry.Context(ctx),
		retry.Attempts(10),
		retry.Delay(pollInterval),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxDelay(5*time.Second),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			s.output.PrintVerbosef("  Bench: retry %d looking up operation: %v", n+1, err)
		}),
	).Do(func() error {
		operations, _, err := s.accountClient.ListOperations(ctx, portalsdk.WithFilters(filter.FieldEqual("cid", cid)))
		if err != nil {
			if isUnrecoverableError(err) {
				return retry.Unrecoverable(err)
			}
			return err
		}
		if len(operations) == 0 {
			return fmt.Errorf("no operation found for CID %s", cid)
		}
		operationID = int64(operations[0].Id)
		return nil
	})
	if err != nil {
		s.output.PrintVerbosef("  Bench: failed to find operation: %v", err)
		return stages, fmt.Sprintf("failed to find operation: %v", err)
	}
	s.output.PrintVerbosef("  Bench: found operation %d for CID %s", operationID, cid)

	// Map operation statuses to bench stage names
	stageName := func(status string) string {
		switch portalsdk.OperationStatus(status) {
		case portalsdk.OperationStatusPending:
			return "queued"
		case portalsdk.OperationStatusProcessing:
			return "processing"
		default:
			return status
		}
	}

	// Check if a status is terminal
	isTerminal := func(status string) bool {
		return portalsdk.OperationStatus(status).IsSettled()
	}

	// Poll the operation status
	checkStatus := func() bool {
		op, err := s.accountClient.GetOperation(ctx, operationID)
		if err != nil {
			s.output.PrintVerbosef("  Bench poll error: %v", err)
			return false
		}

		currentStatus := op.Status
		if currentStatus != lastStatus {
			if lastStatus != "" {
				stages = append(stages, BenchStage{
					Name:      stageName(lastStatus),
					StartedAt: stageStart,
					EndedAt:   time.Now(),
					Duration:  time.Since(stageStart),
				})
			}
			stageStart = time.Now()
			lastStatus = currentStatus

			s.output.PrintVerbosef("  Stage: %s (%s)", stageName(currentStatus), currentStatus)
		}

		if isTerminal(currentStatus) {
			// Capture error details if the operation failed
			if portalsdk.OperationStatus(currentStatus) == portalsdk.OperationStatusFailed ||
				portalsdk.OperationStatus(currentStatus) == portalsdk.OperationStatusDuplicate {
				op, _ := s.accountClient.GetOperation(ctx, operationID)
				if op != nil {
					detail := op.StatusMessage
					if op.Error != nil && *op.Error != "" {
						detail = *op.Error
					}
					if detail != "" {
						opErrorMsg = fmt.Sprintf("operation %s: %s", currentStatus, detail)
					} else {
						opErrorMsg = fmt.Sprintf("operation %s", currentStatus)
					}
				}
			}
			return true
		}
		return false
	}

	// Immediate first poll
	if done := checkStatus(); done {
		return stages, opErrorMsg
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if lastStatus != "" && !isTerminal(lastStatus) {
				stages = append(stages, BenchStage{
					Name:      stageName(lastStatus),
					StartedAt: stageStart,
					EndedAt:   time.Now(),
					Duration:  time.Since(stageStart),
				})
			}
			return stages, opErrorMsg
		case <-ticker.C:
			if done := checkStatus(); done {
				return stages, opErrorMsg
			}
		}
	}
}

// generateRandomData creates random file content for benchmarking.
// If the requested size fits in available memory, it uses fstest.MapFS (in-memory).
// If the size exceeds available memory, it writes random files to a temp directory
// on disk and returns os.DirFS. A cleanup function is returned to remove the temp dir.
func generateRandomData(opts BenchOptions) (fs.FS, string, func(), error) {
	if benchStorageMode(opts.SizeBytes) == "disk" {
		return generateRandomDataDisk(opts)
	}
	fs, name, err := generateRandomDataMemory(opts)
	return fs, name, nil, err
}

// benchStorageMode returns "disk" if the requested size exceeds available system memory,
// or "memory" if it fits.
func benchStorageMode(sizeBytes int64) string {
	avail := availableMemory()
	if avail > 0 && sizeBytes > avail {
		return "disk"
	}
	return "memory"
}

// generateRandomDataMemory creates random file content in memory using fstest.MapFS.
func generateRandomDataMemory(opts BenchOptions) (fs.FS, string, error) {
	mapFS := make(fstest.MapFS)
	now := time.Now()

	fileSize := opts.SizeBytes
	if opts.Files > 1 {
		fileSize = opts.SizeBytes / int64(opts.Files)
	}

	for i := 0; i < opts.Files; i++ {
		content := make([]byte, fileSize)
		if _, err := rand.Read(content); err != nil {
			return nil, "", fmt.Errorf("failed to generate random data: %w", err)
		}

		path := benchFilePath(i, opts.Files, opts.Depth)
		mapFS[path] = &fstest.MapFile{
			Data:    content,
			Mode:    0644,
			ModTime: now,
		}
	}

	name := "bench"
	if opts.Depth > 0 || opts.Files > 1 {
		name = "bench_dir"
	}

	return mapFS, name, nil
}

// generateRandomDataDisk writes random files to a temp directory and returns os.DirFS.
func generateRandomDataDisk(opts BenchOptions) (fs.FS, string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "pinner-bench-*")
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	fileSize := opts.SizeBytes
	if opts.Files > 1 {
		fileSize = opts.SizeBytes / int64(opts.Files)
	}

	for i := 0; i < opts.Files; i++ {
		path := benchFilePath(i, opts.Files, opts.Depth)
		fullPath := filepath.Join(tmpDir, path)

		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			cleanup()
			return nil, "", nil, fmt.Errorf("failed to create directory structure: %w", err)
		}

		f, err := os.Create(fullPath)
		if err != nil {
			cleanup()
			return nil, "", nil, fmt.Errorf("failed to create temp file: %w", err)
		}

		if _, err := randReadFile(f, fileSize); err != nil {
			_ = f.Close()
			cleanup()
			return nil, "", nil, fmt.Errorf("failed to write random data: %w", err)
		}

		if err := f.Close(); err != nil {
			cleanup()
			return nil, "", nil, fmt.Errorf("failed to close temp file: %w", err)
		}
	}

	name := "bench"
	if opts.Depth > 0 || opts.Files > 1 {
		name = "bench_dir"
	}

	return os.DirFS(tmpDir), name, cleanup, nil
}

// randReadFile writes random bytes to a file in chunks to avoid allocating
// the entire file content in memory at once.
func randReadFile(f *os.File, size int64) (int64, error) {
	const chunkSize = 32 * 1024 * 1024 // 32MB chunks
	buf := make([]byte, chunkSize)
	var written int64

	for written < size {
		remaining := size - written
		if remaining < int64(len(buf)) {
			buf = buf[:remaining]
		}

		if _, err := rand.Read(buf); err != nil {
			return written, err
		}

		n, err := f.Write(buf)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}

	return written, nil
}

// benchFilePath generates a file path for the given index, distributing across depth levels.
func benchFilePath(index, total, depth int) string {
	filename := fmt.Sprintf("bench_%04d.dat", index)

	if depth == 0 {
		return filename
	}

	parts := make([]string, 0, depth+1)
	for d := 0; d < depth; d++ {
		parts = append(parts, fmt.Sprintf("level%d", d))
	}
	parts = append(parts, filename)

	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result = result + "/" + parts[i]
	}
	return result
}

// buildSummary aggregates iteration results into summary statistics.
func buildSummary(iterations []BenchIteration, totalDuration time.Duration) BenchSummary {
	summary := BenchSummary{
		TotalDuration: totalDuration,
		Stages:        make(map[string]StageStats),
	}

	stageDurations := make(map[string][]time.Duration)

	for _, iter := range iterations {
		if iter.Error != nil {
			continue
		}
		summary.UploadDuration += iter.Total
		for _, stage := range iter.Stages {
			stageDurations[stage.Name] = append(stageDurations[stage.Name], stage.Duration)
		}
	}

	for name, durations := range stageDurations {
		if len(durations) == 0 {
			continue
		}

		sort.Slice(durations, func(i, j int) bool {
			return durations[i] < durations[j]
		})

		var sum time.Duration
		for _, d := range durations {
			sum += d
		}

		median := durations[len(durations)/2]
		if len(durations)%2 == 0 && len(durations) > 1 {
			median = (durations[len(durations)/2-1] + durations[len(durations)/2]) / 2
		}

		summary.Stages[name] = StageStats{
			Min:    durations[0],
			Max:    durations[len(durations)-1],
			Avg:    sum / time.Duration(len(durations)),
			Median: median,
		}
	}

	return summary
}

// formatBenchResult renders a BenchResult as human-readable output.
func formatBenchResult(output Output, result *BenchResult) {
	input := result.Input
	multiIter := len(result.Iterations) > 1

	// Benchmark config header
	headerFields := []Field{}
	if input.Type == "random" {
		headerFields = append(headerFields,
			Field{Label: "Size", Value: units.HumanSize(float64(input.Size))},
			Field{Label: "Files", Value: fmt.Sprintf("%d", input.Files)},
			Field{Label: "Depth", Value: fmt.Sprintf("%d", input.Depth)},
			Field{Label: "Storage", Value: input.Storage},
		)
	} else {
		headerFields = append(headerFields,
			Field{Label: "Path", Value: input.Path},
		)
	}
	output.PrintFields(FieldGroup{Title: "Benchmark", Fields: headerFields, PadTop: 1})

	// Per-iteration details
	for _, iter := range result.Iterations {
		if iter.Err != nil {
			output.Printfln("  Iteration %d: FAILED - %s", iter.Number, FormatError(iter.Err, output.IsVerbose()))
			continue
		}

		iterFields := []Field{
			{Label: "CID", Value: iter.CID},
			{Label: "Size", Value: humanReadableSize(iter.Size)},
			{Label: "Total", Value: iter.Total.Round(time.Millisecond).String()},
		}
		output.PrintFields(FieldGroup{
			Title:  fmt.Sprintf("Iteration %d", iter.Number),
			Fields: iterFields,
			PadTop: 1,
		})

		// Stage table (always show per-iteration stages)
		headers := []string{"Stage", "Duration"}
		rows := make([][]string, len(iter.Stages))
		for i, stage := range iter.Stages {
			rows[i] = []string{stage.Name, stage.Duration.Round(time.Millisecond).String()}
		}
		output.PrintTable(headers, rows)
	}

	// Cleanup
	if len(result.Cleanup.Unpinned) > 0 || len(result.Cleanup.Failed) > 0 {
		cleanupFields := []Field{
			{Label: "Unpinned", Value: fmt.Sprintf("%d", len(result.Cleanup.Unpinned))},
			{Label: "Failed", Value: fmt.Sprintf("%d", len(result.Cleanup.Failed))},
			{Label: "Duration", Value: result.Cleanup.Duration.Round(time.Millisecond).String()},
		}
		output.PrintFields(FieldGroup{Title: "Cleanup", Fields: cleanupFields, PadTop: 1})
	}

	// Summary
	summaryFields := []Field{
		{Label: "Total", Value: result.Summary.TotalDuration.Round(time.Millisecond).String()},
		{Label: "Upload", Value: result.Summary.UploadDuration.Round(time.Millisecond).String()},
		{Label: "Cleanup", Value: result.Summary.CleanupDuration.Round(time.Millisecond).String()},
	}
	output.PrintFields(FieldGroup{Title: "Summary", Fields: summaryFields, PadTop: 1})

	// Stage breakdown table: only for multi-iteration runs where aggregates are meaningful
	if multiIter && len(result.Summary.Stages) > 0 {
		headers := []string{"Stage", "Avg", "Min", "Max", "Median"}
		var rows [][]string

		// Ordered stages first
		stageOrder := []string{"generate", "upload", "queued", "processing"}
		seen := make(map[string]bool)
		for _, name := range stageOrder {
			stats, ok := result.Summary.Stages[name]
			if !ok {
				continue
			}
			seen[name] = true
			rows = append(rows, stageStatsRow(name, stats, true))
		}

		// Any stages not in the predefined order
		for name, stats := range result.Summary.Stages {
			if !seen[name] {
				rows = append(rows, stageStatsRow(name, stats, true))
			}
		}

		output.PrintTable(headers, rows)
	}
}

// stageStatsRow builds a table row for a stage's aggregate statistics.
func stageStatsRow(name string, stats StageStats, multiIter bool) []string {
	if multiIter {
		return []string{
			name,
			stats.Avg.Round(time.Millisecond).String(),
			stats.Min.Round(time.Millisecond).String(),
			stats.Max.Round(time.Millisecond).String(),
			stats.Median.Round(time.Millisecond).String(),
		}
	}
	return []string{name, stats.Avg.Round(time.Millisecond).String()}
}

// newBenchError creates a BenchError from an error, extracting structured detail
// when the error is an HTTPError.
func newBenchError(err error) *BenchError {
	benchErr := &BenchError{
		Message: strings.TrimSpace(err.Error()),
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		benchErr.Message = "upload failed"
		benchErr.Detail = map[string]any{
			"http_status": httpErr.StatusCode,
			"body":        httpErr.Body,
		}
	}

	return benchErr
}

// isUnrecoverableError returns true for errors that should not be retried
// (e.g., auth failures, permission errors).
func isUnrecoverableError(err error) bool {
	return errors.Is(err, portalsdk.ErrUnauthorized) || errors.Is(err, portalsdk.ErrForbidden)
}
