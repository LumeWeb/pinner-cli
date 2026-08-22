import { readFileSync, writeFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { test, expect } from 'sunpeak/test';
import { invoke, textOf } from './helpers';

// Serial: the auth file is stateful against the SHARED fixture config. Test 1
// (auth_status) observes the seeded credential; test 2 (auth_login) writes a
// token; test 3 (auth_logout) clears it. Ordering must be deterministic and
// the final state must be restored, so the file runs serially in one worker.
test.describe.configure({ mode: 'serial' });

/**
 * Auth domain tools (auth_status / auth_login / auth_logout) driven through
 * the host-discovery contract: every call goes through invoke_tool (the
 * progressive-disclosure meta-tool) with { name, args } — the same path a
 * ChatGPT/Claude host uses to surface catalog tools. auth_status and
 * auth_logout are directly curated onto tools/list; auth_login deliberately
 * lives only behind invoke_tool (tool-surface.test.ts enforces that).
 *
 * STATE SAFETY (the tricky part): auth_login and auth_logout are LOCAL config
 * operations — they persist the edited credential to
 * fixtures/pinner-home/.config/pinner/config.yaml, which is the SHARED fixture
 * read by BOTH host projects (chatgpt/claude) in a run, and by every other
 * test file. Clearing the token (auth_logout) or swapping in a synthetic JWT
 * (auth_login) would de-authenticate the rest of the suite. So:
 *   - The destructive tests run LAST (test 3), serially.
 *   - The ORIGINAL config bytes are captured at module load, before any
 *     mutation, and restored in afterAll. afterAll runs unconditionally, so
 *     the shared fixture is never left de-authenticated even on failure.
 *
 * CONTRACT NOTES (from internal/catalogops/auth_ops.go):
 *   - auth_status returns { authenticated: bool, email?, user_id?, message? }.
 *     Its output text contains the literal word "authenticated", so
 *     isCleanSuccess (which regex-checks /authenticat/i) false-negatives —
 *     we assert structure directly, never isCleanSuccess.
 *   - auth_login is the agent-safe TOKEN variant, NOT email/password: it takes
 *     a `token` (JWT, 3 dot-separated parts), validates its shape, saves it,
 *     and returns { status: 'logged_in', message }. The interactive
 *     email/password/OTP flow is a terminal mechanism, not an MCP tool.
 *   - auth_logout is LOCAL: clears the stored token without revoking
 *     server-side API keys. Returns { status: 'logged_out', config_path,
 *     message }. The fake's login API never learns about it.
 */

// The shared fixture config, relative to this test file.
const CONFIG_PATH = fileURLToPath(
  new URL('../fixtures/pinner-home/.config/pinner/config.yaml', import.meta.url),
);

// Capture the pristine committed config BEFORE any mutation so afterAll can
// restore it byte-for-byte (no token is ever injected/removed permanently).
const ORIGINAL_CONFIG = readFileSync(CONFIG_PATH, 'utf8');

// A structurally-valid JWT (3 dot-separated non-empty segments). It is never
// asserted or verified against the fake; auth_login only checks JWT shape
// before persisting and returns logged_in.
const SYNTHETIC_JWT = 'e30.e30.c2ln';

test.afterAll(() => {
  // Restore the shared fixture so both host projects keep authenticating.
  writeFileSync(CONFIG_PATH, ORIGINAL_CONFIG);
});

test('auth_status reports authenticated as the seeded account', async ({ mcp }) => {
  // The fixture config carries the seeded token (token-e2e@example.com), so
  // auth_status must resolve against the fake and report authenticated.
  // NOTE: do NOT use isCleanSuccess here — the word "authenticated" trips its
  // false-negative regex; assert structure instead.
  const result = await invoke(mcp, 'auth_status', {});

  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  // authenticated sits under the {status, value} envelope (see real-tools.test.ts).
  expect(result).toHaveStructuredContent({ value: { authenticated: true } });

  // The fake seeds e2e@example.com (cmd/mcp-test-server), so the status must
  // surface that account email, not an authentication failure.
  expect(result).toHaveTextContent('e2e@example.com');
});

test('auth_login returns a logged_in contract', async ({ mcp }) => {
  // auth_login is the agent-safe token variant (not email/password). It
  // accepts a JWT-shaped token, validates its structure, persists it, and
  // returns { status: 'logged_in', message }. We assert the status contract,
  // never the token value (brittle).
  const result = await invoke(mcp, 'auth_login', { token: SYNTHETIC_JWT });

  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveStructuredContent({ value: { status: 'logged_in' } });
});

test('auth_logout clears the local credential (logged_out state)', async ({ mcp }) => {
  // auth_logout is LOCAL-only: it clears the stored auth token from config; it
  // does not revoke server-side API keys and does not call the fake. After it,
  // auth_status must report not authenticated.
  const logout = await invoke(mcp, 'auth_logout', {});
  expect(logout).not.toBeError();
  expect(logout).toHaveStructuredContent({ status: 'ok' });
  // Both host projects share the fixture config, so whichever project's
  // auth_logout runs first clears the token and gets {status:'logged_out'};
  // the other finds it already gone and gets {status:'not_authenticated'}.
  // Either is the correct LOCAL logout contract — assert the union.
  expect(textOf(logout)).toMatch(/logged_out|not_authenticated/);

  // Observe the post-logout contract: no token configured => authenticated:false.
  const status = await invoke(mcp, 'auth_status', {});
  expect(status).not.toBeError();
  expect(status).toHaveStructuredContent({ status: 'ok' });
  expect(status).toHaveStructuredContent({ value: { authenticated: false } });

  // afterAll restores the pristine config, so the shared fixture (and the
  // other host project) is never left de-authenticated.
});
