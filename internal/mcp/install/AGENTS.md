# Supported agents & keeping them in sync

`pinner mcp install` writes the pinner MCP server entry into coding agents' config files. This
document lists the supported agents, why that set was chosen, and how to keep it accurate.

Source of the per-agent schema intelligence is a Go port of the
[schema](https://github.com/neon-solutions/add-mcp/blob/main/src/agents.ts) used by the `add-mcp`
CLI. Pinner deliberately does **not** regenerate from `add-mcp` at build time; it copies the table
and transforms into Go (`internal/mcp/install/*.go`) and stays in sync by diffing periodically.

## Supported agents (19)

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
| `antigravity` | Antigravity | `~/.gemini/config/mcp_config.json` | json | `mcpServers` | yes |
| `cline` | Cline (VS Code) | `<vscode>/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` | json | `mcpServers` | yes |
| `cline-cli` | Cline CLI | `$CLINE_DIR\|~/.cline/data/settings/cline_mcp_settings.json` | json | `mcpServers` | yes |
| `goose` | Goose | `<appData>/Block/goose/config/config.yaml` (win), `~/.config/goose/config.yaml` | yaml | `extensions` | yes |
| `github-copilot-cli` | GitHub Copilot CLI | `$XDG_CONFIG_HOME\|~/.copilot/mcp-config.json`, `.vscode/mcp.json` (project) | json | `mcpServers`/`servers` | yes |
| `grok-build` | Grok Build | `$GROK_HOME\|~/.grok/config.toml`, `.grok/config.toml` | toml | `mcp_servers` | yes |
| `kilo-code` | Kilo Code | `<xdg>/.config/kilo/kilo.json`, `kilo.json` (project) | json | `mcp` | yes |
| `kimi-code` | Kimi Code | `$KIMI_CODE_HOME\|~/.kimi-code/mcp.json`, `.kimi-code/mcp.json` | json | `mcpServers` | yes |
| `kiro-cli` | Kiro CLI | `~/.kiro/settings/mcp.json`, `.kiro/settings/mcp.json` | json | `mcpServers` | yes |
| `mcporter` | MCPorter | `~/.mcporter/mcporter.json`, `config/mcporter.json` | json | `mcpServers` | yes |
| `windsurf` | Windsurf | `~/.codeium/windsurf/mcp_config.json` | json | `mcpServers` | yes |

Path resolution is OS- and env-aware (`$CODEX_HOME`, `$CLINE_DIR`, `$GROK_HOME`, `$KIMI_CODE_HOME`,
`$XDG_CONFIG_HOME`, `$APPDATA`, `$HOME`, `~` expansion). Several agents share a common VS Code /
AppData base; see `agents.go` for the exact helpers (`vscodeUserPath`, `xdgConfigHome`, `codexConfigPath`,
`claudeDesktopPath`, `gooseConfigPath`, `clineCliConfigPath`, `grokConfigPath`, `kimiConfigPath`,
`kiloConfigPath`, `homeDir`, and friends).

## Why this set

The set mirrors the full 19-target surface of `add-mcp`, so `pinner mcp install` is a drop-in for
the tool users already reach for. Keeping parity means a user choosing any of these agents gets the
same config written, and the per-agent transforms are a faithful Go port that stays in sync by
periodic diff with `add-mcp/src/agents.ts`.

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

## Divergences from add-mcp (deliberate)

- **Timeout fields are dropped.** The reference emits per-agent request timeouts (grok
  `tool_timeout_sec`, kilo `timeout`, kimi `toolTimeoutMs`, kiro `timeout`) from a `timeout`
  capability. Pinner's installer does not collect a timeout, so `McpServerConfig` carries no
  `Timeout` field and these agents fall back to their own defaults. Re-introducing the field would
  recreate a config value nothing in the CLI can populate.
- **Global/local path resolution is single-path, not candidate-based.** The reference's
  `resolveConfigPath` picks the first *existing* config among several candidates (e.g. kilo's
  `kilo.jsonc`/`kilo.json` chain, opencode's `.json` fallback). Pinner writes to the single
  declared `ConfigPath()`/`LocalConfigPath` per agent.
- **Codex approval is opt-in.** Auto-approve mode is emitted only when `AutoApproveSet` is true;
  the reference likewise leaves the entry untouched unless auto-approve was requested.

## Adding an agent (checklist)

1. Add the `AgentKey` constant and entry to `AllAgents` in `types.go`.
2. Add the table entry in `agents.go` (paths/format/configKey/transports).
3. Add the `Transform` in `transforms.go` (build a fresh map with only recognized keys).
4. Add path helpers in `agents.go` if the agent has OS/env-dependent paths.
5. Add transform + writer round-trip tests in `transforms_test.go` / `writer_test.go`, and
   path-helper tests in `paths_test.go`.
6. The CLI `--agent` help text and error messages derive from `install.AllAgents`, so no manual
   list edit is needed — just update the table above.
