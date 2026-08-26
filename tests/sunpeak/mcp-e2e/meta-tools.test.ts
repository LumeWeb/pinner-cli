import { test, expect } from 'sunpeak/test';
import { invoke, describeTool, searchTool, textOf } from './helpers';

/**
 * Progressive-disclosure meta-tools: search_tools / describe_tool / invoke_tool
 * and the host-curated orientation tools (capabilities / agent_guide).
 *
 * search_tools / describe_tool / invoke_tool operate over the FULL operation
 * catalog (reachable by keyword / name); capabilities and agent_guide are
 * direct tools on the public surface. These tests lock the discovery contract:
 * ranked keyword search, input-schema introspection, the clean error paths of
 * invoke_tool, and the structured orientation output of the two direct tools.
 *
 * Keywords / structured shapes below were probed against the running server
 * (2026-08-22) before being locked.
 *
 * NOTE on isCleanSuccess: it is tuned for API-touching invoke_tool results and
 * flags the words "authenticated"/"authentication" as auth failures. Those words
 * legitimately appear inside catalog *descriptions* returned by search_tools /
 * describe_tool, so for those discovery tools we use `not.toBeError()` as the
 * clean-success signal instead.
 */

// ── search_tools: keyword search over the full catalog ──────────────

test('search_tools finds domain tools by keyword', async ({ mcp }) => {
  const pins = await searchTool(mcp, 'pin');
  // NOTE: discovery tools return catalog descriptions that legitimately contain
  // the word "authenticated"/"401", so isCleanSuccess (which regex-scans for
  // those) false-negatives here — signal success with not.toBeError() instead.
  expect(pins).not.toBeError();
  // ranked keyword search surfaces the whole pins_* family
  expect(pins).toHaveTextContent('pins_add');
  expect(pins).toHaveTextContent('pins_list');
  expect(pins).toHaveTextContent('pins_status');
  expect(pins).toHaveTextContent('pins_rm');

  const account = await searchTool(mcp, 'account');
  expect(account).not.toBeError();
  // `account` also matches the hidden-behind-invoke account_info tool.
  expect(account).toHaveTextContent('account_info');
});

test('search_tools with empty/help query returns the start-here set', async ({ mcp }) => {
  // Both the empty query and "help" return the primary curated orientation set.
  for (const q of ['', 'help']) {
    const result = await searchTool(mcp, q);
    expect(result).not.toBeError();

    // Locked from a live probe: the start-here set is the auth + pins + vault
    // primary flows. (websites_* are curated onto tools/list but are NOT part
    // of this orientation set, so we only assert what it actually returns.)
    expect(result).toHaveTextContent('auth_status');
    expect(result).toHaveTextContent('pins_add');
    expect(result).toHaveTextContent('pins_list');
    expect(result).toHaveTextContent('vault_create');
    expect(result).toHaveTextContent('vault_status');
  }
});

// ── describe_tool: input schema introspection ───────────────────────

test('describe_tool returns the input schema', async ({ mcp }) => {
  const pinsAdd = await describeTool(mcp, 'pins_add');
  // describe_tool returns catalog schema/descriptions that may legitimately
  // contain "authenticated"/"401", so signal with not.toBeError() (per the
  // file's NOTE at the top), not isCleanSuccess.
  expect(pinsAdd).not.toBeError();
  // The schema is returned inline as JSON text; cids is the required field.
  expect(pinsAdd).toHaveTextContent('inputSchema');
  expect(pinsAdd).toHaveTextContent('cids');
  expect(pinsAdd).toHaveTextContent('cid');
  expect(pinsAdd).toHaveTextContent('"type":"object"');

  const accountInfo = await describeTool(mcp, 'account_info');
  expect(accountInfo).not.toBeError();
  // account_info takes no required args: an empty properties schema.
  expect(accountInfo).toHaveTextContent('"type":"object"');
});

// ── invoke_tool: unknown + validation error paths ───────────────────

test('invoke_tool with unknown name returns a clean error', async ({ mcp }) => {
  const result = await invoke(mcp, '_definitely_not_a_real_tool_', {});
  expect(result).toBeError();
  // A clean error still carries explanatory text; it must not crash the session.
  expect(result).toHaveTextContent('unknown tool');
});

