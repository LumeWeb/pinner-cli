#!/usr/bin/env bash
# Build a Pinner MCP Bundle (.mcpb) for a single platform from a compiled
# pinner binary, then (optionally) upload it to the matching GitHub release.
#
# A .mcpb is a zip: a server binary + a manifest.json describing how to run it
# as a local MCP server. Because the binary is platform-specific (Go cgo), each
# OS/arch needs its own bundle. Claude Desktop runs on macOS + Windows, so we
# emit one bundle per supported desktop target.
#
# Usage (called once per platform by GoReleaser's publishers pipe):
#   mcpb-pack.sh <binary> <os> <arch> <version> [--upload]
#
# Binary layout of the produced bundle:
#   pinner-<os>-<arch>.mcpb
#   └── manifest.json      # rendered from mcpb/manifest.json.tmpl
#   └── server/
#       └── pinner[.exe]   # the compiled binary, executable bit set
set -euo pipefail

BINARY="${1:?binary path required}"
OS="${2:?os required (darwin|windows|linux)}"
ARCH="${3:?arch required (amd64|arm64)}"
VERSION="${4:?version required}"
UPLOAD="${5:-}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$REPO_ROOT/mcpb/manifest.json.tmpl"
STAGE_DIR="$REPO_ROOT/dist/mcpb/pinner-${OS}-${ARCH}"
OUT_FILE="$REPO_ROOT/dist/mcpb/pinner-${OS}-${ARCH}.mcpb"

# Windows output binary name: on disk the file carries a .exe extension (the
# toolchain requires it to be treated as an executable). The MCPB spec's
# manifest must declare the entry point/command WITHOUT the extension
# ("server/pinner"): the spec states that apps automatically append `.exe`
# on Windows when resolving the command. Declaring ".exe" in the base command
# would therefore resolve to "server/pinner.exe.exe" and fail to launch.
# So `EXE` is used only for the on-disk filename below, never for the manifest.
EXE=""
if [ "$OS" = "windows" ]; then
    EXE=".exe"
fi
# Map Go's GOOS/GOARCH to the manifest's compatibility.platforms values.
# Spec uses "darwin", "win32", "linux".
PLATFORM_OS="$OS"
if [ "$OS" = "darwin" ]; then
    PLATFORM_OS="darwin"
elif [ "$OS" = "windows" ]; then
    PLATFORM_OS="win32"
fi

echo ">> Packing Pinner MCPB for $OS/$ARCH (version $VERSION)"

# Stage the bundle directory.
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/server"

# Render the manifest. The template uses {{VERSION}} and {{OS}} markers; the
# entry point/command are always "server/pinner" (apps auto-append .exe on
# Windows), so no extension marker is rendered into the manifest.
sed -e "s/{{VERSION}}/${VERSION}/g" \
    -e "s/{{OS}}/${PLATFORM_OS}/g" \
    "$TEMPLATE" > "$STAGE_DIR/manifest.json"

# Place the binary at the declared entry point and make it executable.
cp "$BINARY" "$STAGE_DIR/server/pinner${EXE}"
chmod +x "$STAGE_DIR/server/pinner${EXE}"

# Pack the staged directory into the .mcpb archive.
# Preferred path: the official mcpb CLI (also validates the manifest). If it is
# not installed (e.g. a lean release image without node), fall back to building
# the .mcpb zip directly — a bundle is just a zip of manifest.json + server/.
if command -v mcpb >/dev/null 2>&1; then
    mcpb validate "$STAGE_DIR/manifest.json" >/dev/null
    (cd "$STAGE_DIR" && mcpb pack . "$OUT_FILE") >/dev/null
elif command -v zip >/dev/null 2>&1; then
    if ! command -v python3 >/dev/null 2>&1; then
        echo "!! neither mcpb, nor zip+python3 available; cannot build .mcpb" >&2
        exit 1
    fi
    (cd "$STAGE_DIR" && python3 -c '
import zipfile, os, sys
src = "."
out = sys.argv[1]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    for root, _, files in os.walk(src):
        for f in files:
            p = os.path.join(root, f)
            z.write(p, os.path.relpath(p, src))
' "$OUT_FILE")
else
    echo "!! no packer available (wanted mcpb or zip+python3)" >&2
    exit 1
fi

echo ">> Produced $OUT_FILE"
ls -lh "$OUT_FILE"

# Optional: attach to the GitHub release for this tag.
if [ "$UPLOAD" = "--upload" ]; then
    : "${GITHUB_TOKEN:?GITHUB_TOKEN required for release upload}"
    ASSET_NAME="pinner-mcp-${OS}-${ARCH}-${VERSION}.mcpb"
    echo ">> Uploading $ASSET_NAME to GitHub release $VERSION"
    gh release upload "${VERSION}" "$OUT_FILE" --clobber --repo LumeWeb/pinner-cli 2>&1 || true
    echo ">> Uploaded $ASSET_NAME"
fi
