# Pinner.xyz CLI

[![Go Version](https://img.shields.io/badge/Go-1.25.5-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A minimal, developer-focused CLI tool for pinning content to IPFS via the Pinner.xyz service.

## Principles

- **KISS**: Simple command structure, easy to discover
- **Progressive disclosure**: Simple commands first, advanced via flags
- **Sensible defaults**: Don't require flags for common operations
- **JSON-first for automation**: All commands support `--json`

## Installation

### From Source

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

### From Pre-built Binaries

Download the latest release from the [releases page](https://github.com/lumeweb/pinner-cli/releases).

## Quick Start

```bash
# Authenticate with the Pinner.xyz service
pinner auth

# Upload a file
pinner upload file.png

# Pin an existing CID
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# List your pins
pinner list

# Check pin status
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4
```

## Configuration

Config file location: `~/.config/lume/pinner.yaml`

```yaml
auth_token: your-jwt-token
api_endpoint: https://api.lumeweb.com
max_retries: 3
secure: true
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `PINNER_AUTH_TOKEN` | Override auth token for the current session |

## Commands

### Authentication

```bash
# Authenticate with JWT token
pinner auth <jwt-token>

# Or authenticate interactively
pinner auth

# Show current account info
pinner account

# Confirm email (if required)
pinner confirm-email <token>
```

### Upload

Upload files or directories to IPFS and pin them to the Pinner.xyz service.

```bash
# Upload a file
pinner upload file.png

# Upload a directory
pinner upload ./my-folder

# Upload with custom name
pinner upload file.png --name "My File"

# Upload and wait for completion
pinner upload file.png --wait

# Upload from stdin (pipe)
cat file.txt | pinner upload --name "piped-file"

# Upload multiple files
pinner upload file1.png file2.jpg file3.pdf

# JSON output for automation
pinner upload file.png --json
```

### Pin

Pin an existing CID to the Pinner.xyz service.

```bash
# Pin existing CID
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Pin with custom name
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --name "My Pin"

# Pin and wait for completion
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --wait

# Pin with metadata
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --name "Backup" --set "owner=derrick" --set "type=backup"
```

### List

List your pinned content.

```bash
# List recent pins
pinner list

# Filter by name
pinner list --name "My Pin"

# Limit results
pinner list --limit 20

# JSON output
pinner list --json
```

### Status

Check the status of a pinned CID.

```bash
# Check pin status
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Watch until settled
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --watch

# JSON output
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --json
```

### Unpin

Remove a CID from pinning.

```bash
# Unpin (prompts for confirmation)
pinner unpin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Skip confirmation
pinner unpin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --confirm

# JSON output
pinner unpin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --json
```

### Metadata

Manage metadata for pinned content.

```bash
# Set metadata
pinner metadata bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --set "owner=derrick" --set "type=backup"

# Clear all metadata
pinner metadata bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --clear

# Show current metadata
pinner metadata bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# JSON output
pinner metadata bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --json
```

### Config

Manage CLI configuration.

```bash
# Show all config
pinner config

# Get specific value
pinner config get auth_token

# Set value
pinner config set api_endpoint https://custom.endpoint.com

# Reset to defaults
pinner config reset
```

### Setup

Interactive setup wizard for initial configuration.

```bash
# Run setup wizard
pinner setup

# Re-run setup (overwrites existing config)
pinner setup --force
```

The setup wizard guides you through:
- Authentication with the Pinner.xyz service
- Configuration of API endpoints
- Shell completion setup
- Configuration file creation

### Doctor

Diagnose configuration and environment issues.

```bash
# Run diagnostics
pinner doctor

# Check specific components
pinner doctor --config
pinner doctor --auth
pinner doctor --completion
```

## Shell Completion

Enable tab-completion for your shell to discover commands and flags faster.

### Bash

```bash
# Add to ~/.bashrc
echo 'source <(pinner completion bash)' >> ~/.bashrc

# Or source immediately
source <(pinner completion bash)
```

### Zsh

```bash
# Add to ~/.zshrc
echo 'source <(pinner completion zsh)' >> ~/.zshrc

# Or source immediately
source <(pinner completion zsh)
```

### Fish

```bash
# Save completion file
mkdir -p ~/.config/fish/completions
pinner completion fish > ~/.config/fish/completions/pinner.fish
```

### PowerShell

```powershell
# Add to your PowerShell profile
pinner completion pwsh | Out-File -Append $PROFILE

# Or run immediately
pinner completion pwsh | Invoke-Expression
```

**Tip**: Use `pinner setup` to automatically configure shell completion.

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output JSON instead of human-readable text |
| `--verbose, -v` | Show detailed output |
| `--quiet, -q` | Suppress non-error output |
| `--unmask` | Show sensitive data (tokens, passwords) unmasked |
| `--auth-token` | Override auth token (also reads from `PINNER_AUTH_TOKEN` env var) |
| `--secure` | Use HTTPS instead of HTTP for endpoints |
| `--help, -h` | Show help |

## Architecture

### Project Structure

```
pinner-cli/
├── cmd/pinner/              # CLI entry point
│   └── main.go              # Main application entry
├── pkg/
│   ├── cli/                 # Primary CLI command implementations
│   │   ├── auth.go          # Authentication commands
│   │   ├── upload.go        # Upload commands
│   │   ├── pin.go           # Pin commands
│   │   ├── list.go          # List commands
│   │   ├── status.go        # Status checking
│   │   ├── unpin.go         # Unpin commands
│   │   ├── metadata.go      # Metadata management
│   │   ├── config.go        # Configuration commands
│   │   ├── doctor.go        # Diagnostics
│   │   ├── setup.go         # Setup wizard
│   │   ├── flags.go         # Global flags definition
│   │   ├── register.go      # Command registration
│   │   ├── output.go        # Output formatting
│   │   ├── progress.go      # Progress display
│   │   ├── auth_service.go  # Auth service interface
│   │   ├── pinning_service.go # Pinning service interface
│   │   ├── upload_service.go # Upload service interface
│   │   └── internal/        # Internal implementations
│   ├── upload/              # CAR file generation and upload
│   │   ├── car.go           # CAR generation functions
│   │   ├── car_blockstore.go # LRU blockstore
│   │   ├── car_dir_builder.go # Two-pass CAR generation
│   │   └── unixfs_generator.go # UnixFS DAG node generation
│   ├── config/              # Configuration management
│   │   └── config.go        # Extends configmanager
│   └── internal/io/         # Filesystem abstractions
│       ├── stdinfs.go       # stdin fs implementation
│       └── singlefilefs.go  # Single file fs implementation
├── build/                   # Build-time information
│   ├── build.go             # Build variables
│   └── build_info.go        # BuildInfo interface and struct
├── mocks/                   # Generated mocks
├── .mockery.yaml            # Mockery configuration
├── go.mod                   # Go module definition
└── README.md                # This file
```

### Key Components

**Service Layer Pattern**: Each major feature has a service interface with default implementation:
- `PinningService` - Pin/unpin/list/status operations
- `UploadService` - Upload files/directories to IPFS
- `AuthService` - Authentication and account operations
- `Manager` - Configuration management

**Output Formatting**: The `Output` interface provides methods for formatted output with both human-readable and JSON implementations.

**CAR Generation Flow**: Files are converted to CARv1 format using a two-pass generation process with LRU blockstore for memory efficiency.

## Development

### Prerequisites

- Go 1.25.5 or later
- Make (optional, for building)

### Running Tests

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

```bash
# Generate all mocks
mockery

# Generate mocks for specific interfaces
mockery --name=PinningService
mockery --name=UploadService
mockery --name=AuthService
```

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

### Running the CLI

```bash
# Run from source
go run ./cmd/pinner

# Or use built binary
./pinner <command>
```

## Troubleshooting

### Common Issues

**Authentication token not found**
- Ensure you've run `pinner auth` or set `PINNER_AUTH_TOKEN` environment variable
- Check that `~/.config/lume/pinner.yaml` exists and contains a valid `auth_token`

**Upload fails with timeout**
- Increase retries in config: `pinner config set max_retries 5`
- Check your network connection to the API endpoint

**Shell completion not working**
- Run `pinner doctor` to check completion status
- Ensure your shell loads the completion script (check your shell config file)
- Try re-running `pinner setup` to reconfigure completion

### Getting Help

```bash
# Show help for a specific command
pinner upload --help

# Run diagnostics
pinner doctor

# Enable verbose output for debugging
pinner upload file.png --verbose
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.


## Acknowledgments

- Built with [urfave/cli/v3](https://github.com/urfave/cli) for CLI framework
- Uses [ipfs/boxo](https://github.com/ipfs/boxo) for IPFS libraries
- CAR generation powered by [go-car/v2](https://github.com/ipld/go-car)
