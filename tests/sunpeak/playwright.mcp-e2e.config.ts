import { defineConfig } from 'sunpeak/test/config';
import { join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = fileURLToPath(new URL('.', import.meta.url));

// Sunpeak MCP e2e suite driving REAL tool calls through `pinner mcp`,
// pointed at the swagger-generated fake Pinner API instead of the live
// service. This is the "generate servers, not just clients" payoff: the MCP
// server -> SDK client -> HTTP -> API stack runs end-to-end against a
// deterministic contract-accurate double, so `account_info` returns real
// account data rather than "authentication required".
//
// Two processes:
//   1. The fake API (cmd/mcp-test-server) is started by globalSetup on
//      127.0.0.1:8126 and seeds a deterministic account/token.
//   2. `pinner mcp` is booted by sunpeak with HOME isolated to
//      fixtures/pinner-home, whose config.yaml points base_endpoint and
//      auth_token at that fake.
//
// Build both binaries first:
//   go build -o bin/pinner ./cmd/pinner
//   go build -o bin/mcp-test-server ./cmd/mcp-test-server
//
// Run: npx sunpeak test -c playwright.mcp-e2e.config.ts

const FAKE_PORT = 8126;
const FIXTURE_HOME = join(__dirname, 'fixtures', 'pinner-home');

export default defineConfig({
  server: {
    command: '../../bin/pinner',
    args: ['mcp'],
    env: {
      HOME: FIXTURE_HOME,
    },
  },
  globalSetup: './mcp-e2e-setup.mjs',
  testDir: 'mcp-e2e',
  timeout: 120_000,
});
