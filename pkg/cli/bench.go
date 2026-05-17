package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/go-units"
	"github.com/urfave/cli/v3"
)

// Bench flag constants
const (
	FlagBenchSize         = "size"
	FlagBenchFiles        = "files"
	FlagBenchDepth        = "depth"
	FlagBenchIterations   = "iterations"
	FlagBenchNoCleanup    = "no-cleanup"
	FlagBenchPollInterval = "poll-interval"
)

func newBenchCommand() *cli.Command {
	return &cli.Command{
		Name:  "bench",
		Usage: "Benchmark upload and pinning performance",
		Description: `Run a benchmark by uploading random data (or a specified path) and
tracking each stage of the pipeline: generate, upload, queued, pinning, pinned.

After the benchmark completes, all uploaded pins are automatically removed
(unless --no-cleanup is specified).

Examples:
  # Quick benchmark: 1MB random file
  pinner bench

  # Benchmark with real data
  pinner bench ./my-project

  # 10 iterations of 100MB upload for avg/min/max stats
  pinner bench --size 100MB --iterations 10

  # Simulate folder upload: 3 levels deep, 20 files, 50MB total
  pinner bench --size 50MB --files 20 --depth 3

  # Parallel uploads (3 concurrent)
  pinner bench --size 10MB --iterations 9 --parallel 3

  # Keep the pins around for inspection
  pinner bench --no-cleanup

  # Faster polling for stage transitions
  pinner bench --poll-interval 250ms

The output includes per-iteration stage timing and a summary with
min/max/avg/median statistics across all iterations.`,
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			BenchSizeFlag(),
			BenchFilesFlag(),
			BenchDepthFlag(),
			BenchIterationsFlag(),
			ParallelFlag(),
			BenchNoCleanupFlag(),
			BenchPollIntervalFlag(),
			MemoryLimitFlag(),
			ChunkSizeFlag(),
			ChunkerFlag(),
			MaxLinksFlag(),
			DryRunFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return bench(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory)
		},
	}
}

// BenchSizeFlag returns a flag for setting the random data size.
func BenchSizeFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  FlagBenchSize,
		Usage: "Size of random data to generate (e.g., 1MB, 100MB, 1GB). Only used when no path is given.",
		Value: "1MB",
	}
}

// BenchFilesFlag returns a flag for setting the number of random files.
func BenchFilesFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagBenchFiles,
		Usage: "Number of random files to generate",
		Value: 1,
	}
}

// BenchDepthFlag returns a flag for setting the directory nesting depth.
func BenchDepthFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagBenchDepth,
		Usage: "Create nested folder structure N levels deep with random files",
		Value: 0,
	}
}

// BenchIterationsFlag returns a flag for setting the number of iterations.
func BenchIterationsFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:  FlagBenchIterations,
		Usage: "Number of upload iterations (each generates fresh random data)",
		Value: 1,
	}
}

// BenchNoCleanupFlag returns a flag to skip cleanup after benchmark.
func BenchNoCleanupFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  FlagBenchNoCleanup,
		Usage: "Skip unpin after benchmark (pins remain on the server)",
	}
}

// BenchPollIntervalFlag returns a flag for setting the stage polling interval.
func BenchPollIntervalFlag() *cli.DurationFlag {
	return &cli.DurationFlag{
		Name:  FlagBenchPollInterval,
		Usage: "Interval for polling pin status during stage tracking",
		Value: 500 * time.Millisecond,
	}
}

// benchCommandGetter defines the interface for getting bench command flags.
type benchCommandGetter interface {
	String(name string) string
	Int(name string) int
	Int64(name string) int64
	Bool(name string) bool
	Uint64(name string) uint64
	Duration(name string) time.Duration
	Args() cli.Args
}

