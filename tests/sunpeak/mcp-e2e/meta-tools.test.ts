import { test, expect } from 'sunpeak/test';
import { invoke, describeTool, searchTool } from './helpers';

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

test('capabilities tool returns declared capabilities', async ({ mcp }) => {
  const result = await mcp.callTool('capabilities', {});
  expect(result).not.toBeError();

  // text content carries the report JSON (same data as the structured payload)
  // so a text-only client sees the transport + source modes, not a stub label.
  expect(result).toHaveTextContent('transport');
  expect(result).toHaveTextContent('source_modes');

  // Locked from a live probe: stdio transport advertises only `path` sourcing.
  expect(result).toHaveStructuredContent({ transport: 'stdio' });
  expect(result).toHaveStructuredContent({ source_modes: ['path'] });
});

const GUIDE_FLOWS = [
  { name: 'auth', title: 'Authenticate', steps: ['auth_status', 'auth_sso', 'auth_resume', 'auth_status'], detail: 'Run auth_status; if unauthenticated, call auth_sso and poll auth_resume with the returned handle until the human completes the browser sign-in.' },
  { name: 'vault_create', title: 'Create a vault', steps: ['vault_create', 'vault_create_resume', 'vault_status'], detail: 'Call vault_create with a profile name; poll vault_create_resume with the returned handle; confirm with vault_status until unlocked.' },
  { name: 'vault_restore', title: 'Restore a vault', steps: ['vault_restore', 'vault_restore_resume', 'vault_status'], detail: 'Call vault_restore; poll vault_restore_resume with the returned handle; confirm with vault_status until unlocked.' },
  { name: 'upload', title: 'Upload new content (creates + pins)', steps: ['capabilities', 'upload_file', 'upload_status'], detail: 'Check capabilities; call upload_file with a transport-scoped source (host path in co-located stdio, a minted presigned HTTP PUT in remote mode, or url/data on the OpenAI tunnel), then poll upload_status for the CID. The returned CID is already pinned — use it directly in websites_create/update; do NOT call pins_add after an upload.' },
  { name: 'vault_upload', title: 'Store a file in a vault', steps: ['capabilities', 'vault_put_file', 'upload_status'], detail: 'Check capabilities; if vault_put_file is available and the target vault is unlocked, call it with a transport-scoped source (host path in co-located stdio mode, a minted presigned PUT in remote mode, or url/data on the OpenAI tunnel) plus the destination vault_path, then monitor with upload_status for the CID.' },
  { name: 'download', title: 'Download IPFS content to a file', steps: ['capabilities', 'download_file'], detail: 'Check capabilities\' download_sink_modes; call download_file with ipfs_path (CID or CID/path) and a supported sink. sink=local writes the bytes to a host-side output_path on the MCP server\'s own disk (available on every transport); sink=drop (when advertised) returns a one-time HTTP GET filedrop link to pull from out of band with curl -o or a browser.' },
  { name: 'vault_download', title: 'Download a file from a vault', steps: ['capabilities', 'vault_get_file'], detail: 'Check capabilities\' download_sink_modes and that the vault is unlocked; call vault_get_file with vault_path and a supported sink. sink=local writes the decrypted bytes to a host-side output_path on the MCP server\'s own disk; sink=drop (when advertised) returns a one-time HTTP GET filedrop link.' },
  { name: 'pins', title: 'Manage pins', steps: ['pins_add', 'pins_list', 'pins_status', 'pins_rm'], detail: 'pins_add imports content already on IPFS by external CID; it is NOT for use after an upload tool (which already pins). pins_status takes one cid; pins_rm requires confirm and exactly one of cids or all.' },
  { name: 'publish_website_upload', title: 'Publish a website (new content)', steps: ['upload_file', 'websites_create'], detail: 'Upload new bytes, e.g. upload_file returning a CID, then websites_create/update. CID from upload is already pinned; no pins_add.' },
];

test('agent_guide tool returns guided onboarding text', async ({ mcp }) => {
  const result = await mcp.callTool('agent_guide', {});
  expect(result).not.toBeError();

  expect(result).toHaveTextContent('Pinner agent guide');

  // The guide is structured as ordered tool chains. Its flow names/steps chain
  // the onboarding keywords (auth_status, vault_create, pins) exactly.
  expect(result).toHaveStructuredContent({
    flows: GUIDE_FLOWS,
  });
  // Explicit onboarding-keyword coverage beyond the exact flow snapshot.
  expect(result).toHaveStructuredContent({ summary: 'Start here. Drive Pinner through these primary flows; each step is a tool. Check the current state first, then follow the matching flow.' });
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
