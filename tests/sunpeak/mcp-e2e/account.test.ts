import { test, expect } from 'sunpeak/test';
import { invoke } from './helpers';

/**
 * Account domain tool (account_subscription) driven through the
 * host-discovery contract: every call goes through invoke_tool (the
 * progressive-disclosure meta-tool) with { name, args } — the same path a
 * ChatGPT/Claude host uses to surface catalog tools.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/account/server_test.go validate the
 * same endpoints via `go test -race ./internal/mcptest/...`.
 *
 * Only read-only account ops are covered here: the account email/password
 * mutation ops (account_update_email / account_update_password) are NOT part
 * of the MCP surface — they pass credentials through the LLM channel and were
 * superseded by the hosted-browser OOB tools (account_email_change /
 * account_password_update), so they are dropped from the compiled catalog
 * surface (see catalogsurface.go). Account mutation stays covered by the
 * Go-side fake-API unit tests and the CLI path.
 */

test('account_subscription reports the free, not-subscribed status', async ({ mcp }) => {
  // The seeded account is free: is_subscribed=false, no plan period/gateway.
  const result = await invoke(mcp, 'account_subscription', {});

  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveStructuredContent({ value: { is_subscribed: false } });
});
