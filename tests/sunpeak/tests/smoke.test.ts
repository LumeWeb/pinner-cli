import { test, expect } from 'sunpeak/test';

// Connectivity smoke: the real `pinner mcp` binary boots over stdio and serves
// its tool catalog. If this fails, the server isn't reachable and nothing else
// in the suite can pass.
test('server exposes tools', async ({ mcp }) => {
  const tools = await mcp.listTools();
  expect(tools.length).toBeGreaterThan(0);
});
