.PHONY: build install clean generate

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
# internal/mcpapp) and each carries a //go:generate templ generate directive.
# Because `templ generate` recurses the whole repo by default, running it via
# `go generate ./...` executes that directive twice from two different
# directories (go:generate runs the command with the package dir as cwd),
# which is redundant and, depending on the templ version and tree state, can
# fail trying to write a *_templ.go for a templ source in the "wrong" package
# dir. A single `templ generate` anchored at the repo root covers every *.templ
# file exactly once and always emits output next to its source.
generate:
	templ generate

# jsbuild builds the MCP App JS bundles (packages/apps via tsdown) into
# self-contained ESM files and copies them to internal/mcpapp/appsassets/dist/
# so Go embeds them. Requires pnpm on PATH. Go build/test embed these bundles,
# so jsbuild must run before any go build/test.
jsbuild:
	cd packages/apps && pnpm install --frozen-lockfile && pnpm build && cd ../.. && \
	mkdir -p internal/mcpapp/appsassets/dist && \
	cp packages/apps/dist/*.js internal/mcpapp/appsassets/dist/

# cssbuild compiles the MCP Apps Tailwind theme (internal/mcpapp/css/input.css)
# into the embedded stylesheet (internal/mcpapp/css/tailwind.css) that every
# ui:// app inlines. Requires pnpm on PATH. Must run before any go build so the
# go:embed picks up the freshly compiled CSS.
cssbuild:
	pnpm build:css

build: generate jsbuild cssbuild
	CGO_ENABLED=1 go build -tags="$(TAGS)" -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner

install: generate jsbuild cssbuild
	CGO_ENABLED=1 go install -tags="$(TAGS)" -ldflags="$(LDFLAGS)" ./cmd/pinner

test: jsbuild cssbuild
	go test -tags "$(TAGS)" ./...

clean:
	rm -f pinner
