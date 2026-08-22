// Playwright globalSetup for the Sunpeak MCP e2e suite.
//
// Boots the swagger-generated fake Pinner API (cmd/mcp-test-server) on
// 127.0.0.1:8125 BEFORE the pinner MCP server starts, so that when a tool
// call reaches the API it talks to the fake instead of the live service.
// The fake seeds a deterministic account/token that the fixture config in
// fixtures/pinner-home references.
//
// Playwright runs globalSetup once before all tests and before webServers,
// so ordering is guaranteed. Tears the process down after the run.
import { spawn } from 'child_process';
import { existsSync, openSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const PORT = 8125;
const HEALTH_URL = `http://127.0.0.1:${PORT}/pins`; // content route, no auth required
const BIN = join(__dirname, '..', '..', 'bin', 'mcp-test-server');

let child;

export default async function () {
  if (!existsSync(BIN)) {
    throw new Error(
      `mcp-test-server binary not found at ${BIN}. Build it first with ` +
        `'go build -o bin/mcp-test-server ./cmd/mcp-test-server'.`
    );
  }

  // If a valid fake is already answering on the port (a reused server from a
  // prior local run), adopt it instead of spawning a second instance that
  // would collide on the bind. This keeps local repeat runs race-free.
  if (await isHealthy()) {
    return;
  }

  const log = openSync(join(__dirname, '.mcp-e2e-fake.log'), 'a');
  // Redirect the fake's stdio to a log file rather than inheriting Playwright's
  // stdout. Inheriting keeps the output pipe open after the run ends and
  // orphans the child, which makes the harness appear to hang on teardown.
  child = spawn(BIN, ['--port', String(PORT)], {
    stdio: ['ignore', log, log],
    detached: true,
  });

  // Wait for the fake to accept requests (bounded poll).
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    if (await isHealthy()) {
      return; // listener is up
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`mcp-test-server not ready on :${PORT} within 20s`);
}

// True when something answers at HEALTH_URL. The content route returns 200
// when unauthenticated; 401/501 still prove the listener is up.
async function isHealthy() {
  try {
    const res = await fetch(HEALTH_URL);
    return res.ok || res.status === 401 || res.status === 501;
  } catch {
    return false;
  }
}

export async function teardown() {
  if (child && child.exitCode === null) {
    // detached: true puts the fake in its own process group; kill the whole
    // group so the server and any children are reaped.
    try {
      process.kill(-child.pid, 'SIGTERM');
    } catch {
      child.kill();
    }
  }
  child = null;
}
