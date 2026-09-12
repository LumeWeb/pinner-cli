.PHONY: build install clean generate templinstall assets genappmanifest

# A bare `make` must produce a binary, not just regenerate templ output.
# generate was added above build, which silently made it (not build) the
# default goal. Pin the default explicitly so reordering rules later can't
# regress it again.
.DEFAULT_GOAL := build

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION := $(shell go version | sed 's/go version //')
PLATFORM := $(shell go env GOOS)
ARCH := $(shell go env GOARCH)

# Build tags. sqlite_fts5 compiles FTS5 into mattn/go-sqlite3 (trigram
# tokenizer), which powers the vault name full-text search. Without it FTS5 is
# absent and vault search silently falls back to plain LIKE matching.
TAGS := sqlite_fts5

PKG := go.lumeweb.com/pinner-cli/build

LDFLAGS := -X '$(PKG).Version=$(VERSION)' \
           -X '$(PKG).GitCommit=$(GIT_COMMIT)' \
           -X '$(PKG).GitBranch=$(GIT_BRANCH)' \
           -X '$(PKG).BuildTime=$(BUILD_TIME)' \
           -X '$(PKG).GoVersion=$(GO_VERSION)' \
           -X '$(PKG).Platform=$(PLATFORM)' \
           -X '$(PKG).Architecture=$(ARCH)'

# generate regenerates the templ-derived *_templ.go files. It runs before
# build/install so a change to a *.templ file is never built with stale
# generated output. Requires the templ CLI on PATH.
#
# We invoke `templ generate` directly from the repo root (not `go generate
# ./...`) deliberately: templ files live in two packages (internal/mcp and
# mcpapp) and each carries a //go:generate templ generate directive.
# Because `templ generate` recurses the whole repo by default, running it via
# `go generate ./...` executes that directive twice from two different
# directories (go:generate runs the command with the package dir as cwd),
# which is redundant and, depending on the templ version and tree state, can
# fail trying to write a *_templ.go for a templ source in the "wrong" package
# dir. A single `templ generate` anchored at the repo root covers every *.templ
# file exactly once and always emits output next to its source.
generate:
	templ generate

# templinstall installs the templ CLI used by `generate`, pinned to the version
# declared in go.mod (github.com/a-h/templ). Runs from the repo root. Part of
# `assets` so a fresh checkout can regenerate templates without templ
# pre-installed.
templinstall:
	go install github.com/a-h/templ/cmd/templ@v0.3.1020

# jsbuild builds the MCP App JS bundles (packages/apps via tsdown) into
# self-contained ESM files and copies them to mcpapp/appsassets/dist/
# so Go embeds them, then regenerates the mcpcanvas AssetSource manifest
# (mcpapp/appsassets/manifest.json via go run ./build/genappmanifest)
# against the freshly copied bundles. Requires pnpm on PATH. Go build/test
# embed these bundles (and the manifest), so jsbuild must run before any go
# build/test.
jsbuild:
	cd packages/apps && CI=true pnpm install --frozen-lockfile && pnpm build && cd ../.. && \
	mkdir -p mcpapp/appsassets/dist && \
	cp packages/apps/dist/*.js mcpapp/appsassets/dist/ && \
	GOFLAGS=-mod=mod go run ./build/genappmanifest

# genappmanifest regenerates ONLY the mcpcanvas AssetSource manifest
# (mcpapp/appsassets/manifest.json) against the bundles already in
# appsassets/dist/. Used by jsbuild (which is what assets/CI chain);
# usable standalone when iterating on bundles without a full JS build.
genappmanifest:
	GOFLAGS=-mod=mod go run ./build/genappmanifest

# cssbuild compiles the MCP Apps Tailwind theme (mcpapp/css/input.css)
# into the embedded stylesheet (mcpapp/css/tailwind.css) that every
# ui:// app inlines. Requires pnpm on PATH. Must run before any go build so the
# go:embed picks up the freshly compiled CSS.
cssbuild:
	pnpm build:css

# assets regenerates all embeddable assets the MCP Apps surface depends on:
# installs the templ CLI (templinstall), regenerates the templ *_templ.go files
# (generate), builds the MCP App JS bundles (jsbuild, which runs
# `pnpm install --frozen-lockfile` then `pnpm build`) and compiles the Tailwind
# stylesheet (cssbuild). It is the single target that regenerates all
# embeddable assets. Run `go run ./build/genappmanifest` standalone to
# regenerate only the manifest. Must run before any go build/test so the
# go:embed directives pick up freshly built assets.
assets: templinstall generate jsbuild cssbuild

build: assets
	CGO_ENABLED=1 go build -tags="$(TAGS)" -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner

install: assets
	CGO_ENABLED=1 go install -tags="$(TAGS)" -ldflags="$(LDFLAGS)" ./cmd/pinner

test: assets
	go test -tags "$(TAGS)" ./...

clean:
	rm -f pinner
