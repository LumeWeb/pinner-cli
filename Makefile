.PHONY: build install clean

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

build:
	go build -ldflags="$(LDFLAGS)" -o pinner ./cmd/pinner

install:
	go install -ldflags="$(LDFLAGS)" ./cmd/pinner

clean:
	rm -f pinner
