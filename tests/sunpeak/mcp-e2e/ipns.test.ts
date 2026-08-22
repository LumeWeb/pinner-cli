import { test, expect } from 'sunpeak/test';
import { invoke, textOf, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the IPNS flow is
// stateful (list seeded key -> create -> get -> publish -> resolve ->
// republish -> delete all share the fake's in-memory IPNS store). Serial mode
// forces this file's tests to run in order in ONE worker.
test.describe.configure({ mode: 'serial' });

/**
 * IPNS domain tools (ipns_keys_* / ipns_publish / ipns_republish /
 * ipns_resolve) driven through the host-discovery contract: every call goes
 * through invoke_tool (the progressive-disclosure meta-tool) with { name,
 * args }, never by calling the direct tool name.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/ipfs/ipns_test.go validate the same
 * fake endpoints via `go test -race ./internal/mcptest/...`.
 *
 * STATE SAFETY: the fake content API's IPNS store is shared by BOTH host
 * projects ([chatgpt] and [claude]) which run this file in separate processes
 * against the SAME store. Each process mints its OWN unique key name at load
 * time so there is no cross-process collision, and only the shared SEEDED key
 * ("seed-key", created by mcptest.Seed) is asserted as present.
 *
 * CONTRACT NOTES (from internal/catalogops/ipns.go and internal/core/ipns):
 *   - ipns_keys_list returns {status:'ok', value:{data:[...],total:N}} and
 *     accepts an optional `search` (server-side name substring).
 *   - ipns_keys_create takes `name` (+ optional `key` import) and returns the
 *     created key {id:N, name, ipns_name, peer_id,...}.
 *   - ipns_keys_get takes `id` (flexible: numeric id or name).
 *   - ipns_publish takes `cid` + `key-name` and returns the published record
 *     {name, value, sequence,...}. The `key-name` is resolved to a key id
 *     server-side.
 *   - ipns_resolve takes `name` and returns the CID the name was published to
 *     ({value, path, name,...}).
 *   - ipns_republish takes `key-name` and returns {count, message}.
 *   - ipns_keys_delete is SafetyDestructive; the MCP dispatch layer refuses
 *     destructive ops invoked by a model actor with a needs_human
 *     confirmation handoff BEFORE the handler runs. Through invoke_tool it
 *     always returns the confirmation hand-off, not a delete. This locks the
 *     gate.
 */

// Mint a unique key name per module instance so each host project's flow is
// isolated in the shared fake store.
const KeyName = `e2e-key-${Math.random().toString(36).slice(2, 8)}`;
const CID = 'QmE2eIpnsContent';

let createdKeyId: string | undefined;
let publishedName: string | undefined;

test('ipns_keys_list contains the seeded key (and the created one later)', async ({ mcp }) => {
  const result = await invoke(mcp, 'ipns_keys_list', {});

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  // The shared seeded key created by mcptest.Seed is always present.
  expect(result).toHaveTextContent('seed-key');
});

test('ipns_keys_create creates a unique key', async ({ mcp }) => {
  const result = await invoke(mcp, 'ipns_keys_create', { name: KeyName });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(KeyName);

  // Capture the numeric key id from the returned key object.
  const text = textOf(result);
  const match = /"id"\s*:\s*(\d+)/.exec(text);
  expect(match).not.toBeNull();
  createdKeyId = match![1];
  expect(createdKeyId!.length).toBeGreaterThan(0);
});

test('ipns_keys_get returns the created key', async ({ mcp }) => {
  expect(createdKeyId).toBeDefined();
  const result = await invoke(mcp, 'ipns_keys_get', { id: createdKeyId! });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(KeyName);
});

test('ipns_keys_list with search narrows to the seeded key only', async ({ mcp }) => {
  const result = await invoke(mcp, 'ipns_keys_list', { search: 'seed-key' });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent('seed-key');
  // The search should NOT surface the created unique key.
  expect(result).not.toHaveTextContent(KeyName);
});

test('ipns_publish publishes a CID under the created key', async ({ mcp }) => {
  const result = await invoke(mcp, 'ipns_publish', {
    cid: CID,
    'key-name': KeyName,
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(CID);
  expect(result).toHaveTextContent('sequence');

  // Capture the published IPNS name for the resolve/republish round-trip.
  const text = textOf(result);
  const match = /"name"\s*:\s*"([^"]+)"/.exec(text);
  expect(match).not.toBeNull();
  publishedName = match![1];
  expect(publishedName!.length).toBeGreaterThan(0);
});

test('ipns_resolve resolves the published name back to the CID', async ({ mcp }) => {
  expect(publishedName).toBeDefined();
  const result = await invoke(mcp, 'ipns_resolve', { name: publishedName! });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(CID);
  expect(result).toHaveTextContent('/ipns/');
});

test('ipns_republish republishes the record for the key', async ({ mcp }) => {
  const result = await invoke(mcp, 'ipns_republish', { 'key-name': KeyName });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('count');
});

test('ipns_keys_delete is gated by the destructive confirmation handoff', async ({ mcp }) => {
  expect(createdKeyId).toBeDefined();
  // ipns_keys_delete is SafetyDestructive and the MCP layer refuses it for a
  // model actor, returning a needs_human confirmation hand-off BEFORE the
  // handler runs (even with confirm:true). This is not an error.
  const result = await invoke(mcp, 'ipns_keys_delete', {
    id: createdKeyId!,
    confirm: true,
  });

  expect(result.isError).toBeUndefined();
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'needs_human' });
  expect(result).toHaveStructuredContent({ reason: 'confirmation' });

  // Because the destructive gate deferred to the human, the key was NOT
  // removed — the store still holds it and list can find it.
  const after = await invoke(mcp, 'ipns_keys_list', {});
  expect(isCleanSuccess(after)).toBe(true);
  expect(after).toHaveTextContent(KeyName);
});
