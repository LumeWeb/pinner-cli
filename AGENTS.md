# AGENTS.md
This file provides guidance to various AI agents when working with code in this repository.

## Common Commands

### Building
```bash
# Build with version info (recommended)
make build

# Or install to $GOPATH/bin
make install

# Build without make
go build -o pinner ./cmd/pinner

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o pinner-linux-amd64 ./cmd/pinner
GOOS=darwin GOARCH=arm64 go build -o pinner-darwin-arm64 ./cmd/pinner
GOOS=windows GOARCH=amd64 go build -o pinner-windows-amd64.exe ./cmd/pinner
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

The project uses [mockery](https://github.com/vektra/mockery) for generating mocks. See the [official documentation](https://vektra.github.io/mockery/latest/installation/) for installation instructions.

#### Usage

```bash
# Generate all mocks
mockery

# Generate mocks for specific interfaces
mockery --name=PinningService
mockery --name=UploadService
mockery --name=AuthService
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
Pinner.xyz CLI is a Go-based command-line tool for managing IPFS content via the Pinner.xyz service. Beyond pinning, it supports website hosting, DNS management, IPNS publishing, downloading, benchmarking, and admin operations. It uses a layered architecture with clear separation of concerns between CLI presentation, business logic, and external service integration.

### Core Directories

- **`cmd/pinner/`**: Entry point for the CLI application. Minimal main.go that delegates to `pkg/cli.Run()`.

- **`pkg/cli/`**: Primary CLI command implementations and orchestration logic.
  - Contains all command handlers: `auth.go`, `account.go`, `account_api_keys.go`, `register.go`, `confirm_email.go`, `upload.go`, `download.go`, `pin.go`, `pins.go` + `pins_*.go` (add/ls/rm/status/update), `list.go`, `status.go`, `unpin.go`, `unpin_all.go`, `metadata.go` (removed command), `meta.go` (metadata flag helpers), `operations.go`, `dns.go`, `ipns.go`, `websites.go`, `bench.go`, `config.go`, `doctor.go`, `setup.go`, `admin.go`, `version.go`, `root.go`
  - Defines service interfaces: `PinningService`, `StatusService`, `UploadService`, `AuthService`, `DownloadService`, `BenchService`, `OperationsService`, `DNSService`, `IPNSService`, `WebsitesService`, `QuotaAdminService`, `BillingAdminService`, `WebsiteAdminService`, `AdminTokenProvider`
  - Output formatting system with human-readable and JSON modes
  - Global flags and command registration in `flags.go` and `register.go`
  - Shell completion support for bash, zsh, fish, and PowerShell
  - Generic wizard framework in `wizard/` using Go generics (`Step[S any]`, `Run[S]()`)
  - Structured error types: `BenchError`, `HTTPError`, `FormatError`

- **`pkg/cli/internal/`**: Internal implementations of service interfaces.
  - `PinningClient` interface wraps boxo's remote pinning client
  - `BoxoPinningClient` provides the actual implementation with retry logic
  - HTTP client with configurable retry behavior

- **`pkg/internal/mcp/`**: MCP adapter for exposing the CLI as a Model Context Protocol server.
  - `MCPCommand()` returns a `*cli.Command` that serves the command tree over stdio
  - `adapter.go` - in-process command execution and MCP command registration
  - `catalog.go` - `ToolCatalog` with progressive disclosure
  - `schema.go` - CLI flag to JSON Schema conversion
  - `sdk_official.go` - official MCP SDK server, transport, and descriptor registration
  - `session.go` - FSM-based wizard sessions with TTL (`DefaultSessionTTL = 30m`, `DefaultMaxSessions = 100`)
  - `wizard.go` - website and setup wizard MCP tools (`websites_wizard_start`, `websites_wizard_step`, `setup_wizard_start`, `setup_wizard_step`)
  - `resources.go` - `pinner://` resource handlers (`account/status`, `websites/{domain}/dns-requirements`, `websites/{id}/validation-status`, `wizard/{session_id}/state`)
  - `prompts.go` - MCP prompt templates (`website-onboarding`, `setup`)
  - `interfaces.go` - breaks import cycles between `pkg/cli` and `pkg/internal/mcp`

- **`pkg/internal/`**: Internal shared utilities.
  - `car.go` - CAR file root reading (`GetCarRoots`)
  - `io/stdinfs.go` - Implements `fs.FS` for stdin (pipe) input

- **`pkg/config/`**: Configuration management.
  - Extends `go.lumeweb.com/configmanager` for CLI-specific config
  - Default config location: `~/.config/pinner/config.yaml`
  - Config keys: `auth_token`, `base_endpoint`, `max_retries`, `memory_limit`, `secure`, `gateway_endpoint`
  - Methods for managing auth tokens, endpoints, retries, and secure flags

- **`build/`**: Build-time information.
  - `build.go` - Variables populated at build time via ldflags
  - `build_info.go` - BuildInfo interface and Info struct for version tracking

### Key Interfaces and Patterns

