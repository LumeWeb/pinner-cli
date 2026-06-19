# Pinner.xyz CLI

[![Go Version](https://img.shields.io/badge/Go-1.26.0-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A developer-focused CLI for pinning content to IPFS, managing websites, DNS, and IPNS via the Pinner.xyz service.

## Principles

- **KISS**: Simple command structure, easy to discover
- **Progressive disclosure**: Simple commands first, advanced via flags
- **Sensible defaults**: Don't require flags for common operations
- **JSON-first for automation**: All commands support `--json`

## Installation

### Universal Installer

**macOS / Linux:**

```bash
curl -fsSL https://get.pinner.xyz | sh
```

**Windows (PowerShell):**

```powershell
irm https://get.pinner.xyz/install.ps1 | iex
```

The installer detects your package manager and uses it automatically. On macOS it prefers Homebrew; on Linux it tries `dpkg` then `rpm` before falling back to a binary download. On Windows it tries winget, then Scoop, then falls back to a binary download. All methods verify SHA256 checksums.

### Other Install Methods

The universal installer handles most cases. Use these when you need more control.

| Method | Command |
|--------|---------|
| Homebrew | `brew install lumeweb/tap/pinner` |
| winget | `winget install Pinner.Cli` |
| Scoop | `scoop bucket add lumeweb https://github.com/LumeWeb/scoop-bucket` then `scoop install pinner` |
| Debian/Ubuntu | `sudo dpkg -i pinner-cli_VERSION_linux_amd64.deb` |
| Fedora/RHEL | `sudo rpm -i pinner-cli_VERSION_linux_amd64.rpm` |
| Go | `go install go.lumeweb.com/pinner-cli/cmd/pinner@latest` |
| Binary | Download from [GitHub Releases](https://github.com/LumeWeb/pinner-cli/releases) |

#### Installer Flags

**macOS / Linux** (`install.sh`):

| Flag | Description |
|------|-------------|
| `--system` | Install to `/usr/local/bin` (requires sudo if not writable) |
| `--bin-dir DIR` | Install to a custom directory |
| `--arch ARCH` | Override detected architecture (`amd64` or `arm64`) |
| `--base-url URL` | Override download base URL (for testing) |
| `--no-pkg` | Skip package manager detection, use binary install |
| `--uninstall` | Remove the Pinner CLI |
| `--debug` | Enable verbose output |

**Windows** (`install.ps1`):

| Flag | Description |
|------|-------------|
| `-System` | Install to Program Files (requires admin) |
| `-NoPkg` | Skip winget/scoop detection |
| `-CI` | Enable CI mode (also activated by `CI=true` env var) |
| `-Uninstall` | Remove the Pinner CLI |
| `-Debug` | Enable verbose output |

### From Source

```bash
# Build with version info from git (recommended)
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

The Makefile injects git commit, branch, version, build time, and platform info via ldflags.

## Quick Start

```bash
# Authenticate with the Pinner.xyz service
pinner auth

# Upload a file
pinner upload file.png

# Pin an existing CID
pinner pins add bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# List your pins
pinner pins ls

# Check pin status
pinner pins status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Create a website
pinner websites create example.com --cid bafybeig...

# Manage DNS
pinner dns zones create --domain example.com

# Publish to IPNS
pinner ipns publish bafybeig... --key-name my-key
```

## Configuration

Config file location: `~/.config/pinner/config.yaml`

```yaml
auth_token: your-jwt-token
base_endpoint: pinner.xyz
max_retries: 3
memory_limit: 100
secure: true
gateway_endpoint: ""
```

### Environment Variables

All config keys can be set via `PINNER_<UPPERCASE_KEY>` environment variables.

| Variable | Description |
|----------|-------------|
| `PINNER_AUTH_TOKEN` | Override auth token for the current session |
| `PINNER_BASE_ENDPOINT` | Override API base endpoint |
| `PINNER_MAX_RETRIES` | Maximum retry attempts for failed operations |
| `PINNER_MEMORY_LIMIT` | Memory limit in MB for CAR generation |
| `PINNER_SECURE` | Use HTTPS for endpoints |
| `PINNER_GATEWAY_ENDPOINT` | IPFS gateway URL |
| `PINNER_EMAIL` | Email for `auth` command |
| `PINNER_PASSWORD` | Password for `auth` and `account otp disable` commands |
| `PINNER_OTP` | OTP code for `auth` command |

## Commands

### Authentication

```bash
# Authenticate with JWT token
pinner auth <jwt-token>

# Authenticate interactively (prompts for email/password)
pinner auth

# Authenticate with flags
pinner auth --email user@example.com --password secret

# Authenticate with OTP
pinner auth --email user@example.com --otp-code 123456

# Authenticate with API key options
pinner auth --key-name "my-key" --no-create-key

# Force re-authentication
pinner auth --force

# Check authentication status
pinner auth status
```

### Account

```bash
# Show current account info
pinner account

# Enable two-factor authentication
pinner account otp enable --otp <code>

# Disable two-factor authentication
pinner account otp disable --password <password>
```

### Register

```bash
# Create a new account
pinner register --email user@example.com --first-name John --last-name Doe --password secret
```

### Confirm Email

```bash
# Confirm email address
pinner confirm-email --email user@example.com --token <confirmation-token>
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

# Upload without waiting for pinning to complete
pinner upload file.png --no-wait

# Upload from stdin (pipe)
cat file.txt | pinner upload --name "piped-file"

# Dry run (show what would be uploaded)
pinner upload file.png --dry-run

# Customize CAR generation
pinner upload file.png --memory-limit 512 --chunk-size 1048576 --chunker size-262144 --max-links 174

# JSON output for automation
pinner upload file.png --json
```

### Download

```bash
# Download content to a file
pinner download bafybeig... -o output.bin

# Download a specific path from a directory CID
pinner download bafybeig.../path/to/file.txt -o file.txt

# Force overwrite existing file
pinner download bafybeig... -o output.bin --force

# Dry run (show what would be downloaded)
pinner download bafybeig... --dry-run
```

### Cat

Stream IPFS content directly to stdout.

```bash
# Stream content to stdout
pinner cat bafybeig...

# Stream a specific file from a directory CID
pinner cat bafybeig.../path/to/file.txt

# Pipe to another command
pinner cat bafybeig... | gzip > archive.gz
```

### Ls

List contents of an IPFS directory.

```bash
# List directory contents
pinner ls bafybeig...

# List a nested directory
pinner ls bafybeig.../subdir

# Limit results
pinner ls bafybeig... --limit 50
```

### Pins

Manage pinned content with subcommands. This is the canonical command group for pinning operations.

```bash
# Add a pin
pinner pins add bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Add with custom name
pinner pins add bafybeig... --name "My Pin"

# Add without waiting for completion
pinner pins add bafybeig... --no-wait

# Add with metadata
pinner pins add bafybeig... --meta owner=alice --meta env=prod

# Add multiple CIDs at once
pinner pins add cid1 cid2 cid3 --name "Batch"

# Add CIDs from a file (one per line)
pinner pins add --file cids.txt --name "Batch"

# Add in parallel
pinner pins add --file cids.txt --parallel 3

# List pins
pinner pins ls

# List with filters
pinner pins ls --status pinned --limit 20

# Check pin status
pinner pins status bafybeig...

# Update pin name and metadata
pinner pins update bafybeig... --name "Renamed" --meta owner=bob

# Clear all metadata
pinner pins update bafybeig... --clear-meta

# Remove a pin
pinner pins rm bafybeig... --force

# Remove multiple pins
pinner pins rm cid1 cid2 cid3 --force

# Remove all pins (requires --force)
pinner pins rm --all --force

# Remove all failed pins
pinner pins rm --all --status failed --force

# Dry run
pinner pins add bafybeig... --dry-run
```

**Note**: `pin`, `list`, `status`, and `unpin` are first-class shortcuts for `pins add`, `pins ls`, `pins status`, and `pins rm` respectively — use whichever you prefer.

### Pin

Pin existing content by CID. Alias for `pinner pins add`.

```bash
# Pin a single CID
pinner pin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Pin with custom name
pinner pin bafybeig... --name "My Pin"

# Pin without waiting for completion
pinner pin bafybeig... --no-wait

# Pin multiple CIDs at once
pinner pin cid1 cid2 cid3 --name "Batch"

# Pin CIDs from a file (one per line)
pinner pin --file cids.txt --name "Batch"

# Pin in parallel
pinner pin --file cids.txt --parallel 3

# Dry run
pinner pin bafybeig... --dry-run
```

### List

List your pinned content. Alias for `pinner pins ls`.

```bash
# List recent pins (default: 10)
pinner list

# Filter by name
pinner list --name "My Pin"

# Limit results
pinner list --limit 20

# Filter by status
pinner list --status pinned

# Watch for changes
pinner list --watch

# JSON output
pinner list --json
```

### Status

Check the status of a pinned CID. Alias for `pinner pins status`.

```bash
# Check pin status
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Watch until settled
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --watch

# JSON output
pinner status bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4 --json
```

### Unpin

Remove pins. Alias for `pinner pins rm`.

```bash
# Unpin (prompts for confirmation)
pinner unpin bafybeig77vqcdozl2wyk6z3cscaj5q5fggi53aoh64fewkdiri3cdauyn4

# Skip confirmation
pinner unpin bafybeig... --force

# Unpin multiple CIDs
pinner unpin cid1 cid2 cid3 --force

# Unpin CIDs from a file
pinner unpin --file cids.txt --force

# Unpin in parallel
pinner unpin --file cids.txt --parallel 3

# Unpin all pins
pinner unpin all --force

# Unpin all with status filter
pinner unpin all --status failed --force

# Dry run
pinner unpin bafybeig... --dry-run

# JSON output
pinner unpin bafybeig... --json
```

### Metadata

The `metadata` command has been removed. Use `pinner pins update` to manage pin metadata instead.

```bash
# Update metadata for a pin
pinner pins update bafybeig... --meta owner=derrick --meta type=backup

# Clear all metadata
pinner pins update bafybeig... --clear-meta

# Rename and update metadata
pinner pins update bafybeig... --name "Backup" --meta type=archive
```

If you run `pinner metadata`, the CLI returns an error suggesting `pinner pins update`.

### Operations

List and inspect account operations.

```bash
# List operations
pinner operations list

# Filter operations
pinner operations list --status pending --operation upload --limit 20

# Watch operations
pinner operations list --watch

# Get details of a specific operation
pinner operations get <operation-id>

# Watch an operation until complete
pinner operations get <operation-id> --watch
```

### DNS

Manage DNS zones and records.

```bash
# List all DNS zones
pinner dns zones list

# Create a DNS zone
pinner dns zones create --domain example.com

# Create with custom nameservers
pinner dns zones create --domain example.com --nameservers ns1.example.com,ns2.example.com

# Get a DNS zone
pinner dns zones get example.com

# Validate zone nameserver delegation
pinner dns zones validate example.com

# Delete a DNS zone
pinner dns zones delete example.com

# List DNS records for a zone
pinner dns records list example.com

# Create a DNS record
pinner dns records create example.com --name www --type A --content "1.2.3.4" --ttl 3600

# Get a DNS record
pinner dns records get example.com --name www --type A

# Update a DNS record
pinner dns records update example.com --name www --type A --content "5.6.7.8"

# Delete a DNS record
pinner dns records delete example.com --name www --type A
```

### IPNS

Manage IPNS keys and records.

```bash
# List all IPNS keys
pinner ipns keys list

# Create a new IPNS key
pinner ipns keys create my-key

# Create with existing key
pinner ipns keys create my-key --key <base64-key>

# Get details of a specific key
pinner ipns keys get my-key

# Delete an IPNS key
pinner ipns keys delete my-key

# Publish a CID to an IPNS key
pinner ipns publish bafybeig... --key-name my-key

# Publish with custom TTL
pinner ipns publish bafybeig... --key-name my-key --ttl 1h

# Publish and wait for completion
pinner ipns publish bafybeig... --key-name my-key --wait

# Republish an IPNS record
pinner ipns republish my-key

# Resolve an IPNS name to its target CID
pinner ipns resolve k51qzi5uqu5dg4vh...
```

### Websites

Manage IPFS-hosted websites.

```bash
# List all websites
pinner websites list

# Create a website
pinner websites create example.com --cid bafybeig...

# Create with DNS hosting
pinner websites create example.com --cid bafybeig... --dns-hosting

# Get website details
pinner websites get example.com

# Update a website
pinner websites update example.com --cid bafybeig...

# Enable IPNS targeting
pinner websites enable-ipns example.com --cid bafybeig...

# Delete a website
pinner websites delete example.com

# Validate a website
pinner websites validate example.com

# Check SSL certificate status
pinner websites ssl status example.com

# Watch SSL provisioning
pinner websites ssl status example.com --watch

# Show website hosting configuration
pinner websites config

# Interactive website creation wizard
pinner websites wizard
```

### Bench

Benchmark upload and pinning performance.

```bash
# Run default benchmark (1MB, 1 file)
pinner bench

# Benchmark with custom size
pinner bench --size 10MB

# Benchmark multiple files
pinner bench --files 10 --depth 2

# Run multiple iterations
pinner bench --iterations 3

# Parallel uploads
pinner bench --parallel 3

# Keep benchmark artifacts
pinner bench --no-cleanup

# Customize CAR generation and polling
pinner bench --memory-limit 512 --chunk-size 1048576 --chunker size-262144 --max-links 174 --poll-interval 1s

# Dry run (show what would be benchmarked)
pinner bench --dry-run

# Benchmark a specific path
pinner bench ./my-folder
```

### Config

Manage CLI configuration.

```bash
# Show all config
pinner config

# Get specific value
pinner config get auth_token

# Set value
pinner config set base_endpoint custom.endpoint.com

# Dry run (preview change without saving)
pinner config set base_endpoint custom.endpoint.com --dry-run
```

### Setup

Interactive setup wizard for initial configuration.

```bash
# Run setup wizard
pinner setup

# Skip authentication step
pinner setup --skip-auth

# Skip configuration step
pinner setup --skip-config

# Reset existing config
pinner setup --reset

# Non-interactive mode
pinner setup --non-interactive
```

The setup wizard guides you through:
- Authentication with the Pinner.xyz service
- API endpoint configuration
- Shell completion setup
- Quick tutorial

### Doctor

Diagnose configuration and environment issues.

```bash
# Run diagnostics
pinner doctor

# JSON output
pinner doctor --json
```

Displays: version, OS/arch, config path and status, endpoint, memory limit, max retries, auth status, and shell completion status.

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
| `--auth-token` | Override auth token (env: `PINNER_AUTH_TOKEN`) |
| `--secure` | Use HTTPS instead of HTTP for endpoints (default: true, env: `PINNER_SECURE`) |
| `--version, -V` | Show version |
| `--help, -h` | Show help |

## Help Categories

Commands are organized into categories in the help output:

| Category | Commands |
|----------|----------|
| **Setup** | `setup`, `auth`, `register`, `confirm-email` |
| **Content** | `upload`, `download`, `cat`, `ls` |
| **Pinning** | `pins`, `pin`, `list`, `status`, `unpin`, `metadata` |
| **Management** | `websites`, `dns`, `ipns`, `operations` |
| **System** | `config`, `doctor`, `bench` |
| **Admin** | `admin` |

Use `pinner --help` to see the full command list organized by category.

## Architecture

### Project Structure

Primary source files shown (test files, mocks, and supporting implementations omitted for clarity).

```
pinner-cli/
├── cmd/pinner/                    # CLI entry point
│   └── main.go                    # Main application entry
├── pkg/
│   ├── cli/                       # Primary CLI command implementations
│   │   ├── auth.go                # Authentication commands
│   │   ├── account.go             # Account and OTP management
│   │   ├── register.go            # Account registration
│   │   ├── confirm_email.go       # Email confirmation
│   │   ├── upload.go              # Upload commands
│   │   ├── download.go            # Download, cat, ls commands
│   │   ├── pin.go                 # Pin command (alias for pins add)
│   │   ├── pins.go / pins_*.go    # Pins command group (add, rm, ls, status, update)
│   │   ├── list.go                # List command (alias for pins ls)
│   │   ├── status.go              # Status command (alias for pins status)
│   │   ├── unpin.go / unpin_all.go # Unpin command (alias for pins rm)
│   │   ├── metadata.go            # Removed command (error with suggestion)
│   │   ├── operations.go          # Operations list/get
│   │   ├── dns.go                 # DNS zone/record management
│   │   ├── ipns.go                # IPNS key/publish/resolve
│   │   ├── websites.go           # Website management
│   │   ├── websites_ssl.go        # SSL status
│   │   ├── websites_wizard.go     # Interactive website wizard
│   │   ├── bench.go               # Benchmarking
│   │   ├── config.go              # Configuration commands
│   │   ├── doctor.go              # Diagnostics
│   │   ├── setup.go / setup_wizard.go / setup_pterm.go # Setup wizard
│   │   ├── admin.go               # Admin commands (quota, billing, websites)
│   │   ├── version.go             # Version display
│   │   ├── flags.go               # Global flags definition
│   │   ├── register.go            # Command registration
│   │   ├── root.go                # Root command
│   │   ├── output.go              # Output formatting (human + JSON)
│   │   ├── progress.go            # Progress display
│   │   ├── error_formatter.go     # Structured error formatting
│   │   ├── sources.go             # Flag/env/config source resolution
│   │   ├── *_service.go           # Service interface definitions
│   │   ├── wizard/                # Generic Go-generics wizard framework
│   │   └── internal/              # Internal implementations
│   │       ├── pinning_client.go       # PinningClient interface (boxo wrapper)
│   │       ├── pinning_client_boxo.go  # BoxoPinningClient with retry logic
│   │       └── http_client.go          # Retry HTTP transport
│   ├── config/                    # Configuration management
│   │   ├── core.go                # Config struct, keys, defaults, endpoints
│   │   └── manager.go            # Manager interface (extends configmanager)
│   └── internal/
│       ├── car.go                 # CAR file root reading
│       └── io/
│           └── stdinfs.go         # stdin fs implementation
├── build/                         # Build-time information
│   ├── build.go                   # Build variables (Version, GitCommit, etc.)
│   └── build_info.go              # BuildInfo interface and struct
├── mocks/                         # Generated mocks
├── .mockery.yaml                  # Mockery configuration
├── go.mod                         # Go module definition
└── README.md                      # This file
```

### Key Components

**Service Layer Pattern**: Each major feature has a service interface with default implementation, enabling dependency injection for testing:

| Interface | Description |
|-----------|-------------|
| `PinningService` | Pin/unpin/list/status/batch operations on existing CIDs |
| `StatusService` | Extended status checking with operation tracking |
| `UploadService` | Upload files/directories to IPFS |
| `AuthService` | Authentication, registration, OTP, token management |
| `DownloadService` | Download, cat, and directory listing from IPFS |
| `BenchService` | Benchmark upload and pinning performance |
| `OperationsService` | List and inspect account operations |
| `DNSService` | DNS zone and record CRUD operations |
| `IPNSService` | IPNS key management, publish, republish, resolve |
| `WebsitesService` | Website CRUD, SSL status, config |
| `QuotaAdminService` | Admin quota plans, allowances, user configs, stats |
| `BillingAdminService` | Admin credits, price lines, pricing plans, subscribers |
| `WebsiteAdminService` | Admin website block/unblock |

**Output Formatting**: The `Output` interface provides methods for formatted output (`Print`, `Printf`, `PrintTable`, `PrintJSON`, etc.) with both human-readable and JSON implementations, selected by the `--json` flag.

**Wizard Framework**: `pkg/cli/wizard/` provides a generic Go-generics wizard framework (`Step[S any]`, `Run[S]()`) used by the websites and setup wizards. Each step defines `Name()`, `ShouldSkip()`, `Execute()`, and `ShouldRetry()`.

**Error Formatting**: `BenchError`, `HTTPError`, and `FormatError` provide structured error types with JSON-safe output (human-readable via `FormatError`, raw structured data for `--json`).

**CAR Generation**: Upload uses IPFS boxo libraries for DAG building and CAR format handling. CAR root reading is in `pkg/internal/car.go`.

**CLI Command Pattern**: Each command has a `newXxxCommand()` function returning a `cli.Command`. Commands use `commandGetter` interface for flag access and factory functions for dependency injection.

**Build Information**: Version, commit, and build time are injected at build time via ldflags. `build.Default` provides access throughout the application.

## Development

### Prerequisites

- Go 1.26.0 or later
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
# Build with version info (recommended)
make build

# Build without make
go build -o pinner ./cmd/pinner

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o pinner-linux-amd64 ./cmd/pinner
GOOS=darwin GOARCH=arm64 go build -o pinner-darwin-arm64 ./cmd/pinner
GOOS=windows GOARCH=amd64 go build -o pinner-windows-amd64.exe ./cmd/pinner
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
- Check that `~/.config/pinner/config.yaml` exists and contains a valid `auth_token`

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
