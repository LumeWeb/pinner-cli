# MCP Bundles (.mcpb)

Pinner ships as an MCP server (`pinner mcp`) and is also distributed as **MCP
Bundles** — single-file `.mcpb` packages that install the server with one click
in Claude Desktop for macOS/Windows and other MCPB-supporting apps.

## What a bundle is

A `.mcpb` is a ZIP archive containing:

```
pinner-<os>-<arch>.mcpb
├── manifest.json      # rendered at build time from mcpb/manifest.json.tmpl
└── server/
    └── pinner[.exe]   # the compiled pinner binary (runs `pinner mcp`)
```

The manifest describes the server (`server.type: "binary"`, entry point
`pinner mcp`), the required user config (Pinner API token), the platform
compatibility, and links to the applicable privacy policy. On install, the app
prompts the user for their API token and registers the server.

> `privacy_policies` is required by the manifest spec because the server
> connects to the Pinner service (pinner.xyz), which processes user data. It
> points at the hosted privacy policy at `/privacy-policy/`.

## Why per-platform bundles

The binary is platform-specific (Go cgo builds separate executables for each
OS/arch), and a bundle contains an executable. So there is one `.mcpb` per
supported desktop target: darwin-arm64, darwin-amd64, windows-amd64,
windows-arm64 (plus linux-* for desktop Linux).

## How bundles are built

Bundles are produced automatically during a GoReleaser release and attached to
the GitHub release as assets:

1. GoReleaser cross-compiles all platform binaries into `dist/`.
2. A custom publisher (`publishers:` in `.goreleaser.yaml`) invokes
   `scripts/mcpb-pack.sh` once per platform.
3. The script renders the manifest from `mcpb/manifest.json.tmpl`, stages the
   bundle directory, packs it into a `.mcpb`, and uploads it to the release.

The release job (`release.yml`) passes `PROJECT_DIR` into the goreleaser-cross
container so the publisher can resolve `scripts/mcpb-pack.sh`.

## Building manually

```bash
# Build a binary, then pack a bundle for one platform:
./scripts/mcpb-pack.sh /path/to/pinner darwin arm64 v0.2.1
# optionally upload to the v0.2.1 release:
./scripts/mcpb-pack.sh /path/to/pinner darwin arm64 v0.2.1 --upload
```

The pack script prefers the official `mcpb` CLI (`npm i -g @anthropic-ai/mcpb`)
for validation/packing, and falls back to building the ZIP directly with
python3 if `mcpb` is unavailable.

## Signing

Bundles are currently unsigned. To sign future releases, run `mcpb sign` with a
code-signing certificate (`--cert`/`--key`) supplied via CI secrets; the
signature is a detached PKCS#7 block appended to the file.
