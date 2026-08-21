import { test, expect, request } from '@playwright/test';

// Raw HTTP assertions for the static-bearer auth gate of `pinner mcp --http`.
// These hit the running server directly (see playwright.http.config.ts, which
// starts it) and verify the streamable-HTTP wire contracts:
//   - unauthenticated POST /mcp  -> 401 with no bearer token
//   - authenticated   POST /mcp  -> served (non-401)
//   - GET /healthz               -> 200 without auth (liveness probe)

const BASE = 'http://127.0.0.1:8125';
const SECRET = process.env.PINNER_TEST_SECRET ?? 'sunpeak-test-secret';

test('unauthenticated /mcp request is rejected with 401', async () => {
  const ctx = await request.newContext({ baseURL: BASE });
  const res = await ctx.post('/mcp', {
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    data: { jsonrpc: '2.0', id: 1, method: 'initialize', params: {} },
  });
  expect(res.status()).toBe(401);
  await ctx.dispose();
});

test('authenticated /mcp request is served', async () => {
  const ctx = await request.newContext({ baseURL: BASE });
  const res = await ctx.post('/mcp', {
    headers: {
      // Streamable HTTP requires BOTH mime types in Accept.
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${SECRET}`,
    },
    data: { jsonrpc: '2.0', id: 2, method: 'initialize', params: {} },
  });
  // The 200 proves the bearer token was actually accepted (a 401 here would
  // mean auth failed).
  expect(res.status()).toBe(200);
  await ctx.dispose();
});

test('the /healthz liveness probe is reachable without auth', async () => {
  const ctx = await request.newContext({ baseURL: BASE });
  const res = await ctx.get('/healthz');
  expect(res.status()).toBe(200);
  await ctx.dispose();
});
