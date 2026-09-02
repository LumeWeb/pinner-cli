# Pinner CLI — Architecture

This document describes the layout and design of the Pinner.xyz CLI
(`go.lumeweb.com/pinner-cli`). It is a companion to
[`docs/build.md`](build.md) (build &amp; test workflow) and is intended for
developers and AI agents working in this repository.

## Overview

Pinner CLI is a Go command-line tool for managing IPFS content through the
Pinner.xyz service: upload, pin, download, website hosting, DNS, IPNS,
vault/Sia accounts, and admin operations. It is built on
[`urfave/cli/v3`](https://github.com/urfave/cli/v3), the IPFS
[`boxo`](https://github.com/ipfs/boxo) libraries, and the Lume Web
[`portal-sdk`](https://github.com/LumeWeb/portal-sdk).

The codebase is organized around a **two-tier design**:

- A **core domain layer** (`internal/core/`) that contains the business logic
  and is free of any presentation/frontend dependency (no urfave, no MCP).
- A set of **frontends** (`internal/cli/`, `internal/mcp/`) that present that
  domain to a terminal CLI and to an MCP server respectively.

The two primary frontends are **both compiled from a single in-memory
operation catalog** (`internal/catalog/`). No frontend is the source of truth
for what an operation does or how it is described; the catalog is.

## Repository Layout

```
cmd/pinner/                 Entry point. Minimal main.go -> cli.Run()
internal/core/<domain>/     Domain logic; pure Go, no urfave/MCP/Output
internal/catalog/           Operation-descriptor registry (single source of truth)
internal/catalogops/        Per-domain Operation providers
internal/cli/               urfave CLI commands, service interfaces, wiring
internal/cli/internal/      PinningClient / BoxoPinningClient (HTTP + retry)
internal/fieldform/         CLI-side wizard field system (Field/Gather/ValueSource)
internal/cli/wizard/        pterm-backed wizard + prompter; install wizard
internal/mcp/               MCP server adapter (catalog-driven tool surface)
internal/mcp/wizard/        MCP-side wizard FSMs (website/setup flows)
internal/mcp/core/          MCP internal building blocks (sessions, model, transfer)
internal/mcp/hostenv/       Host platform capability model (features/profiles)
internal/mcp/toolforge/     Forge: host-aware tool/schema/guide construction
internal/mcpapp/            Embedded MCP app assets & CSS (go:embed)
internal/urlopen/           Cross-platform "open URL in browser" helper
internal/service/           OS service integration (Windows/systemd/launchd)
internal/car/               CAR file root reading
internal/io/                stdin as fs.FS
internal/dnsutil/           DNS helpers
internal/mcptest/           Go fake upstream server used by integration tests
tests/sunpeak/              MCP integration tests (drives `pinner mcp` over stdio)
build/                      Build-time info (version/commit injected via ldflags)
```

### Core domain packages (`internal/core/`)

Each domain is its own package and owns its service logic and types:

- `auth` — authentication, registration, OTP, session tokens
- `upload`, `download`, `pinning`, `status`, `operations` — content workflows
- `websites`, `dns`, `ipns` — hosting, DNS zones/records, IPNS keys
- `vault` — Sia vault account and seed management
- `admin`, `apikeys`, `bench` — admin surface, API keys, benchmarking
- `config` — configuration management (extends `configmanager`)
- `errors` — shared error types
- `ipfsbase` — shared IPFS client base

## The Operation Catalog

`internal/catalog` holds the registry of **operations**, each described by an
`Operation` descriptor. The two frontends compile the same registry into
urfave commands (CLI) and MCP tools (MCP). This replaced the earlier design in
which the MCP surface was derived by walking the CLI command tree — a model
that forced the MCP layer to reverse-engineer intent from command names and
had no clean place for frontend-specific divergence.

### Operation descriptor

An `Operation` carries declarative metadata plus an input schema and a
handler:

| Metadata | Meaning |
|---|---|
| `Name` | Dot-namespaced, e.g. `pins.add`, `dns.records.create` |
| `Safety` | `Read` / `Mutate` / `Destructive` |
| `Interaction` | `AgentSafe` / `HumanOnly` / `NeedsHandoff` |
| `Visibility` | `Model` (agent-discoverable) / `AppOnly` / `Both` |
| `Args` | Declared `OperationArg` list → JSON Schema |
| `Category` | Grouping for discovery/help |

`OperationArg` types map to urfave flag types and to JSON Schema
(`ArgTypeString`, `ArgTypeInt`, `ArgTypeFloat`, `ArgTypeBool`,
`ArgTypeNullableBool`, `ArgTypeNullableInt`, `ArgTypeDuration`,
`ArgTypeStringSlice`, `ArgTypeFlexibleID`, ...). Args can be marked
`Required`, `PositionalOnly`, or grouped into mutually exclusive
`SelectionGroup`s (compiled to `oneOf` in the MCP schema).

### Runtime classifications

- **Safety** is declared on the operation, never inferred from its name.
  Destructive operations (delete, forget, `--force` paths) are gated at
  dispatch. A destructive op invoked by a model actor is refused with the
  `ErrConfirmRequired` sentinel (surfaced as a `needs_human` confirm hand-off).
  It runs headlessly only when the operation explicitly opts into agent
  self-confirmation by marking a bool `confirm` argument `AgentConfirm` and the
  model supplies `confirm=true` (e.g. `vault_version_restore`'s rollback
  contract). Every other destructive op — no `confirm`, an `AgentRequired`
  `confirm` (`ipns_keys_delete`, admin platform-domain delete), or a `confirm`
  with a different semantic (`api_keys_delete`'s self-delete force guard) —
  always requires the human hand-off for a model actor. `AgentConfirm` is
  validated at registration to be set only on a bool arg named `confirm`.
- **Interaction** tells a frontend whether an autonomous actor may invoke the
  operation directly, whether it must prompt a human interactively, or whether
  it needs an out-of-band human step (see *Runtime classifications*
  below).
- **Actor** (`model` / `app` / `human`) is part of the dispatch context and is
  used to enforce the interaction/visibility/safety gates.

### Discovery vs dispatch

`ToolDescriptor` is the **discovery-only** view produced by
`Catalog.Describe`/the MCP compiler. It deliberately carries **no executable
handler** — a consumer that finds a tool cannot invoke it directly. All
execution goes through **`Catalog.Invoke`**, the single enforcement point for
interaction, visibility, safety, and required-argument gates. This invariant
prevents bypassing confirmation prompts or human-only restrictions.

### Compilers

- `internal/catalog/compile_cli.go` — builds urfave `*cli.Command` trees from
  `Operation` descriptors (flags, help, names).
- `internal/catalog/compile_mcp.go` — builds the base MCP tool JSON Schema
  from the same descriptors, including value-aware `oneOf` for
  `SelectionGroup`s. The published schema can then be adapted per host profile
  by `internal/mcp/toolforge` (see *MCP server*).
- `internal/catalog/accessors.go` — typed readers (`StrArg`, `IntArg`,
  `BoolArg`, `BoolArgPtr`, ...) that decouple handlers from the raw input map.
- `internal/catalog/positional.go`, `search.go`, `list.go` — positional-arg
  handling and discovery helpers.

### Operation providers: `internal/catalogops/`

`internal/catalogops` holds one provider per domain (e.g. `pins.go`,
`websites.go`, `dns.go`, `vault.go`). Each provider returns a slice of
`catalog.Operation`s (`PinsOperations(deps)`, `DNSOperations(deps)`, ...) and
takes a per-domain deps struct (getter **functions**, never values — deps are
resolved lazily per invocation, never at package init). Handlers drive the
core service domains directly and **return typed data**; rendering belongs to
the frontends.

## Frontends

### CLI (`internal/cli/`)

- `root.go` — `NewRootCommand()`, `Run(ctx, args)`, command tree root,
  `mcpadapter.MCPCommand()` registration.
- Command files (`pins.go`, `upload.go`, `websites.go`, `dns.go`, ...) plus
  the domain service interfaces and their implementations.
- Catalog wiring (`catalog_wiring.go`, `dns_wiring.go`, ...) adapts catalog
  operations to the urfave tree: it maps positional args and `--file`/stdin
  into the operation input map, applies CLI-only gates (`--force`), and renders
  the handler's returned data through the `Output` formatter. The catalog
  compiler supplies flags/help/names; only the action is wrapped.
- `internal/cli/internal/` — `PinningClient` (wraps boxo's remote pinning
  client) and `BoxoPinningClient` (the concrete implementation) with an HTTP
  client that supports retry/backoff.

### MCP server (`internal/mcp/`)

- `sdk_official.go` — the official MCP SDK server, transports, capability and
  tool registration.
- `catalog.go` — `ToolCatalog`: the in-memory registry behind a **two-tier
  tool surface**. Curated, most-used tools are listed directly in
  `tools/list`; the remaining catalog is served through progressive disclosure
  (`search_tools` → `describe_tool` → the typed invoke dispatchers
  `invoke_read_tool`/`invoke_write_tool`/`invoke_destructive_tool`), keeping the initial tool
  surface small and the context budget predictable.
- `catalogassembly.go` — `AssembleCatalogOps(deps *CatalogDepsBundle)` builds
  one catalog covering every domain (auth, account, vault, pins, websites,
  dns, ipns, api-keys, operations, admin, ...).
- `catalogdeps.go` — `CatalogDepsBundle`, per-domain deps wired from CLI state.
- `catalogdispatch.go` — `DispatchCatalogOp(...)`: translates typed tool
  requests into operation input, routes them through the catalog, and converts
  gated outcomes (e.g. confirmation-required, interactive-only) into MCP result
  envelopes.
- `hostenv/` + `toolforge/` — the MCP surface is **host-aware**: tool
  descriptions, JSON schemas, and file-input variants are resolved against the
  connected host profile (platform, transport, auth) through a feature-gating
  layer (`hostenv.Feature` sets). The agent guide is built from the same
  platform DSL.
- `resources.go` / `prompts.go` — `pinner://` resources and prompt templates.
- `internal/mcp/wizard/` — FSM-based wizard flows (e.g. website creation),
  session-based with a TTL (`DefaultSessionTTL = 30m`,
  `DefaultMaxSessions = 100`).
- `internal/mcpapp/` — embedded JS/CSS asset bundles for MCP-hosted UIs
  (built by `pnpm`, embedded with `go:embed`; the server fails at startup if
  the bundles are missing).

## Service Layer Pattern

Each major feature is exposed through a **service interface** with a default
implementation:

- `PinningService`, `StatusService`, `UploadService`, `AuthService`,
  `DownloadService`, `BenchService`, `OperationsService`, `DNSService`,
  `IPNSService`, `WebsitesService`, plus the admin service interfaces
  (`QuotaAdminService`, `BillingAdminService`, `WebsiteAdminService`, ...).

Rules:

- CLI service interfaces are **delegation only** — they pass through to the
  SDK wrapper, they do not contain business logic.
- Implementations are constructed through **factory functions**
  (`defaultPinningServiceFactory`, `defaultUploadServiceFactory`, ...) which
  take a config manager (and, where relevant, an output writer). Commands
  accept factories so tests can inject fakes.
- This makes the whole tree dependency-injectable: a command handler receives
  a factory, so tests never need a live urfave context or a network call.

## Output Formatting

The `Output` interface separates presentation from logic:

- `Print` / `Printf` / `Printfln` — text output
- `PrintJSON` — structured output
- `PrintTable` / `PrintList` — tabular list rendering
- `MaskSensitive` — token/password masking
- `Watch` — long-running polling loops

`humanFormatter` and `jsonFormatter` implement it, selected by the global
`--json` flag. Handlers in `catalogops` return typed data; the CLI wiring
renders it, so the same handler serves both human and JSON output — and MCP,
which formats its own results.

## Import Boundaries

- `internal/core/...` depends on **nothing** above the domain layer.
- `internal/cli` imports `internal/mcp` (to register the `mcp` command);
  therefore `internal/mcp` **must not** import `internal/cli` (cycle).
- Cross-cutting helpers that both need live in neutral leaf packages:
  `internal/urlopen` (browser opening), etc.
- `internal/catalogops` is presentation-free: it never imports `internal/cli`
  and returns data rather than rendered output.

## Wizards

Two wizard systems exist:

- **CLI side** — `internal/fieldform/` (declarative `Field[S,T]`,
  `Gather`/`GatherAny`, `ValueSource` provenance) wired to a pterm UI by
  `internal/cli/wizard/`. Used for interactive install/setup and service
  configuration.
- **MCP side** — `internal/mcp/wizard/`: typed structs with `jsonschema` tags
  compiled to step definitions, driven as stateless FSM transitions with
  TTL-bounded sessions.

## Content Flows

- **CAR generation (upload)**: uploads build the DAG and CAR via IPFS boxo;
  CAR roots are read with `GetCarRoots` in `internal/car`. Memory is capped by
  the `--memory-limit` flag (default 100 MB).
- **Website updates**: because an existing domain conflicts with
  `websites create`, updating content follows a specific order — resolve the
  current state, pin the new CID, update, then verify. See the *Website Update
  Flow* section in `AGENTS.md` for the full protocol and its failure modes.

## Design Invariants

- Presentation is a frontend concern; frontends may diverge, but they compile
  from the shared catalog.
- Handlers return typed data; rendering happens in the wiring layer.
- Deps are resolved lazily (getters), never at package init.
- Dispatch always flows through `Catalog.Invoke`; descriptors are discovery
  only.
- `internal/mcp` never reasons about stdin; interactive flows that cannot run
  through the LLM channel are represented as `InteractionNeedsHandoff` and
  surfaced by the frontend.