**Service Layer Pattern**: Each major feature has a service interface with default implementation:
- `PinningService` - Pin/unpin/list/status/batch/update operations on existing CIDs
- `StatusService` - Extended status checking with operation tracking
- `UploadService` - Upload files/directories to IPFS
- `AuthService` - Authentication, registration, OTP, token management
- `DownloadService` - Download, cat, and directory listing from IPFS
- `BenchService` - Benchmark upload and pinning performance
- `OperationsService` - List and inspect account operations
- `DNSService` - DNS zone and record CRUD operations
- `IPNSService` - IPNS key management, publish, republish, resolve
- `WebsitesService` - Website CRUD, SSL status, config
- `QuotaAdminService` - Admin quota plans, allowances, user configs, stats
- `BillingAdminService` - Admin credits, price lines, pricing plans, subscribers
- `WebsiteAdminService` - Admin website block/unblock
- `Manager` - Configuration management

These interfaces enable dependency injection for testing. Factory functions create service instances (`defaultPinningServiceFactory`, `defaultUploadServiceFactory`, etc.).

**Output Formatting**: The `Output` interface provides methods for formatted output:
- `Print`, `Printf`, `Printfln` for text output
- `PrintJSON` for structured data
- `PrintTable`, `PrintList` for tabular/list data
- `MaskSensitive` to hide tokens/passwords
- `Watch` for monitoring long-running operations

Two implementations: `humanFormatter` and `jsonFormatter`, selected based on global `--json` flag.

**MCP Adapter Pattern**: The MCP adapter (`pkg/internal/mcp/`) exposes the CLI command tree as an MCP server over stdio. Key design decisions:
- In-process execution: tool invocations run the command tree directly, no subprocess fork
- Progressive disclosure: only `search_tools`, `describe_tool`, and `invoke_tool` are visible in `tools/list`; the full catalog is internal
- Automatic `--agent` injection: all tool invocations receive `--agent`, forcing JSON output and disabling interactive prompts
- Wizard sessions: FSM-based with `SessionStore` enforcing TTL (`30m`) and max capacity (`100`)
- Resources: live data exposed via `pinner://` URIs (`account/status`, `websites/{domain}/dns-requirements`, etc.)
- Prompts: deterministic workflow guides (`website-onboarding`, `setup`) with optional pre-filled arguments
- Import cycle broken via `interfaces.go` — `WizardDepsFactory` and `ResourceProvidersFactory` are defined in `pkg/internal/mcp`, concrete implementations live in `pkg/cli`

**CAR Generation Flow**:
1. Upload uses IPFS boxo libraries for DAG building and CAR format handling
2. CAR root reading via `GetCarRoots` in `pkg/internal/car.go`
3. Memory usage limited by `--memory-limit` flag (default: 100MB)

**CLI Command Pattern**:
- Each command has a `newXxxCommand()` function returning a `cli.Command`
- Commands use `commandGetter` interface for flag access (enables testing)
- Command handlers accept factory functions for dependency injection
- `cliCommandWrapper` adapts `cli.Command` to `commandGetter`
- The `pins` command group (`pins add/rm/ls/status/update`) is the canonical interface; `pin`, `unpin`, `list`, `status` are first-class root shortcuts
- Help categories (Setup, Content, Pinning, Management, System, Admin) are set via urfave/cli `Category` field
- `mcp` command is registered in `root.go` via `mcpadapter.MCPCommand()` with wizard and resource factories

**Mockery Configuration**:
- Mocks are generated using mockery
- Configuration in `.mockery.yaml` defines which interfaces to mock
- Mocks are placed alongside source files or in `mocks/` subdirectories

**Build Information Injection**:
- Version, commit, and build time are injected at build time via ldflags
- `build.Default` provides access to build info throughout the application
- Used in diagnostics (`pinner doctor`) and version display

**Command Structure**:
- `pins` is the canonical command group with subcommands: `add`, `rm`, `ls`, `status`, `update`
- Root shortcuts (`pin`, `unpin`, `list`, `status`) delegate to pins subcommands — first-class, no deprecation
- `metadata` command removed; `pins update` replaces it (hidden error command with suggestion)
- Upload and pins add wait by default for pinning to complete; `--no-wait` to detach
- `--meta key=value` flag on pins add and upload for metadata at pin creation time
- `--force` consolidates `--confirm`/`--yes` as the primary skip-confirmation flag

### Global Flags
All commands support these global flags:
- `--json` - Output JSON instead of human-readable text
- `--verbose, -v` - Show detailed output
- `--quiet, -q` - Suppress non-error output
- `--unmask` - Show sensitive data (tokens, passwords) unmasked
- `--auth-token` - Override auth token (also reads from `PINNER_AUTH_TOKEN` env var)
- `--secure` - Use HTTPS instead of HTTP for endpoints (default: true, env: `PINNER_SECURE`)

### Dependencies
- `github.com/urfave/cli/v3` - CLI framework
- `github.com/ipfs/boxo` - IPFS libraries (pinning, DAG, blockstore)
- `go.lumeweb.com/configmanager` - Configuration management
- `go.lumeweb.com/portal-sdk` - Portal SDK (local replace in go.mod)
- `github.com/pterm/pterm` - Terminal UI for setup wizard
- `github.com/stretchr/testify` - Testing framework
- `github.com/vektra/mockery` - Mock generation
