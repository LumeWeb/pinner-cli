# AGENTS.md

This file provides guidance to AI agents and developers working with the
Pinner.xyz CLI. For the full architecture, see
[`docs/architecture.md`](docs/architecture.md); for build, test, and release
workflows, see [`docs/build.md`](docs/build.md). For auditing the MCP
tool-programming surface as a connected host sees it (host-specific regressions
and intel), see [`docs/mcp-host-audit.md`](docs/mcp-host-audit.md).

## Common Commands

### Building

**Use `make` targets.** The Makefile chains the full pipeline (`templ
generate` → JS app bundles → Tailwind CSS → Go build), which a raw
`go build`/`go test` skips — the latter produces a binary missing the embedded
app assets.

```bash
make build          # full pipeline, produces ./pinner with version info
make install        # full pipeline, installs to $GOPATH/bin
make test           # build assets, then go test ./...
make generate       # templ generate only
make clean          # rm -f pinner
```

Raw builds (for quick Go-only checks or cross-compilation):

```bash
go build -o pinner ./cmd/pinner

GOOS=linux   GOARCH=amd64 go build -o pinner-linux-amd64   ./cmd/pinner
GOOS=darwin  GOARCH=arm64 go build -o pinner-darwin-arm64  ./cmd/pinner
GOOS=windows GOARCH=amd64 go build -o pinner-windows-amd64.exe ./cmd/pinner
```

> **Required build tag:** the vault name search uses SQLite FTS5 (trigram), so
> the binary MUST be built with `-tags sqlite_fts5` (this compiles FTS5 into
> `mattn/go-sqlite3`, which ships with FTS5 disabled by default). `make
> build`/`make install`/`make test` pass it automatically. Any raw `go
> build`/`go test` invocation must add `-tags sqlite_fts5`, or the vault 0007
> migration (`CREATE VIRTUAL TABLE ... fts5`) fails and vault operations break.
> `Search()` still degrades to plain LIKE matching (no FTS) if the index is
> ever missing.

### Testing

```bash
go test -tags sqlite_fts5 ./...                  # whole suite (assumes assets already built)
go test -tags sqlite_fts5 ./internal/cli
go test ./internal/cli
go test -v ./...
go test ./internal/cli -run TestUpload
```

### Mock Generation

