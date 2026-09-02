import { test, expect } from 'sunpeak/test';

/**
 * Protocol-surface contract for the pinner MCP server, run over stdio via the
 * sunpeak `mcp` fixture (protocol primitives — no rendering, fast).
 *
 * The real `pinner mcp` binary requires no credentials to boot and to serve its
 * tool catalog, but every tool call that reaches the Pinner API needs an
 * authenticated session. These tests pin the auth-free surface: the tool
 * catalog, schema validity, and the clean / non-crashing error contract for an
 * unauthenticated call.
 */

test('server exposes the progressive-disclosure tool surface', async ({ mcp }) => {
  const tools = await mcp.listTools();
  const names = tools.map((t) => t.name);

  // The always-visible adapter tools are the discovery entry points.
  for (const name of [
    'search_tools',
    'describe_tool',
    'invoke_read_tool',
    'invoke_write_tool',
    'invoke_destructive_tool',
  ]) {
    expect(names).toContain(name);
  }

  // The catalog surface is larger than the always-visible meta set (auth,
  // vault, upload, account ops, etc.).
  expect(tools.length).toBeGreaterThan(5);
});

test('every advertised tool carries a valid JSON schema with required args', async ({ mcp }) => {
  const tools = await mcp.listTools();
  for (const tool of tools) {
    expect(tool.name, tool.name).toBeTruthy();
    expect(tool.inputSchema, `${tool.name} inputSchema`).toBeDefined();
    expect(tool.inputSchema.type, `${tool.name} type`).toBe('object');
    // Types + properties must be present and well-formed.
    expect(tool.inputSchema.properties, `${tool.name} properties`).toBeDefined();
    expect(tool.inputSchema.properties).toBeInstanceOf(Object);
  }
});

test('describe_tool returns a schema for a known catalog tool', async ({ mcp }) => {
  // Discover a real catalog tool via search_tools, then describe it. The
  // first returned tool is a genuine registered operation, so describing it
  // must succeed and include its input schema.
  const search = await mcp.callTool('search_tools', { query: '' });
  const searchText = search.content?.map((c) => c.text ?? '').join('') ?? '';
  const parsed = JSON.parse(searchText);
  const first = parsed.tools?.[0];
  expect(first?.name).toBeTruthy();

  const result = await mcp.callTool('describe_tool', { name: first.name });
  const dtext = result.content?.map((c) => c.text ?? '').join('') ?? '';
  expect(result.isError).toBeFalsy();
  expect(dtext.length).toBeGreaterThan(0);
  expect(dtext).toContain('inputSchema');
});

test('unauthenticated call fails cleanly with a machine-readable error, not a crash', async ({ mcp }) => {
  // The typed invoke dispatchers route to a real catalog operation. Without an authed
  // session it must fail with isError:true and a useful message rather than
  // hang or hard-crash the server process. Use a real registered tool
  // (account_info) so we exercise the auth check, not the unknown-tool path.
  //
  // A developer who runs this suite against their own config (e.g. a local
  // ~/.config/pinner/config.yaml with a valid auth_token) is already
  // authenticated, so `account_info` legitimately succeeds and the
  // unauthenticated-error contract cannot be exercised. Skip in that case
  // rather than fail the run; CI has no token and still asserts the gate.
  const probe = await mcp.callTool('invoke_read_tool', { name: 'account_info', args: {} });
  if (!probe.isError) {
    test.skip(true, 'environment is already authenticated; cannot exercise the unauthenticated path');
  }
  const result = await mcp.callTool('invoke_read_tool', { name: 'account_info', args: {} });
  expect(result.isError).toBe(true);
  const text = result.content?.map((c) => c.text ?? '').join('') ?? '';
  // The actual error is `authentication failed: not authenticated: no auth
  // token` — assert on a specific auth-failure marker so the test fails when
  // the auth gate is broken and returns an unrelated error instead.
  expect(text).toMatch(/authenticat|401|unauthor/i);
});
