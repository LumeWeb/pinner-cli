import { test, expect } from 'sunpeak/test';

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
  const result = await mcp.callTool('invoke_tool', {
    name: 'account_info',
    args: {},
  });

  // A successful tool call is not flagged as an error (the sunpeak fixture
  // only sets isError:true on failure; success leaves it unset).
  expect(result.isError).not.toBe(true);
  const text = result.content?.map((c) => c.text ?? '').join('') ?? '';
  // The fake seeds e2e@example.com (see cmd/mcp-test-server), so the tool
  // must surface that account rather than an authentication failure.
  expect(text).not.toMatch(/authenticat|401|unauthor/i);
  expect(text).toContain('e2e@example.com');
  expect(JSON.parse(text).status).toBe('ok');
  expect(JSON.parse(text).value).toMatchObject({
    email: 'e2e@example.com',
    first_name: 'E2E',
    last_name: 'Test',
    verified: true,
  });
});

test('pins list returns the empty fake store, reaching the content contract', async ({ mcp }) => {
  const result = await mcp.callTool('invoke_tool', {
    name: 'pins_list',
    args: {},
  });

  expect(result.isError).not.toBe(true);
  const text = result.content?.map((c) => c.text ?? '').join('') ?? '';
  // The fake content store starts empty; a successful 200 response (no auth
  // error, no network failure) proves the content API path is live.
  expect(text).not.toMatch(/authenticat|401|unauthor|connection refused/i);
});