Mocks are generated with [mockery](https://vektra.github.io/mockery/latest/)
using `.mockery.yaml` (interface → output dir/package mapping, testify
template). Run `mockery` with **no arguments**. Do not reinstall mockery; the
dev environment already has it.

```bash
mockery        # regenerate all mocks from .mockery.yaml
```

### Running the CLI

```bash
go run ./cmd/pinner           # from source
./pinner <command>            # built binary
```

## Repository Layout

The codebase is a **two-tier design**: a domain layer plus frontends, all
compiled from a single operation catalog.

```
cmd/pinner/                 Entry point. Minimal main.go -> cli.Run()
internal/core/<domain>/     Domain logic; pure Go, no urfave/MCP/Output
internal/catalog/           Operation-descriptor registry (single source of truth)
internal/catalogops/        Per-domain Operation providers
internal/cli/               urfave CLI commands, service interfaces, wiring
internal/cli/internal/      PinningClient / BoxoPinningClient (HTTP + retry)
internal/fieldform/         CLI-side wizard field system (Field/Gather/ValueSource)
internal/cli/wizard/        pterm-backed wizard + prompter; Step[S]/Run[S] framework
internal/mcp/               MCP server adapter (catalog-driven tool surface)
internal/mcp/wizard/        MCP-side wizard FSMs (website/setup flows)
internal/mcp/core/          MCP building blocks (sessions, model, transfer, ...)
internal/mcp/hostenv/       Host platform capability model (features/profiles)
internal/mcp/toolforge/     Forge: host-aware tool/schema/guide construction
internal/mcpapp/            Embedded MCP app assets & CSS (go:embed)
internal/urlopen/           Cross-platform "open URL in browser" helper
internal/service/           OS service integration (Windows/systemd/launchd)
internal/car/               CAR file root reading (GetCarRoots)
internal/io/                stdin as fs.FS (stdinfs.go)
build/                      Build-time info (version/commit injected via ldflags)
tests/sunpeak/              MCP integration tests (driver `pinner mcp` over stdio)
```

### Core Directories

- **`internal/core/`** — domain logic, one package per domain: `auth`,
  `upload`, `download`, `pinning`, `status`, `operations`, `websites`, `dns`,
  `ipns`, `vault`, `admin`, `apikeys`, `bench`, `config`, `errors`,
  `ipfsbase`. Nothing here depends on urfave, MCP, or the `Output` formatter.
  - `internal/core/config/` — configuration management (extends
    `go.lumeweb.com/configmanager`). Default config location is platform
    native: `~/.config/pinner/config.yaml` (Linux),
    `~/Library/Application Support/pinner/config.yaml` (macOS),
    `%AppData%\pinner\config.yaml` (Windows), or `$PINNER_HOME/config.yaml`.
    Config keys include: `auth_token`, `base_endpoint`, `max_retries`,
    `memory_limit`, `secure`, `gateway_endpoint`, `default_timeout`,
    `upload_timeout`, `sync_timeout`.

- **`internal/catalog/`** — the operation registry. `Operation` descriptors
  declare `Safety` (Read/Mutate/Destructive), `Interaction`
  (AgentSafe/HumanOnly/NeedsHandoff), `Visibility` (Model/AppOnly/Both),
  and `Args`. `compile_cli.go` compiles operations into urfave commands;
  `compile_mcp.go` compiles them into MCP JSON Schemas. Dispatch always flows
  through `Catalog.Invoke`; the discovery-only `ToolDescriptor` never carries
  a handler.

- **`internal/catalogops/`** — one provider per domain (`pins.go`,
  `websites.go`, `dns.go`, ...) returning `[]catalog.Operation`. Providers
  take per-domain deps structs of **getter functions** (resolved lazily per
  invocation, never at package init). Handlers return typed data; rendering is
  the frontend's job.

- **`internal/cli/`** — urfave CLI commands and orchestration.
  - Root command and registration in `root.go`; global flags in `flags.go`.
  - Domain service interfaces (`PinningService`, `StatusService`,
    `UploadService`, `AuthService`, `DownloadService`, `BenchService`,
    `OperationsService`, `DNSService`, `IPNSService`, `WebsitesService`,
    `QuotaAdminService`, `BillingAdminService`, `WebsiteAdminService`,
    `AdminTokenProvider`) with concrete implementations.
  - Catalog wiring (`catalog_wiring.go`, `dns_wiring.go`, ...) adapts catalog
    operations to the urfave tree (positional args, `--file`/stdin, `--force`
    gate, rendering).
  - `internal/cli/internal/` — `PinningClient` wraps boxo's remote pinning
    client; `BoxoPinningClient` is the concrete implementation with an HTTP
    client supporting retry.

- **`internal/mcp/`** — MCP adapter exposing the CLI tree as an MCP server
  over stdio.
  - `sdk_official.go` — official MCP SDK server, transport, capability/tool
    registration.
  - `catalog.go` — `ToolCatalog`: a two-tier tool surface. Curated, most-used
    tools are listed directly in `tools/list`; the rest of the catalog is
    served through progressive disclosure (`search_tools` → `describe_tool` →
    `invoke_tool`).
  - `hostenv/` + `toolforge/` — the MCP surface is host-aware: tool
    descriptions, schemas, and variants are resolved against the connected host
    profile (platform/transport/auth) via feature gating (`hostenv.Feature`
    sets); the agent guide is built from a platform DSL.
  - `catalogassembly.go` / `catalogdeps.go` — `AssembleCatalogOps(deps
    *CatalogDepsBundle)` builds one catalog covering every domain;
    per-domain deps live in `CatalogDepsBundle`.
  - `catalogdispatch.go` — `DispatchCatalogOp` routes typed tool requests
    through the catalog and maps gated outcomes into MCP result envelopes.
  - `resources.go` / `prompts.go` — `pinner://` resources and prompt
    templates.
  - `internal/mcp/wizard/` — FSM wizard flows, session-based with TTL
    (`DefaultSessionTTL = 30m`, `DefaultMaxSessions = 100`).
  - `internal/mcpapp/` — embedded JS/CSS app bundles (server fails at startup
    if missing; build with `pnpm` first).

## Key Interfaces and Patterns

### Service Layer Pattern

Each major feature has a service interface with a default implementation
(`PinningService`, `UploadService`, ...). Rules:

- CLI service interfaces are **delegation only** — they pass through to the
  SDK wrapper, not raw SDK methods, and contain no business logic.
- Service instances come from **factory functions**
  (`defaultPinningServiceFactory`, `defaultUploadServiceFactory`, ...). Commands
  accept factories so tests can inject fakes.

### Output Formatting

The `Output` interface separates presentation from logic:

- `Print` / `Printf` / `Printfln` — text; `PrintJSON` — structured output
- `PrintTable` / `PrintList` — tabular list rendering
- `MaskSensitive` — token/password masking
- `Watch` — long-running polling loops

Two implementations (`humanFormatter`, `jsonFormatter`) are selected by the
global `--json` flag. Handlers in `catalogops` return typed data; wiring
layers render it, so one handler serves both human and JSON output.

### Operation Catalog Architecture

- Both frontends (CLI, MCP) compile from the catalog; **no frontend is the
  source of truth**.
- `Operation` metadata (Safety/Interaction/Visibility) is declared, never
  inferred from command names.
- **Discovery vs dispatch**: `ToolDescriptor` is the discovery-only view and
  carries no handler. All execution goes through `Catalog.Invoke` — the single
  enforcement point for interaction, visibility, safety, and required-arg
  gates.
- Normalization: operation input is normalized on every frontend path before
  handler execution, so defaults are applied and types coerced consistently
  (CLI and MCP).

### Command Wiring

**To add a traditional (service-based) command:**
1. Add the method to the service interface.
2. Add a delegating implementation (calls the SDK wrapper).
3. Create `newXxxCommand()` returning `*cli.Command` (urfave v3 uses
   `Commands:`, not `Subcommands:`).
4. Register in `root.go` or the parent command's `Commands` slice.
5. Extend **all** mock structs implementing the interface (func-type `*Fn`
   fields); grep every `*_test.go` and `*_handler_test.go`.
6. Update `expectedRootSubcommands` in `command_registration_test.go`, which
   asserts `len(root.Commands)`.

**To add a new domain to both CLI and MCP (catalog path):**
1. Core service in `internal/core/<domain>/` — pure Go, no urfave/MCP/Output.
2. Catalog ops in `internal/catalogops/<domain>_ops.go` — `Operation` with args
   + handler.
3. CLI wiring in `internal/cli/<domain>_wiring.go` — `CatalogOpsAdapter` impl,
   `catalogActionAdapter`.
4. Assembly in `internal/mcp/catalogassembly.go` — call
   `AssembleCatalogOps(deps *CatalogDepsBundle)`.

The domain must appear in **both** the CLI wiring and `catalogops`; missing
catalog ops produces a silent half-failure (CLI works, MCP has no tools).

### Import Boundaries

- `internal/core/...` depends on nothing above the domain layer.
- `internal/cli` imports `internal/mcp` (to register the `mcp` command);
  therefore `internal/mcp` **must not** import the `internal/cli` package (an
  import cycle). Leaf subpackages that do not themselves import `internal/cli`
  (e.g. `internal/cli/wizard`) are fine.
- Cross-cutting helpers both need live in neutral leaf packages, e.g.
  `internal/urlopen`.
- `internal/catalogops` is presentation-free (no `internal/cli`, no `Output`).

### Testing Patterns

- **`*WithService` helpers**: extract command logic into
  `xxxWithService(ctx, cmd, output, service)` so tests exercise it with mock
  services and no live urfave context.
- **Mock fidelity**: when extending an interface, extend **every** mock struct
  with func-type fields (`*Fn`) that return `nil, nil` when unset.
- **Integration**: `internal/mcptest` is a Go fake of the upstream API at the
  HTTP layer; `tests/sunpeak` drives `pinner mcp` as a real stdio server.

### Wizard Framework

Two wizard systems exist:

- **CLI side**: `internal/fieldform/` (declarative `Field[S,T]`,
  `Gather`/`GatherAny`, `ValueSource` provenance) wired to pterm by
  `internal/cli/wizard/` (generic `Step[S]`, `Run[S](ctx, ui, steps, state)`).
  Used for install/setup and service configuration.
- **MCP side**: `internal/mcp/wizard/` — typed structs with `jsonschema` tags
  compiled to step definitions, run as stateless FSM transitions with
  TTL-bounded sessions.

## Content Flows

### CAR Generation Flow

1. Upload builds the DAG + CAR via IPFS boxo libraries.
2. CAR roots are read via `GetCarRoots` in `internal/car/car.go`.
3. Memory is capped by the `--memory-limit` flag (default 100 MB).

### Website Update Flow

When updating an existing website (its domain already exists, so `websites
create` conflicts), follow this order. `websites_update` (MCP tool) / `websites
update` is the correct command; the MCP `website-update` prompt encodes this
same protocol.

1. **Resolve current state** — call `websites get <domain>` first. Capture the
   current `target_type` (`ipfs` or `ipns`) and `dns_hosting_enabled`. Never
   guess these; passing a wrong `target-type` can silently flip IPFS↔IPNS and
   break DNS.
2. **Pin the new content** — ensure the new CID is pinned before updating
   (`pins add` with wait). Updating an unpinned CID returns a 422
   `CidNotPinned`; pin first, then retry.
3. **Update** — `websites update <domain> --cid <new> --target-type <current>`.
   If `--target-type` is omitted together with `--cid`, the site's current
   `target_type` is preserved automatically, so a bare `--cid` update is safe.
   Change `target-type` only when intentionally switching IPFS↔IPNS.
4. **DNS by mode**:
   - **Managed** (`dns_hosting_enabled=true`): do not touch DNS. Pinner
     reconciles the `_dnslink` record asynchronously, so `websites validate` /
     `validation-status` right after the update may report the OLD CID. That is
     reconciliation lag, not failure — wait ~30–60s and re-check.
   - **Self-managed**: publish the new `_dnslink` TXT before validation will
     pass; read `pinner://websites/<domain>/dns-requirements` for the expected
     value.
5. **Verify** — re-check `websites get` (confirm `target_hash` updated) and
   then `websites validate`.

Common failure modes:
- `--target-type is required when --cid is provided` → re-run including the
  current `target-type` (or omit it and let it be inherited).
- `CID_NOT_PINNED` → the CID is not pinned on the gateway; run `pins add`
  first.
- Validation showing a stale `_dnslink` after a managed-DNS update →
  reconciliation is still running; wait and re-check, do not treat as an
  update failure.

## Command Structure

- `pins` is the canonical command group with subcommands `add`, `rm`, `ls`,
  `status`, `update`; root shortcuts (`pin`, `unpin`, `list`, `status`)
  delegate to it — first-class, no deprecation.
- `metadata` command removed; `pins update` replaces it (a hidden error
  command suggests this).
- Upload and `pins add` wait by default for pinning to complete;
  `--no-wait` to detach.
- `--meta key=value` on `pins add` and `upload` sets metadata at pin creation.
- `--force` is the primary skip-confirmation flag (consolidating
  `--confirm`/`--yes`).
- Shell completion is enabled for bash/zsh/fish/PowerShell.

## Global Flags

All commands support these global flags:
- `--json` — output JSON instead of human-readable text
- `--verbose, -v` — detailed output
- `--quiet, -q` — suppress non-error output
- `--unmask` — show sensitive data (tokens, passwords) unmasked
- `--auth-token` — override auth token (also reads `PINNER_AUTH_TOKEN` env
  var)
- `--secure` — use HTTPS instead of HTTP (default: true, env: `PINNER_SECURE`)

## Dependencies

- `github.com/urfave/cli/v3` — CLI framework
- `github.com/ipfs/boxo` — IPFS libraries (pinning, DAG, blockstore)
- `go.lumeweb.com/configmanager` — configuration management
- `go.lumeweb.com/portal-sdk` — Portal SDK (local replace in `go.mod`)
- `github.com/pterm/pterm` — terminal UI for the setup wizard
- `github.com/stretchr/testify` — testing framework
- `github.com/vektra/mockery` — mock generation
