#!/usr/bin/env bash
# CLI-side wrapper over pinner's shared DRY ensure-canvasassets.sh.
#
# Stage the go.lumeweb.com/pinner/canvasassets embed inputs (appsassets bundles,
# mcpcanvas manifest, compiled Tailwind theme) wherever this build resolves the
# pinner module (pinned pseudo-version in the module cache, or a checkout) —
# they are regenerated, not committed, so they must be produced in place before
# `go build`/`go test`. Delegating to pinner's own script keeps the staging
# mechanism identical for every consumer (CLI, hosted server/plugin).
set -euo pipefail

MODULE="go.lumeweb.com/pinner"

# Ensure the module source is actually present before we ask go list for its
# dir (a fresh checkout may not have downloaded it yet).
go mod download "$MODULE" 2>/dev/null || true

# Resolve the directory Go will compile the module from.
DIR="$(go list -m -f '{{.Dir}}' "$MODULE" 2>/dev/null || true)"
if [ -z "$DIR" ] || [ ! -d "$DIR" ]; then
    echo "!! ensure-canvasassets: cannot resolve $MODULE dir; run 'go mod download go.lumeweb.com/pinner'" >&2
    exit 1
fi

if [ ! -f "$DIR/scripts/ensure-canvasassets.sh" ]; then
    echo "!! ensure-canvasassets: $MODULE is pinned before the shared staging script existed; bump go.lumeweb.com/pinner in go.mod" >&2
    exit 1
fi

# shellcheck disable=SC1090
source "$DIR/scripts/ensure-canvasassets.sh" "$MODULE"