test('invoke_tool with missing required arg returns a validation error', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_add', {});
  expect(result).toBeError();
  // pins_add requires cids; the validation failure names the missing field.
  expect(result).toHaveTextContent('cids');
});

// ── capabilities / agent_guide: direct orientation tools ─────────────

// asJson returns the parsed structured payload of a tool result, preferring
// the structuredContent channel and falling back to the text channel (which
// carries the canonical JSON).
function asJson(result: any): any {
  const sc = result?.structuredContent;
  if (sc !== undefined && sc !== null) {
    return sc;
  }
  return JSON.parse(textOf(result));
}

// collectGuideSteps flattens every step tool referenced by the guide's flows
// (including decision branches) so the e2e can independently verify the guide
// never names a tool that does not exist.
function collectGuideSteps(guide: any): string[] {
  const steps: string[] = [];
  const walkFlows = (flows: any[]) => {
    for (const flow of flows) {
      steps.push(...(flow.steps ?? []));
      if (flow.decision) {
        walkBranches(flow.decision);
      }
    }
  };
  const walkBranches = (decision: any) => {
    for (const branch of decision.branches ?? []) {
      steps.push(...(branch.steps ?? []));
      if (branch.next) {
        walkBranches(branch.next);
      }
    }
  };
  walkFlows(guide.flows);
  return steps;
}

test('capabilities tool returns a coherent report', async ({ mcp }) => {
  const result = await mcp.callTool('capabilities', {});
  expect(result).not.toBeError();

  // text content carries the report JSON (same data as the structured payload)
  // so a text-only client sees the transport + source modes, not a stub label.
  expect(result).toHaveTextContent('transport');
  expect(result).toHaveTextContent('source_modes');

  // Invariant: the report names a non-empty transport and a non-empty set of
  // source modes it can actually serve. This can genuinely fail if the
  // transport decision breaks, without needing a committed reference.
  const report = asJson(result);
  expect(typeof report.transport).toBe('string');
  expect(report.transport.length).toBeGreaterThan(0);
  expect(Array.isArray(report.source_modes)).toBe(true);
  expect(report.source_modes.length).toBeGreaterThan(0);
});

test('agent_guide returns a coherent guide whose steps resolve to real tools', async ({ mcp }) => {
  const result = await mcp.callTool('agent_guide', {});
  expect(result).not.toBeError();

  expect(result).toHaveTextContent('Pinner agent guide');

  // Invariant: the guide is well-formed (summary + a non-empty set of flows,
  // each a named chain with steps or a branching decision).
  const guide = asJson(result);
  expect(typeof guide.summary).toBe('string');
  expect(guide.summary.length).toBeGreaterThan(0);
  expect(Array.isArray(guide.flows)).toBe(true);
  expect(guide.flows.length).toBeGreaterThan(0);
  for (const flow of guide.flows) {
    expect(typeof flow.name).toBe('string');
    expect(typeof flow.title).toBe('string');
    expect(flow.steps !== undefined || flow.decision !== undefined).toBe(true);
  }

  // Cross-surface invariant: every step tool the guide names resolves to a real
  // tool in the catalog (advertised OR behind invoke_tool). We resolve via
  // describe_tool, which can describe the whole catalog, rather than requiring
  // each step to be on the host-specific advertised tools/list — the guide may
  // legitimately point at invoke-only tools. This fails only if the guide
  // references a tool that was removed or renamed.
  for (const step of collectGuideSteps(guide)) {
    const desc = await describeTool(mcp, step);
    expect(desc.isError, `agent_guide references unknown tool "${step}"`).toBeFalsy();
  }
});

// Sanity: the session is still healthy after the error-path tests (unknown tool
// and validation failure). We re-route a call through invoke_tool and confirm
// it still VALIDATES deterministically (clean arg-validation error, not a
// crashed/hung session). We deliberately avoid an upstream-API-touching call
// (e.g. account_info) here: the fake API is shared and its auth-ping route is
// flaky under the parallel suite, which would make this session-health check
// depend on an unrelated upstream double.
test('session survives the error-path tests', async ({ mcp }) => {
  // invoke_tool must still be responsive and validating after the earlier
  // unknown-tool and missing-arg errors — a clean validation error proves the
  // session did not crash or wedge.
  const result = await invoke(mcp, 'pins_add', {});
  expect(result).toBeError();
  expect(result).toHaveTextContent('cids');
  expect(result).toHaveTextContent('missing required argument');
});
