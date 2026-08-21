import { defineConfig } from 'sunpeak/test/config';

// Canonical sunpeak config for an external (Go) MCP server: a self-contained
// tests/sunpeak/ suite auto-discovered by `sunpeak test` from the repo root.
//
// Server: the real `pinner mcp` binary over stdio. Build it first with
// `go build -o bin/pinner ./cmd/pinner` (see the repo root npm scripts).
//
// The suite covers two fixture levels:
//   - `mcp`        protocol primitives (listTools / callTool / listResources)
//   - `inspector`  host-iframe rendering of the ui:// MCP Apps (real browser)
//
// The raw-HTTP transport suites live in tests/sunpeak/http and tests/sunpeak/oauth
// with their own plain-Playwright configs; they are OUTSIDE this testDir so
// `sunpeak test` runs only the stdio protocol + render suite.
export default defineConfig({
  server: {
    command: '../../bin/pinner',
    args: ['mcp'],
  },
  timeout: 120_000,
  testDir: 'tests',
});
