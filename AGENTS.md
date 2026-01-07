# AGENTS.md
This file provides guidance to various AI agents when working with code in this repository.

## Common Commands

### Building
```bash
# Build for current platform
go build -o pinner ./cmd/pinner

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o pinner-linux-amd64 ./cmd/pinner
GOOS=darwin GOARCH=arm64 go build -o pinner-darwin-arm64 ./cmd/pinner
GOOS=windows GOARCH=amd64 go build -o pinner-windows-amd64.exe ./cmd/pinner

# Build with version info
go build -ldflags="-X 'build.Version=1.0.0' -X 'build.GitCommit=abc123'" -o pinner ./cmd/pinner
```

### Testing
```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run tests for a specific package
go test ./pkg/cli

# Run tests with verbose output
go test -v ./...

# Run specific test functions
go test ./pkg/cli -run TestUpload
```

### Mock Generation
```bash
# Generate all mocks (mockery is pre-installed at $HOME/go/bin/mockery)
$HOME/go/bin/mockery --all

# Generate mocks for specific interfaces
$HOME/go/bin/mockery --name=PinningClient
```

### Running the CLI
```bash
# Run from source
go run ./cmd/pinner

# Or use built binary
./pinner <command>
```

## High-Level Architecture

### Project Overview
Pinner.xyz CLI is a Go-based command-line tool for pinning content to IPFS via the Pinner.xyz service. It uses a layered architecture with clear separation of concerns between CLI presentation, business logic, and external service integration.

### Core Directories

- **`cmd/pinner/`**: Entry point for the CLI application. Minimal main.go that delegates to `pkg/cli.Run()`.

- **`pkg/cli/`**: Primary CLI command implementations and orchestration logic.
  - Contains all command handlers: `auth.go`, `upload.go`, `pin.go`, `list.go`, `status.go`, `unpin.go`, `metadata.go`, `config.go`, `doctor.go`, `setup.go`
  - Defines service interfaces: `PinningService`, `UploadService`, `AuthService`
  - Output formatting system with human-readable and JSON modes
  - Global flags and command registration in `flags.go` and `register.go`
  - Shell completion support for bash, zsh, fish, and PowerShell

- **`pkg/cli/internal/`**: Internal implementations of service interfaces.
  - `PinningClient` interface wraps boxo's remote pinning client
  - `PinningClientBoxo` provides the actual implementation with retry logic
  - HTTP client with configurable retry behavior

- **`pkg/upload/`**: CAR file generation and upload logic.
  - `car.go` - Main CAR generation functions: `StreamCAR`, `StreamCARWithSize`
  - `car_blockstore.go` - LRU blockstore for memory-constrained operations
  - `car_dir_builder.go` - Two-pass CAR generation (summary then write)
  - `unixfs_generator.go` - UnixFS DAG node generation from files
  - Uses IPFS boxo libraries for DAG building and CAR format handling

- **`pkg/config/`**: Configuration management.
  - Extends `go.lumeweb.com/configmanager` for CLI-specific config
  - Default config location: `~/.config/lume/pinner.yaml`
  - Methods for managing auth tokens, endpoints, retries, and secure flags

- **`pkg/internal/io/`**: Filesystem abstractions.
  - `stdinfs.go` - Implements `fs.FS` for stdin (pipe) input
  - `singlefilefs.go` - Implements `fs.FS` for single files
  - Enables uniform handling of files, directories, and piped data

- **`build/`**: Build-time information.
  - `build.go` - Variables populated at build time via ldflags
  - `build_info.go` - BuildInfo interface and Info struct for version tracking

### Key Interfaces and Patterns

**Service Layer Pattern**: Each major feature has a service interface with default implementation:
- `PinningService` - Pin/unpin/list/status operations on existing CIDs
- `UploadService` - Upload files/directories to IPFS
- `AuthService` - Authentication and account operations
- `Manager` - Configuration management

These interfaces enable dependency injection for testing. Factory functions create service instances (`defaultPinningServiceFactory`, `defaultUploadServiceFactory`, etc.).

**Output Formatting**: The `Output` interface provides methods for formatted output:
- `Print`, `Printf`, `Printfln` for text output
- `PrintJSON` for structured data
- `PrintTable`, `PrintList` for tabular/list data
- `MaskSensitive` to hide tokens/passwords
- `Watch` for monitoring long-running operations

Two implementations: `humanFormatter` and `jsonFormatter`, selected based on global `--json` flag.

**CAR Generation Flow**:
1. Input is resolved to an `fs.FS` (file, directory, or stdin)
2. `UnixFSNodeGenerator` creates DAG nodes from the filesystem
3. `CARBuilder` performs two-pass generation (summary then write)
4. LRU blockstore limits memory usage during generation
5. Result is a CARv1 file with root CID

**CLI Command Pattern**:
- Each command has a `newXxxCommand()` function returning a `cli.Command`
- Commands use `commandGetter` interface for flag access (enables testing)
- Command handlers accept factory functions for dependency injection
- `cliCommandWrapper` adapts `cli.Command` to `commandGetter`

**Mockery Configuration**:
- Mocks are generated using mockery (pre-installed at `$HOME/go/bin/mockery`)
- Configuration in `.mockery.yaml` defines which interfaces to mock
- Mocks are placed alongside source files or in `mocks/` subdirectories
- Never attempt to reinstall mockery via `go install`

**Build Information Injection**:
- Version, commit, and build time are injected at build time via ldflags
- `build.Default` provides access to build info throughout the application
- Used in diagnostics (`pinner doctor`) and version display

### Global Flags
All commands support these global flags:
- `--json` - Output JSON instead of human-readable text
- `--verbose, -v` - Show detailed output
- `--quiet, -q` - Suppress non-error output
- `--unmask` - Show sensitive data (tokens, passwords) unmasked
- `--auth-token` - Override auth token (also reads from `PINNER_AUTH_TOKEN` env var)
- `--secure` - Use HTTPS instead of HTTP for endpoints

### Dependencies
- `github.com/urfave/cli/v3` - CLI framework
- `github.com/ipfs/boxo` - IPFS libraries (pinning, DAG, blockstore)
- `go.lumeweb.com/configmanager` - Configuration management
- `go.lumeweb.com/portal-sdk` - Portal SDK (local replace in go.mod)
- `github.com/pterm/pterm` - Terminal UI for setup wizard
- `github.com/stretchr/testify` - Testing framework
- `github.com/vektra/mockery` - Mock generation (pre-installed)
