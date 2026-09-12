.PHONY: build install clean generate templinstall assets ensure-canvasassets

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
# ./...`) deliberately: templ files live in internal/mcp and carry a
# //go:generate templ generate directive. Because `templ generate` recurses
# the whole repo by default, running it via
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

# ensure-canvasassets guarantees the go.lumeweb.com/pinner/canvasassets embed
# inputs (appsassets bundles + mcpcanvas manifest + compiled Tailwind theme)
# exist at whatever location this build resolves the pinner module from —
# a pinned module version (cache) or a checkout. The inputs are regenerated,
# not committed, so every consumer runs pinner's shared staging script; this
# CLI delegates to it (single DRY implementation, identical for the hosted
# server/plugin consumer). Requires pnpm on PATH for the JS bundle/CSS build.
ensure-canvasassets:
	@scripts/ensure-canvasassets-wrapper.sh

# assets regenerates what this CLI's own build needs: installs the templ CLI
# (templinstall), regenerates the templ *_templ.go files (generate), and stages
# the canvasassets embed inputs imported from the pinner module
# (ensure-canvasassets). Go build/test needs no local JS/CSS regeneration or
# bundle production beyond what pinner's own script performs.
assets: ensure-canvasassets templinstall generate

build: assets
	CGO_ENABLED=1 go build -tags="$(TAGS)" -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner

install: assets
	CGO_ENABLED=1 go install -tags="$(TAGS)" -ldflags="$(LDFLAGS)" ./cmd/pinner

test: assets
	go test -tags "$(TAGS)" ./...

clean:
	rm -f pinner
