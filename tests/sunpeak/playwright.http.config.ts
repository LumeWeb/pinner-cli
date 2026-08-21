import { defineConfig, devices } from '@playwright/test';

// Raw HTTP transport tests for the pinner MCP server's static-bearer auth gate
// (pinner mcp --http --auth-token). Plain Playwright is used here — not the
// sunpeak `mcp`/`inspector` fixtures — because these tests exercise the raw
// HTTP wire: an unauthenticated request must be a 401, an authed one is served,
// and /healthz is open. Playwright's built-in webServer starts and health-checks
// the pinned binary.
//
// Server: the real `pinner mcp` HTTP server on 127.0.0.1:8125 with a static
// shared-secret bearer token. Build the binary first:
//   go build -o ../../bin/pinner ./cmd/pinner

const PORT = 8125;
const SECRET = process.env.PINNER_TEST_SECRET ?? 'sunpeak-test-secret';

export default defineConfig({
  testDir: 'http',
  timeout: 60_000,
  use: { trace: 'on-first-retry' },
  webServer: {
    command: `../../bin/pinner mcp --http --host 127.0.0.1 --port ${PORT} --auth-token ${SECRET}`,
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
