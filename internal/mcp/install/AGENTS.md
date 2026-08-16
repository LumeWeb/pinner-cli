# Supported agents & keeping them in sync

`pinner mcp install` writes the pinner MCP server entry into coding agents' config files. This
document lists the supported agents, why that set was chosen, and how to keep it accurate.

Source of the per-agent schema intelligence is a Go port of the
[schema](https://github.com/neon-solutions/add-mcp/blob/main/src/agents.ts) used by the `add-mcp`
CLI. Pinner deliberately does **not** regenerate from `add-mcp` at build time; it copies the table
and transforms into Go (`internal/mcp/install/*.go`) and stays in sync by diffing periodically.

## Supported agents (8)

| Key | Display | Config file | Format | Server key | HTTP |
|-----|---------|-------------|--------|-----------|------|
| `claude-code` | Claude Code | `~/.claude.json` (global), `.mcp.json` (project) | json | `mcpServers` | yes |
| `claude-desktop` | Claude Desktop | `<appData>/Claude/claude_desktop_config.json` | json | `mcpServers` | no (stdio only) |
| `vscode` | VS Code | `~/.config/Code/User/mcp.json` (linux), `.vscode/mcp.json` (project) | json | `servers` | yes |
| `cursor` | Cursor | `~/.cursor/mcp.json`, `.cursor/mcp.json` (project) | json | `mcpServers` | yes |
| `codex` | Codex | `$CODEX_HOME\|~/.codex/config.toml`, `.codex/config.toml` | toml | `mcp_servers` | yes |
| `gemini-cli` | Gemini CLI | `~/.gemini/settings.json`, `.gemini/settings.json` | json | `mcpServers` | yes |
| `opencode` | OpenCode | `~/.config/opencode/opencode.jsonc`, `opencode.jsonc` | json | `mcp` | yes |
| `zed` | Zed | `<appData>/Zed/settings.json`, `.zed/settings.json` | json | `context_servers` | yes |

Path resolution is OS-dependent and env-aware (`CODEX_HOME`, `APPDATA`, `HOME`, `~` expansion).
See `agents.go` for the exact helpers (`vscodeUserPath`, `zedPath`, `codexConfigPath`,
`claudeDesktopPath`, `homeDir`).

## Why this set

The set is intentionally **smaller** than `add-mcp`'s 19 targets. Only agents that pinner users
plausibly use AND that we can verify end-to-end are included. A smaller set means fewer per-agent
transforms to hand-maintain and verify, and less drift surface. Add a new agent only when there is
a user need and a way to test it.

## Keeping in sync with add-mcp

`add-mcp` is a moving reference. When you bump support or suspect drift:

1. **Diff the agent table.** Compare each entry in `agents.go` against
   `add-mcp/src/agents.ts` for new agents, moved config paths, format changes, or key renames.
2. **Focus on the data, not the transforms.** Paths, formats, and config-keys are declarative and
   cheap to keep current. The per-agent `transformConfig` functions are real logic — they only
   change when a client changes its config schema (e.g. OpenCode's `local`/`remote` discriminator,
   Codex approval modes).
3. **Do not regenerate from add-mcp at runtime or build time.** It is TypeScript; the transforms
   cannot produce Go behavior, and running their build in CI adds dependency weight with no gain.
   The Go table is the source of truth.

## Adding an agent (checklist)

1. Add the `AgentKey` constant and table entry in `agents.go` (paths/format/configKey/transports).
2. Add the `Transform` in `transforms.go` (build a fresh map with only recognized keys).
3. Add path helpers in `agents.go` if the agent has OS/env-dependent paths.
4. Add transform + writer round-trip tests in `transforms_test.go` / `writer_test.go`.
5. Update the list above and the `--agent` help text in `internal/cli/mcp_install.go`.
