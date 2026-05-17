package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/go-units"
	"github.com/urfave/cli/v3"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	internalio "go.lumeweb.com/pinner-cli/pkg/internal/io"
)

func newUploadCommand() *cli.Command {
	return &cli.Command{
		Name:  "upload",
		Usage: "Upload files/directories to IPFS",
		Description: `Upload files or directories to IPFS via the Pinner.xyz service.
Content is converted to CAR format before uploading.

Examples:
  pinner upload myfile.txt
  pinner upload myfile.txt --name "my document"
  pinner upload myfile.txt --wait
  pinner upload /path/to/directory --name "project files"
  pinner upload largefile.zip --memory-limit 500 --wait
  pinner upload myfile.txt --dry-run

  # Upload from stdin (pipe)
  cat myfile.txt | pinner upload --name "my file"
  echo "hello world" | pinner upload --name "greeting"
  curl -s https://example.com/data | pinner upload --name "downloaded"

The output includes:
  - CID: Content identifier for your uploaded content
  - Gateway URL: Public URL to access your content
  - Size: File size in human-readable format
  - Time: Upload duration`,
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			NameFlag("Custom name for the pin"),
			WaitFlag(),
			MemoryLimitFlag(),
			DryRunFlag(),
			ChunkSizeFlag(),
			ChunkerFlag(),
			MaxLinksFlag(),
		},
		Metadata: WithTutorial(1, "Upload and pin a file", "pinner upload myfile.txt"),
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			return handleUpload(ctx, newCLICommandWrapper(c), output, defaultConfigManagerFactory, defaultUploadServiceFactory)
		},
	}
}

// UploadInput represents the resolved input source for an upload operation.
type UploadInput struct {
	Filesystem fs.FS
	Name       string
	closer     io.Closer
}

// Close releases resources held by the UploadInput, such as open file handles.
func (i *UploadInput) Close() error {
	if i.closer != nil {
		return i.closer.Close()
	}
	return nil
}

// resolveUploadInput resolves the upload input source (file, directory, or stdin).
// It detects if stdin is a pipe and creates an appropriate filesystem.
func resolveUploadInput(path string, name string) (*UploadInput, error) {
	if isStdinPipe() {
		// stdin mode: path is ignored or used as name
		if name == "" {
			name = "stdin"
		}
		filesystem, err := internalio.NewStdinFS(name)
		if err != nil {
			return nil, err
		}
		return &UploadInput{Filesystem: filesystem, Name: name}, nil
	}

	// file/dir mode
	if path == "" {
		return nil, fmt.Errorf("%w. Usage: pinner upload <path>", ErrPathRequired)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, WrapFileError("Cannot access file", path, err)
	}

	if fileInfo.IsDir() {
		return &UploadInput{Filesystem: os.DirFS(path), Name: name}, nil
	}

	// single file
	if name == "" {
		name = filepath.Base(path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	filesystem := contentfs.NewSingleFileFS(file, name)

	return &UploadInput{Filesystem: filesystem, Name: name, closer: file}, nil
}

// detectInputType returns a string describing the input type.
func detectInputType(path string) string {
	if isStdinPipe() {
		return "stdin (pipe)"
	}
	if path == "" {
		return "stdin"
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	if fileInfo.IsDir() {
		return "directory"
	}
	return "file"
}

// uploadCommandGetter defines the interface for getting upload command flags.
type uploadCommandGetter interface {
	Uint64(name string) uint64
	Int64(name string) int64
	Int(name string) int
	String(name string) string
	Bool(name string) bool
	Args() cli.Args
}

func handleUpload(ctx context.Context, cmd uploadCommandGetter, output Output, cfgMgrFactory ConfigManagerFactory, uploadServiceFactory UploadServiceFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return err
	}

	// Set memory limit from flag (overrides config if provided, runtime only)
	memoryLimit := cmd.Uint64(FlagMemoryLimit)
	if memoryLimit == 0 {
		memoryLimit = cfgMgr.Config().MemoryLimit
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

	uploadService := uploadServiceFactory(cfgMgr, output, svcOpts...)

	path := cmd.Args().First()
	name := cmd.String(FlagName)
	wait := cmd.Bool(FlagWait)
	dryRun := cmd.Bool(FlagDryRun)

	// Resolve input source (file/dir/stdin)
	input, err := resolveUploadInput(path, name)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()

	if dryRun {
		options := make(map[string]string)
		options[DryRunOptionInputType] = detectInputType(path)
		if path != "" {
			options[DryRunOptionPath] = path
		} else {
			options["Input"] = "stdin"
		}
		options[DryRunOptionName] = input.Name
		options[DryRunOptionMemoryLimit] = fmt.Sprintf("%d MB", memoryLimit)
		if wait {
			options[DryRunOptionWait] = "yes"
		}
		if chunkSize := cmd.Int64(FlagChunkSize); chunkSize > 0 {
			options[DryRunOptionChunkSize] = fmt.Sprintf("%d bytes", chunkSize)
		}
		if chunker := cmd.String(FlagChunker); chunker != "" {
			options[DryRunOptionChunker] = chunker
		}
		if maxLinks := cmd.Int(FlagMaxLinks); maxLinks > 0 {
			options[DryRunOptionMaxLinks] = fmt.Sprintf("%d", maxLinks)
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "upload operation",
			Endpoint:  cfgMgr.Config().GetIPFSEndpointSecure(),
			Options:   options,
		})
		return nil
	}

	result, err := uploadService.Upload(ctx, input.Filesystem, input.Name, wait)
	if err != nil {
		return err
	}

	if result != nil {
		gatewayURL := cfgMgr.Config().GetGatewayEndpointSecure() + result.CID
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"CID", result.CID},
				{"Gateway URL", gatewayURL},
				{"Size", humanReadableSize(result.Size)},
				{"Time", result.Duration.Round(time.Millisecond).String()},
			},
		})
	}

	return nil
}

func defaultUploadServiceFactory(cfgMgr config.Manager, output Output, opts ...UploadServiceOption) UploadService {
	return NewUploadService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpoint(), opts...)
}

func humanReadableSize(bytes int64) string {
	return units.HumanSize(float64(bytes))
}

func parseChunkerStrategy(name string) (ipfs.ChunkerStrategy, error) {
	switch name {
	case "balanced":
		return ipfs.BalancedLayout, nil
	case "trickle":
		return ipfs.TrickleLayout, nil
	default:
		return nil, fmt.Errorf("invalid chunker strategy %q: must be balanced or trickle", name)
	}
}
