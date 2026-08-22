import { test, expect } from 'sunpeak/test';
import { invoke, textOf, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the website flow
// is stateful (create -> list -> get -> update -> validate -> domains ->
// ssl-status -> delete all share the fake's in-memory website store AND the
// module-level captured website domain/id). sunpeak's base config sets
// `fullyParallel: true`, which gives every test its own worker and a fresh
// module instance — that would break the flow. Serial mode forces this file's
// tests to run in order in ONE worker.
test.describe.configure({ mode: 'serial' });

/**
 * Website tools (websites_* / websites_domains_*) driven through the
 * host-discovery contract: every call goes through invoke_tool with
 * { name, args }, never by calling the direct tool name.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/ipfs/websites_test.go validate the
 * same fake endpoints via `go test -race ./internal/mcptest/...`.
 *
 * STATE SAFETY: the website store in cmd/mcp-test-server's fake content API
 * is shared by BOTH host projects in a run ([chatgpt] and [claude]) which run
 * this file in separate processes against the SAME store. To isolate each
 * project's stateful flow, this file mints its OWN unique website domain at
 * load time; no other file/project can collide with it. The tests are ORDERED
 * as one stateful flow and must run serially within this file:
 *
 *   1. websites_create     -> creates a site, captures its domain + id
 *   2. websites_list       -> contains the created domain
 *   3. websites_get        -> resolves the site by domain (round-trip)
 *   4. websites_update     -> changes the target cid (round-trip)
 *   5. websites_validate   -> reports valid
 *   6. websites_ssl_status -> reports ready ssl (keyed by domain)
 *   7. websites_config     -> returns gateway domain + nameservers
 *   8. websites_domains_add -> binds a secondary domain
 *   9. websites_domains_list -> lists it
 *   10. websites_domains_dns_requirements -> returns delegation nameservers
 *   11. websites_enable_ipns -> converts to IPNS targeting (returns ipns_key_id)
 *   12. websites_delete    -> destructive gate (needs_human confirmation)
 */

// Mint a unique domain per module instance so each host project's flow is
// isolated in the shared fake store.
const Domain = `ws-${Math.random().toString(36).slice(2, 8)}.test`;
const Cid = `Qm${Math.random().toString(36).slice(2, 12)}`;

let capturedId: string | undefined;

test('websites_create creates a website for a unique domain', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_create', {
    website: Domain,
    cid: Cid,
    'target-type': 'ipfs',
    'dns-hosting': true,
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(Domain);
  expect(result).toHaveTextContent(Cid);

  const text = textOf(result);
  const match = /"id"\s*:\s*(\d+)/.exec(text);
  expect(match).not.toBeNull();
  capturedId = match![1];
  expect(capturedId!.length).toBeGreaterThan(0);
});

test('websites_list contains the created website', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_list', {});

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(Domain);
});

test('websites_get resolves the website by domain (round-trip)', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_get', { website: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(Domain);
  expect(result).toHaveTextContent('active');
});

test('websites_update changes the target cid (round-trip)', async ({ mcp }) => {
  const newCid = `Qm${Math.random().toString(36).slice(2, 12)}`;
  const result = await invoke(mcp, 'websites_update', {
    website: Domain,
    cid: newCid,
    'target-type': 'ipfs',
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(newCid);
});

test('websites_validate reports the website as valid', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_validate', { website: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('validated');
});

test('websites_ssl_status reports ready ssl for the domain', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_ssl_status', { website: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent('ready');
});

test('websites_config returns gateway domain and nameservers', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_config', {});

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('gateway_domain');
  expect(result).toHaveTextContent('nameservers');
});

test('websites_domains_add binds a secondary domain', async ({ mcp }) => {
  const sub = `www.${Domain}`;
  const result = await invoke(mcp, 'websites_domains_add', {
    domain: sub,
    namespace: 'icann',
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(sub);
});

test('websites_domains_list contains the bound domain', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_domains_list', { website: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(`www.${Domain}`);
});

test('websites_domains_dns_requirements returns delegation nameservers', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_domains_dns_requirements', {
    domain: `www.${Domain}`,
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('nameservers');
});

test('websites_enable_ipns converts the website to IPNS targeting', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_enable_ipns', { website: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('ipns');
  expect(result).toHaveTextContent('ipns_key_id');
});

test('websites_delete is gated by the destructive confirmation handoff', async ({ mcp }) => {
  // websites_delete is SafetyDestructive and the MCP layer refuses it for a
  // model actor, returning a needs_human confirmation hand-off BEFORE the
  // handler runs (even with confirm:true). This is not an error.
  const result = await invoke(mcp, 'websites_delete', {
    website: Domain,
    confirm: true,
  });

  expect(result.isError).toBeUndefined();
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'needs_human' });
  expect(result).toHaveStructuredContent({ reason: 'confirmation' });

  // The destructive gate deferred to the human, so the store still holds it.
  const after = await invoke(mcp, 'websites_get', { website: Domain });
  expect(isCleanSuccess(after)).toBe(true);
  expect(after).toHaveTextContent(Domain);
});
