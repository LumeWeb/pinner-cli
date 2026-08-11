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

PKG := go.lumeweb.com/pinner-cli/build

LDFLAGS := -X '$(PKG).Version=$(VERSION)' \
           -X '$(PKG).GitCommit=$(GIT_COMMIT)' \
           -X '$(PKG).GitBranch=$(GIT_BRANCH)' \
           -X '$(PKG).BuildTime=$(BUILD_TIME)' \
           -X '$(PKG).GoVersion=$(GO_VERSION)' \
           -X '$(PKG).Platform=$(PLATFORM)' \
           -X '$(PKG).Architecture=$(ARCH)'

# generate regenerates checked-in code from source generators (templ pages via
# the //go:generate templ generate directive in internal/mcp/embed.go). It
# runs before build/install so a change to a *.templ file is never built with
# stale generated *_templ.go output. Requires the templ CLI on PATH.
generate:
	go generate ./...

build: generate
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner

install: generate
	CGO_ENABLED=1 go install -ldflags="$(LDFLAGS)" ./cmd/pinner

clean:
	rm -f pinner
