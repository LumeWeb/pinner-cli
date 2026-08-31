# Pinner CLI — Build &amp; Development

Companion to [`docs/architecture.md`](architecture.md). This document covers
prerequisites, the build pipeline, testing, mock generation, and the checks
the CI runs.

## Prerequisites

- **Go 1.22+**
- **`templ`** CLI — compiles `.templ` files to Go (`templ generate`). The
  Makefile requires it on `PATH`.
- **`pnpm`** (Node workspace) — builds the MCP app JS bundles and the Tailwind
  CSS that Go embeds. Required by `make build` and `make test`.
- **Portal SDK** — `go.lumeweb.com/portal-sdk`, resolved via a local `replace`
  in `go.mod`.
- **CGO** is required (`CGO_ENABLED=1`) — there are cgo dependencies, so
  builds must not disable it.
- **mockery** — pre-installed in the dev environment; generate mocks via
  `mockery` with no arguments (uses `.mockery.yaml`).

> **Use `make` targets.** The Makefile chains the full pipeline
> (`templ generate` → JS/CSS assets → Go build). Raw `go build` / `go test`
> skip asset generation and produce a binary missing the embedded app bundles
> and CSS.

## Quick Start

```bash
make build        # full pipeline, produces ./pinner with version info
make test         # build assets, then go test ./...
make install      # full pipeline, install to $GOPATH/bin
./pinner --help
```

Running from source: `go run ./cmd/pinner <command>`.

## Makefile Targets

| Target | Depends on | Action |
|---|---|---|
| `make build` (default) | `mcpembed` | `CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner` |
| `make install` | `mcpembed` | `CGO_ENABLED=1 go install -ldflags="$(LDFLAGS)" ./cmd/pinner` |
| `make test` | `mcpembed` | `go test ./...` |
| `make mcpembed` | `templinstall generate jsbuild cssbuild` | Regenerates every embeddable asset (templ + JS + CSS) |
| `make generate` | — | `templ generate` (recurses the repo from the root; *not* `go generate ./...`) |
| `make templinstall` | — | `go install github.com/a-h/templ/cmd/templ@v0.3.1020` |
| `make clean` | — | `rm -f pinner` |

`mcpembed` is what `go generate ./mcpembed` invokes (`make -C .. mcpembed`).
It is declared `.PHONY` because a real source directory named `mcpembed/`
exists; without that, `make` would treat the directory as an up-to-date target
and skip regeneration.

The default goal is pinned to `build` so a bare `make` always produces a
binary.

## Build Pipeline

```
templinstall → generate → jsbuild → cssbuild → go build / go install
```

1. **`templinstall`** — `go install github.com/a-h/templ/cmd/templ@v0.3.1020`
   pins the templ CLI to the version declared in go.mod, so regeneration works
   on a fresh checkout without templ pre-installed.
2. **`generate`** — runs `templ generate` from the repo root. This is invoked
   deliberately as `templ generate`, **not** via `go generate ./...`, because
   templ files live in multiple packages and a single root-anchored pass
   covers every `*.templ` exactly once.
3. **`jsbuild`** — `cd packages/apps && pnpm install --frozen-lockfile && pnpm
   build`, then copies the dist JS into `internal/mcpapp/appsassets/dist/`
   where Go embeds it.
4. **`cssbuild`** — `pnpm build:css` compiles the Tailwind theme
   (`internal/mcpapp/css/input.css`) into the embedded stylesheet
   (`internal/mcpapp/css/tailwind.css`).
5. **Go build** — `CGO_ENABLED=1` with `-ldflags` injecting version info.

### Version Info (ldflags)

`LDFLAGS` injects `Version`, `GitCommit`, `GitBranch`, `BuildTime`,
`GoVersion`, `Platform`, and `Architecture` into the `build` package
(`go.lumeweb.com/pinner-cli/build`). These surface through version display
and `pinner doctor`.

### Cross-Compiling

The `Makefile` builds for the host. To target other platforms, build directly
with `GOOS`/`GOARCH`:

```bash
GOOS=linux   GOARCH=amd64 go build -o pinner-linux-amd64   ./cmd/pinner
GOOS=darwin  GOARCH=arm64 go build -o pinner-darwin-arm64  ./cmd/pinner
GOOS=windows GOARCH=amd64 go build -o pinner-windows-amd64.exe ./cmd/pinner
```

## Testing

```bash
make test                  # rebuild assets, then run the whole suite
go test ./...              # quick run (assumes assets already built)
go test ./internal/cli     # one package
go test -v ./...           # verbose
go test ./internal/cli -run TestUpload   # a specific test
```

Some tests depend on the embedded JS/CSS assets, which is why `make test`
runs `jsbuild` + `cssbuild` first. For quick Go-only checks without an asset
rebuild, `go vet ./...` / `go build ./...` are fine — but the resulting binary
may lack embedded assets.

### Test Layers

- **Go-level tests** — unit tests throughout `internal/...`, using service
  mocks and the `*WithService` helper pattern that lets tests exercise command
  logic without a live urfave context.
- **`internal/mcptest`** — a Go fake standing in for the upstream Pinner.xyz
  API at the HTTP layer (`go test ./internal/mcptest/...`).
- **`tests/sunpeak`** — integration tests that run `pinner mcp` as a real MCP
  stdio server against a fake backend.

### Mock Generation

Mocks are configured in `.mockery.yaml` and generated with mockery (run with
no arguments; the config defines interfaces, output dirs, and testify
templates). Do **not** reinstall mockery — the environment already has it.
Generated mocks are committed; after changing `.mockery.yaml` or adding an
interface, regenerate and commit the output.

## CI Lint Gate: `templ fmt`

CI runs `templ fmt .` followed by `git diff --exit-code -- '*.templ'`. A
non-canonically formatted `.templ` file fails the lint job. Always run
`templ fmt .` after editing `.templ` files and commit the reformat in the same
change.

Local mirror of the CI checks:

```bash
templ fmt . && go vet ./... && go build ./...
```

`templ fmt .` should report `changed=0` before pushing.

## SDK Bumps

`go.lumeweb.com/portal-sdk` is consumed through a local `replace` in `go.mod`.
When bumping the SDK version:

1. Bump the version / update `go.mod` (`go get` the new tag).
2. Run `make build` — the compiler reports every upstream change to absorb.
3. Fix each compile error (including struct field renames: grep all call sites,
   rename every occurrence).
4. Run `make test` — fixture failures like "unknown field X in struct literal"
   signal swagger schema drift; refresh the fixtures.
5. Repeat until clean. Absorb **all** upstream changes, even if the bump only
   targeted one feature.

When generating or updating OpenAPI client code, the repo uses
`oapi-codegen` invoked via `go run ...@latest` (consistent versioning), and
generated types must be verified against `client.gen.go` — oapi-codegen follows
the swagger spec (e.g. `[32]byte` swagger fields become `[]int`).
