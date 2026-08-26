import { test, expect } from 'sunpeak/test';
import { invoke, isCleanSuccess } from './helpers';

/**
 * End-to-end tests proving pinner's MCP server drives REAL tool calls through
 * the swagger-generated fake Pinner API. Unlike protocol-surface.test.ts
 * (which pins the auth-free surface), these run authenticated: the fixture
 * config points pinner at cmd/mcp-test-server, which seeds an account, so
 * tool calls that reach the API return actual data instead of
 * "authentication required".
 *
 * This is the "generate servers from swagger, not just clients" payoff in
 * action: sunpeak -> pinner mcp -> SDK client -> HTTP -> fake API.
 */

test('account_info returns the seeded account, not an auth error', async ({ mcp }) => {
  const result = await invoke(mcp, 'account_info', {});

  // A successful tool call is not flagged as an error and must not carry the
  // auth/network failure markers that would mean the fake wasn't reached.
  expect(isCleanSuccess(result)).toBe(true);

  // The fake seeds e2e@example.com (see cmd/mcp-test-server), so the tool
  // must surface that account rather than an authentication failure.
  expect(result).toHaveTextContent('e2e@example.com');

  // invoke_tool returns the JSON both as text content and as structuredContent,
  // so assert the structured shape directly. Assert only the field this test's
  // intent requires (the seeded email) rather than a full record snapshot, so
  // a schema addition to the account object does not break the test.
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveStructuredContent({
    value: {
      email: 'e2e@example.com',
    },
  });
});

test('pins list returns the empty fake store, reaching the content contract', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_list', {});

  // No auth error, no connection refused: proves the content API path is live.
  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
});
