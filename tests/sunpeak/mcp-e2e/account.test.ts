import { test, expect } from 'sunpeak/test';
import { invoke } from './helpers';

/**
 * Account domain tools (account_subscription / account_update_email /
 * account_update_password) driven through the host-discovery contract: every
 * call goes through invoke_tool (the progressive-disclosure meta-tool) with
 * { name, args } — the same path a ChatGPT/Claude host uses to surface
 * catalog tools.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/account/server_test.go validate the
 * same endpoints via `go test -race ./internal/mcptest/...`.
 *
 * STATE SAFETY: account.update_email and account.update_password MUTATE the
 * fake's in-memory account state, and they run against the SHARED seeded
 * account (e2e@example.com) whose token the fixture config references across
 * every test file. To avoid breaking sibling tests regardless of worker
 * ordering, the mutating tests run serially and RESTORE the seeded account's
 * email and password at the end (email change -> password change -> email
 * change back -> password restore). The seeded account's password is
 * account.DefaultPassword ("password"), set by cmd/mcp-test-server Seed().
 */
test.describe.configure({ mode: 'serial' });

// The seeded account (cmd/mcp-test-server) and its deterministic password.
const SEED_EMAIL = 'e2e@example.com';
const SEED_PASSWORD = 'password';

test('account_subscription reports the free, not-subscribed status', async ({ mcp }) => {
  // The seeded account is free: is_subscribed=false, no plan period/gateway.
  const result = await invoke(mcp, 'account_subscription', {});

  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveStructuredContent({ value: { is_subscribed: false } });
});

test('account_update_email changes and restores the account email', async ({ mcp }) => {
  const fresh = 'e2e-once@example.com';

  // Change to a fresh address using the seeded password.
  const changed = await invoke(mcp, 'account_update_email', {
    email: fresh,
    password: SEED_PASSWORD,
  });
  expect(changed).not.toBeError();
  expect(changed).toHaveStructuredContent({ status: 'ok' });
  expect(changed).toHaveStructuredContent({ value: { email: fresh } });

  // account_info now reports the new address (server persisted the change).
  const info = await invoke(mcp, 'account_info', {});
  expect(info).not.toBeError();
  expect(info).toHaveTextContent(fresh);

  // Restore the seed email using the (unchanged) seeded password.
  const restored = await invoke(mcp, 'account_update_email', {
    email: SEED_EMAIL,
    password: SEED_PASSWORD,
  });
  expect(restored).not.toBeError();
  expect(restored).toHaveStructuredContent({ status: 'ok' });
  expect(restored).toHaveStructuredContent({ value: { email: SEED_EMAIL } });
});

test('account_update_password verifies current password, then restores', async ({ mcp }) => {
  const next = 'a-new-password';

  // Bounce: set a new password, then reset to the seeded default so the
  // shared fixture account stays usable by sibling tests.
  const set = await invoke(mcp, 'account_update_password', {
    current_password: SEED_PASSWORD,
    new_password: next,
  });
  expect(set).not.toBeError();
  expect(set).toHaveStructuredContent({ status: 'ok' });

  // Wrong current password must be rejected cleanly.
  const wrong = await invoke(mcp, 'account_update_password', {
    current_password: 'definitely-not-the-password',
    new_password: 'x',
  });
  expect(wrong).not.toBeError();
  expect(wrong).toHaveStructuredContent({ status: 'error' });

  // Restore the seeded default password.
  const restore = await invoke(mcp, 'account_update_password', {
    current_password: next,
    new_password: SEED_PASSWORD,
  });
  expect(restore).not.toBeError();
  expect(restore).toHaveStructuredContent({ status: 'ok' });
});
