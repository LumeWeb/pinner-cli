# syntax=docker/dockerfile:1
#
# Pinner — Docker-first distribution image.
#
# Build constraint (critical): the vault cache uses go-sqlite3, a cgo driver
# that compiles to a runtime stub when CGO_ENABLED=0. This image MUST build with
# CGO enabled against musl so the runtime is a fully static, dependency-free
# binary that runs on Alpine/scratch without libc glibc or libsqlite3.
#
# The image is published by GoReleaser (`dockers:` + `docker_manifests:` in
# .goreleaser.yaml) from the same dist/ artifact as the .deb/.rpm/tar.gz, so
# the shipped OCI image and shipped binaries are byte-identical builds.

# ---- Build stage: static musl binary with cgo ----
FROM golang:1.26-alpine AS build

# golang:alpine ships musl; gcc + musl-dev provide the C toolchain for
# go-sqlite3. tzdata/ca-certificates are not needed at build time.
RUN apk add --no-cache gcc musl-dev

WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Version is injected by GoReleaser via ARG. Default to a dev marker so plain
# `docker build` (no --build-arg) still produces a coherent version.
ARG VERSION=0.0.0-dev
ARG GIT_COMMIT=""
ARG GIT_BRANCH=""
ARG BUILD_TIME=""

# -tags netgo osusergo: use the pure-Go net resolver and os/user, avoiding
# libc-dependent resolv/nss lookups so the binary is fully static.
# CGO_ENABLED=1 with the alpine (musl) toolchain gives a static binary.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 CGO_CFLAGS="-O2 -g" \
    go build -trimpath -tags "netgo osusergo" \
      -ldflags "-s -w \
        -X 'go.lumeweb.com/pinner-cli/build.Version=${VERSION}' \
        -X 'go.lumeweb.com/pinner-cli/build.GitCommit=${GIT_COMMIT}' \
        -X 'go.lumeweb.com/pinner-cli/build.GitBranch=${GIT_BRANCH}' \
        -X 'go.lumeweb.com/pinner-cli/build.BuildTime=${BUILD_TIME}' \
        -X 'go.lumeweb.com/pinner-cli/build.GoVersion=$(go version)' \
        -X 'go.lumeweb.com/pinner-cli/build.Platform=linux' \
        -X 'go.lumeweb.com/pinner-cli/build.Architecture=amd64'" \
      -o /out/pinner ./cmd/pinner

# ---- Runtime stage: minimal, non-root ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S pinner \
    && adduser -S pinner -G pinner \
    && mkdir -p /app /data \
    && chown -R pinner:pinner /app /data

WORKDIR /app

COPY --from=build /out/pinner /usr/local/bin/pinner
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Run as non-root. PINNER_HOME defaults to the config dir; a PaaS may mount a
# volume here for the SQLite vault cache + config.yaml persistence.
ENV PINNER_HOME=/data \
    PORT=8080 \
    MCP_HOST=0.0.0.0

USER pinner

EXPOSE 8080

# OCI ownership annotation for the Official MCP Registry:
# io.modelcontextprotocol.server.name MUST equal the server.json `name`.
LABEL org.opencontainers.image.title="Pinner"
LABEL org.opencontainers.image.description="CLI and MCP server for Pinner.xyz"
LABEL org.opencontainers.image.vendor="Pinner.xyz"
LABEL org.opencontainers.image.licenses="MIT"
LABEL io.modelcontextprotocol.server.name="xyz.pinner/mcp"

# Default: serve streamable-HTTP MCP on $PORT. Override with any `pinner`
# subcommand, e.g. `docker run ... pinner mcp` (stdio) or a pure CLI arg.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
