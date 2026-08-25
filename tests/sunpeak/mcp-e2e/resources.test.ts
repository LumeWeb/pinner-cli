import { test, expect } from 'sunpeak/test';
import { invoke, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the website
// dns-requirements resource read below depends on a website that THIS file
// creates via websites_create (the fake content API does not pre-seed any
// website — only the account/token and IPNS key are seeded on boot, see
// cmd/mcp-test-server/main.go and internal/mcptest/mcptest.go Seed). sunpeak's
// base config sets `fullyParallel: true`, so without serial mode each test
// would get its own worker with a fresh module instance and a fresh Domain —
// the create-then-read resource flow would break.
test.describe.configure({ mode: 'serial' });

/**
 * MCP resources surface: listResources() / readResource(uri).
 *
 * These are MCP RESOURCES (MCP resources/list + resources/read over stdio),
 * NOT tools — so they are driven through the mcp fixture's protocol
 * primitives (mcp.listResources() / mcp.readResource(uri)), never through
 * invoke_tool (which dispatches catalog tools only).
 *
 * The pinner resource set (internal/mcp/resources.go ResourceDescriptors):
 *   static (resources/list)                          templates (resources/templates/list)
 *   - pinner://account/status                        - pinner://websites/{domain}/dns-requirements
 *   - pinner://vault/status                          - pinner://websites/{id}/validation-status
 *   - pinner://websites/platform-domains             - pinner://wizard/{session_id}/state
 *
 * NOTE on fixture behavior (verified by probing the real `pinner mcp`
 * binary): the official MCP Go SDK (sdk/resource.go) registers the static
 * resources via srv.AddResource and the three templates via
 * srv.AddResourceTemplate. The MCP client's listResources() (=> resources/list)
 * therefore surfaces ONLY the three static pinner resources (plus the ui:// MCP
 * App HTML resources); the three templates are advertised under
 * resources/templates/list, which the sunpeak mcp fixture does not expose
 * (its Resource shape has uri but no uriTemplate). So this test asserts the
 * three static pinner URIs from listResources() and exercises the template
 * engine through readResource() on an INSTANTIATED template URI for the
 * website domain (the meaningful live behaviour).
 *
 * CI-PENDING: this file is verified in CI (it drives the real `pinner mcp`
 * binary over stdio -> SDK -> fake API). It cannot be run locally on
 * constrained hosts because launching the Playwright browser e2e suite OOMs
 * (SIGKILL/exit 137); the readResource assertions here were validated with a
 * direct stdio probe of the built binary against the seeded fake.
 *
 * STATE SAFETY: the website store in the fake content API is shared by both
 * host projects in a run ([chatgpt] and [claude] each spawn their own
 * `pinner mcp` against the SAME fake). To isolate each project, this file
 * mints its OWN unique domain (like websites.test.ts) and creates it via
 * websites_create before reading its dns-requirements resource. readResource
 * on account/status and vault/status are read-only and side-effect free.
 */

// Mint a unique domain per module instance so each host project's create +
// read-resource flow stays isolated in the shared fake store.
const Domain = `res-${Math.random().toString(36).slice(2, 8)}.test`;
const Cid = `Qm${Math.random().toString(36).slice(2, 12)}`;

// The STATIC pinner:// resources that resources/list (and therefore
// mcp.listResources()) advertises, per internal/mcp/resources.go.
const STATIC_RESOURCE_URIS = [
  'pinner://account/status',
  'pinner://vault/status',
  'pinner://websites/platform-domains',
];

test('listResources exposes the static pinner:// resources', async ({ mcp }) => {
  const resources = await mcp.listResources();

  // Collect the advertised pinner:// URIs (resources/list also carries the
  // ui:// MCP App HTML resources; we only care about our scheme here).
  const pinnerUris = resources.map((r) => r.uri).filter((u) => String(u).startsWith('pinner://'));

  // All static pinner resources must be listed.
  for (const uri of STATIC_RESOURCE_URIS) {
    expect(pinnerUris).toContain(uri);
  }

  // Every advertised pinner:// resource belongs to the verified resource set
  // (the static URIs above). Template URIs are intentionally not listed by
  // resources/list, so none of the brace-form template URIs may appear here.
  for (const uri of pinnerUris) {
    expect(STATIC_RESOURCE_URIS).toContain(uri);
  }
});

test('readResource account/status returns the seeded account status JSON', async ({ mcp }) => {
  // account/status is a static resource. The fixture config carries the seeded
  // token (token-e2e@example.com, from fixtures/pinner-home/config.yaml), and
  // the fake seeds that same account (mcptest.Seed -> e2e@example.com), so the
  // live provider reports authenticated + token valid. The handler returns
  // { authenticated, api_key, token_valid, token_error?, quota?, config? }.
  const raw = await mcp.readResource('pinner://account/status');

  // readResource returns the resource text: must be well-formed JSON.
  const status = JSON.parse(raw);

  expect(status.authenticated).toBe(true);
  // The seeded token validates against the fake account, not an auth failure.
  expect(status.token_valid).toBe(true);
  // config.base_endpoint reflects the fixture's fake endpoint, proving this is
  // the live read against the shared fixture config (not a hardcoded stub).
  expect(status.config?.base_endpoint).toBe('http://127.0.0.1:8126');
});

test('readResource website dns-requirements resolves a created website domain', async ({ mcp }) => {
  // No website is pre-seeded by the fake, so create one first (mirrors the
  // websites.test.ts flow) so the {domain} template resolves against the store.
  const created = await invoke(mcp, 'websites_create', {
    website: Domain,
    cid: Cid,
    'target-type': 'ipfs',
    'dns-hosting': true,
  });
  expect(isCleanSuccess(created)).toBe(true);

  // Read the instantiated dns-requirements template URI for that domain.
  const raw = await mcp.readResource(`pinner://websites/${Domain}/dns-requirements`);
  const reqs = JSON.parse(raw);

  // Resolved for the requested domain; a DNS-hosted site carries NS records
  // (mirroring internal/mcp/resources.go buildDNSRequirements).
  expect(reqs.domain).toBe(Domain);
  expect(typeof reqs.dns_hosting_enabled).toBe('boolean');
  expect(Array.isArray(reqs.records)).toBe(true);
});

test('readResource vault/status returns vault state JSON', async ({ mcp }) => {
  // vault/status is a static resource: it reads the local vault registry under
  // the fixture HOME. The fixture does not configure a vault, so the correct
  // contract is a well-formed JSON status reporting initialized/sia_configured
  // false — that is the documented "no vault configured" state, not an error.
  const raw = await mcp.readResource('pinner://vault/status');

  const status = JSON.parse(raw);
  expect(typeof status.initialized).toBe('boolean');
  expect(typeof status.sia_configured).toBe('boolean');
  expect('indexer_url' in status).toBe(true);
});