func bench(ctx context.Context, cmd benchCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	// Parse size flag
	sizeStr := cmd.String(FlagBenchSize)
	sizeBytes, err := units.RAMInBytes(sizeStr)
	if err != nil {
		return fmt.Errorf("invalid size %q: %w. Use format like 1MB, 100MB, 1GB", sizeStr, err)
	}

	// Set memory limit from flag (overrides config if provided)
	memoryLimit := cmd.Uint64(FlagMemoryLimit)
	if memoryLimit == 0 {
		memoryLimit = cfgMgr.Config().MemoryLimit
	}

	// Create services
	var pinningService PinningService
	if c, ok := cmd.(*cliCommandWrapper); ok {
		secure := GetSecureSetting(c.Command, cfgMgr)
		authToken := GetAuthToken(c.Command, cfgMgr)
		if authToken != "" {
			pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
		} else {
			pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure))
		}
	} else {
		pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointSecure())
	}

	authService := NewAuthService(cfgMgr, output, cfgMgr.Config().GetAccountEndpointSecure())

	var svcOpts []UploadServiceOption
	svcOpts = append(svcOpts, WithMemoryLimit(memoryLimit), WithUploadAuthService(authService))

	if chunkSize := cmd.Int64(FlagChunkSize); chunkSize > 0 {
		svcOpts = append(svcOpts, WithChunkSize(chunkSize))
	}
	if chunker := cmd.String(FlagChunker); chunker != "" {
		strategy, err := parseChunkerStrategy(chunker)
		if err != nil {
			return err
		}
		svcOpts = append(svcOpts, WithChunkerStrategy(strategy))
	}
	if maxLinks := cmd.Int(FlagMaxLinks); maxLinks > 0 {
		svcOpts = append(svcOpts, WithMaxLinks(maxLinks))
	}

	uploadService := NewUploadService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpoint(), svcOpts...)

	accountClient, err := authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate for operation polling: %w", err)
	}

	opts := BenchOptions{
		SizeBytes:       sizeBytes,
		Files:           cmd.Int(FlagBenchFiles),
		Depth:           cmd.Int(FlagBenchDepth),
		Iterations:      cmd.Int(FlagBenchIterations),
		Parallel:        cmd.Int(FlagParallel),
		NoCleanup:       cmd.Bool(FlagBenchNoCleanup),
		PollInterval:    cmd.Duration(FlagBenchPollInterval),
		MemoryLimit:     memoryLimit,
		DryRun:          cmd.Bool(FlagDryRun),
		Path:            cmd.Args().First(),
		ChunkSize:       cmd.Int64(FlagChunkSize),
		ChunkerStrategy: cmd.String(FlagChunker),
		MaxLinks:        cmd.Int(FlagMaxLinks),
	}

	// Validate options
	if opts.SizeBytes <= 0 && opts.Path == "" {
		return fmt.Errorf("size must be positive or provide a path")
	}
	if opts.Files < 1 {
		return fmt.Errorf("files must be at least 1")
	}
	if opts.Iterations < 1 {
		return fmt.Errorf("iterations must be at least 1")
	}
	if opts.Depth < 0 {
		return fmt.Errorf("depth must be non-negative")
	}

	// Dry run
	if opts.DryRun {
		options := make(map[string]string)
		if opts.Path != "" {
			options[DryRunOptionInputType] = "path"
			options[DryRunOptionPath] = opts.Path
		} else {
			options[DryRunOptionInputType] = "random"
			options["Size"] = units.HumanSize(float64(opts.SizeBytes))
			options["Files"] = fmt.Sprintf("%d", opts.Files)
			options["Depth"] = fmt.Sprintf("%d", opts.Depth)
		}
		options["Iterations"] = fmt.Sprintf("%d", opts.Iterations)
		if opts.Parallel > 1 {
			options[DryRunOptionParallel] = fmt.Sprintf("%d", opts.Parallel)
		}
		if opts.NoCleanup {
			options["Cleanup"] = "disabled"
		}
		options[DryRunOptionMemoryLimit] = fmt.Sprintf("%d MB", memoryLimit)
		options["Poll interval"] = opts.PollInterval.String()
		if opts.ChunkSize > 0 {
			options[DryRunOptionChunkSize] = fmt.Sprintf("%d bytes", opts.ChunkSize)
		}
		if opts.ChunkerStrategy != "" {
			options[DryRunOptionChunker] = opts.ChunkerStrategy
		}
		if opts.MaxLinks > 0 {
			options[DryRunOptionMaxLinks] = fmt.Sprintf("%d", opts.MaxLinks)
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "benchmark",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointSecure(),
			Options:   options,
		})
		return nil
	}

	benchService := NewBenchService(cfgMgr, output, uploadService, pinningService, accountClient)

	result, err := benchService.Run(ctx, opts)
	if err != nil {
		return err
	}

	// Output results
	if output.IsJSON() {
		if err := output.PrintJSON(result); err != nil {
			return fmt.Errorf("failed to output results: %w", err)
		}
	} else {
		formatBenchResult(output, result)
	}

	return nil
}
