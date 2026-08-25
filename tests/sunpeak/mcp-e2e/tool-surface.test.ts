import { test, expect } from 'sunpeak/test';

/**
 * Progressive-disclosure contract for pinner's public `tools/list` surface.
 *
 * Pinner's MCP server hides the full operation catalog behind three
 * meta-tools (search_tools / describe_tool / invoke_tool). The client-visible
 * `tools/list` must advertise ONLY:
 *   - the three progressive-disclosure meta-tools, plus
 *   - the host-curated direct tools (compiledCuratedToolNames + the custom
 *     transport tools that register DirectVisible=true).
 *
 * This test locks that exact surface as a snapshot, so a future change that
 * accidentally exposes a hidden catalog tool (account_info, dns_*, ipns_*,
 * operations_*, api_keys_*, ...) — or hides a curated one — fails loudly
 * instead of silently drifting.
 *
 * The expected set below was captured by probing the running server
 * (listTools() over stdio), and is identical across the chatgpt/claude host
 * projects the suite runs against. Every name is either a meta-tool or a
 * deliberately curated direct tool; nothing internal leaks through.
 */

const META_TOOLS = ['search_tools', 'describe_tool', 'invoke_tool'];

// Exact advertised surface, captured from `pinner mcp` over stdio (identical
// on both chatgpt and claude host projects). Includes the `open_*` app
// launchers (ui:// surface from the mcp-apps feature) alongside the curated
// direct tools. Sorted for the assertion below.
const EXPECTED_TOOLS = [
  'account_email_change',
  'account_password_reset',
  'account_password_update',
  'agent_guide',
  'auth_logout',
  'auth_resume',
  'auth_sso',
  'auth_sso_status',
  'auth_status',
  'capabilities',
  'describe_tool',
  'domains_wizard_start',
  'domains_wizard_step',
  'download_file',
  'invoke_tool',
  'ipfs_upload_status',
  'ipfs_upload_submit',
  'open_account',
  'open_account_email',
  'open_account_password',
  'open_download_manager',
  'open_pin_creator',
  'open_pin_list',
  'open_sso_signin',
  'open_upload_manager',
  'open_vault_browser',
  'open_vault_create',
  'open_vault_download_manager',
  'open_vault_manager',
  'open_vault_restore',
  'pin_status',
  'pins_add',
  'pins_list',
  'pins_rm',
  'pins_status',
  'search_tools',
  'upload_cancel',
  'upload_data',
  'upload_file',
  'upload_file_async',
  'upload_list',
  'upload_status',
  'upload_url',
  'vault_create',
  'vault_create_resume',
  'vault_create_status',
  'vault_get_file',
  'vault_ls',
  'vault_put_file',
  'vault_restore',
  'vault_restore_resume',
  'vault_restore_status',
  'vault_search',
  'vault_set_provenance',
  'vault_stat',
  'vault_status',
  'vault_tag_ls',
  'vault_upload_submit',
  'vault_version_get',
  'vault_version_ls',
  'websites_get',
  'websites_list',
  'websites_platform_domain_availability',
  'websites_validate',
  'websites_wizard_start',
  'websites_wizard_step',
].sort();

// Catalog tools that MUST live only behind invoke_tool and never leak into
// tools/list. Guard rail separate from the exact snapshot so the intent reads
// clearly even if the curated set changes.
const HIDDEN_BEHIND_INVOKE = [
  'account_info',
  'auth_login',
  // prefixes that would surface a domain leak:
  'dns_',
  'ipns_',
  'operations_',
  'api_keys_',
];

test('the public tools/list surface is the disclosure meta-tools + curated set', async ({ mcp }) => {
  const tools = await mcp.listTools();
  const names = tools.map((t) => t.name).sort();

  // Exact snapshot: the surface may not gain OR lose a directly-advertised tool.
  expect(names).toEqual(EXPECTED_TOOLS);

  // The meta-tools are always present.
  for (const meta of META_TOOLS) {
    expect(names).toContain(meta);
  }

  // No hidden catalog tool leaks through: none of the exact, and no name
  // carrying a for-invoke-only prefix.
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
