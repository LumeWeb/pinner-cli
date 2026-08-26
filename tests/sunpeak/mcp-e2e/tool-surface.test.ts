import { test, expect } from 'sunpeak/test';

/**
 * Progressive-disclosure contract for pinner's public `tools/list` surface.
 *
 * Pinner's MCP server hides the full operation catalog behind three meta-tools
 * (search_tools / describe_tool / invoke_tool). The client-visible `tools/list`
 * advertises ONLY those meta-tools plus the host-curated direct tools
 * (compiledCuratedToolNames + the custom transport tools that register
 * DirectVisible=true).
 *
 * WHY THERE IS NO EXACT-SURFACE SNAPSHOT (design rationale):
 *
 * This suite asserts BEHAVIOR, not equality between two copies of the tool
 * surface. We deliberately do not lock tools/list to a fixed set. Testing
 * "toolset A == toolset B" (server vs. a committed reference) measures only
 * whether the reference is in sync with the server, so it fails on staleness
 * rather than on a real regression; and comparing against a runtime-derived
 * copy of the same server can never fail at all (it is tautological).
 * Neither tells you whether the surface is actually usable.
 *
 * So instead we assert invariants that can genuinely break and that matter to
 * an agent consuming the surface:
 *   - the progressive-disclosure meta-tools are always present;
 *   - no for-invoke-only catalog tool (account_info, auth_login, dns_*, ipns_*,
 *     operations_*, api_keys_*) leaks into tools/list;
 *   - every advertised tool carries a non-empty description and a valid
 *     inputSchema.
 *
 * Whether an exact tools/list snapshot is even worth maintaining is a separate
 * 1st-principles question; tracking a synchronized copy "just so it can fail"
 * provides no signal and is not part of this contract.
 */

const META_TOOLS = ['search_tools', 'describe_tool', 'invoke_tool'];

// Catalog tools that MUST live only behind invoke_tool and never leak into
// tools/list. Guard rail: a name surfacing in tools/list that is in this list
// (or carries one of these prefixes) is an accidental leak.
const HIDDEN_BEHIND_INVOKE = [
  'account_info',
  'auth_login',
  // prefixes that would surface a domain leak:
  'dns_',
  'ipns_',
  'operations_',
  'api_keys_',
];

test('the public tools/list surface exposes the disclosure meta-tools', async ({ mcp }) => {
  const tools = await mcp.listTools();
  const names = tools.map((t) => t.name);

  expect(names.length).toBeGreaterThan(0);

  // The three always-visible discovery meta-tools are present.
  for (const meta of META_TOOLS) {
    expect(names).toContain(meta);
  }
});

test('no for-invoke-only catalog tool leaks into tools/list', async ({ mcp }) => {
  const names = (await mcp.listTools()).map((t) => t.name).sort();

  // No hidden catalog tool leaks through: none of the exact names, and no
  // name carrying a for-invoke-only prefix.
  for (const name of names) {
    expect(HIDDEN_BEHIND_INVOKE).not.toContain(name);
    for (const prefix of HIDDEN_BEHIND_INVOKE.filter((p) => p.endsWith('_'))) {
      expect(name.startsWith(prefix)).toBe(false);
    }
  }
});

test('every advertised tool has a description and an inputSchema', async ({ mcp }) => {
  const tools = await mcp.listTools();

  expect(tools.length).toBeGreaterThan(0);
  for (const t of tools) {
    const label = `tool "${t.name}"`;
    expect(typeof t.description, `${label} must expose a description`).toBe('string');
    expect(t.description!.trim().length, `${label} description must be non-empty`).toBeGreaterThan(0);
    expect(typeof t.inputSchema, `${label} must expose an inputSchema object`).toBe('object');
    expect(t.inputSchema === null, `${label} inputSchema must not be null`).toBe(false);
  }
});
