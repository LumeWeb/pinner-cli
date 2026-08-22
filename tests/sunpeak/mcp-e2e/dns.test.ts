import { test, expect } from 'sunpeak/test';
import { invoke, textOf, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the DNS flow is
// stateful (create zone -> list -> get -> add record -> list -> get -> update
// -> delete -> validate all share the fake's in-memory DNS store AND the
// module-level captured zone domain/id). sunpeak's base config sets
// `fullyParallel: true`, which gives every test its own worker and therefore
// a fresh module instance (a fresh random domain) — that would break the
// flow. Serial mode forces this file's tests to run in order in ONE worker.
test.describe.configure({ mode: 'serial' });

/**
 * DNS domain tools (dns_zones_* / dns_records_*) driven through the
 * host-discovery contract: every call goes through invoke_tool (the
 * progressive-disclosure meta-tool) with { name, args }, never by calling the
 * direct tool name.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/ipfs/dns_test.go validate the same
 * fake endpoints via `go test -race ./internal/mcptest/...`.
 *
 * STATE SAFETY: the DNS store in cmd/mcp-test-server's fake content API is
 * shared by BOTH host projects in a run ([chatgpt] and [claude]) which run
 * this file in separate processes against the SAME store. To isolate each
 * project's stateful flow, this file mints its OWN unique sub-domain at load
 * time; no other file/project can collide with it. The tests are ORDERED as
 * one stateful flow and must run serially within this file:
 *
 *   1. dns_zones_create -> creates a zone, captures its id + domain
 *   2. dns_zones_list   -> contains the created domain
 *   3. dns_zones_get    -> resolves the zone by domain name (round-trip)
 *   4. dns_records_create -> adds an A record, captures its id
 *   5. dns_records_list -> lists the added record
 *   6. dns_records_get  -> resolves the record by name+type
 *   7. dns_records_update -> changes content, round-trips the change
 *   8. dns_records_delete -> destructive gate (needs_human confirmation)
 *   9. dns_zones_validate -> nameserver delegation reports valid
 *
 * CONTRACT NOTES (from internal/catalogops/dns.go):
 *   - dns_zones_create takes `domain` (+ optional comma-separated `nameservers`)
 *     and returns the created zone as
 *     {"status":"ok","value":{"id":N,"domain":"...","status":"active",...}}.
 *   - dns_zones_get / dns_records_* resolve the zone by DOMAIN NAME (or numeric
 *     id): resolveZoneID lists zones and matches `.Domain`. So later calls pass
 *     the created domain string, not a random id.
 *   - dns_records_create takes {zone, name, type, content} (+ optional ttl) and
 *     returns the created record including its `id`.
 *   - dns_records_update takes {zone, name, type, content}.
 *   - dns_zones_delete / dns_records_delete are SafetyDestructive; the MCP
 *     dispatch layer refuses destructive ops invoked by a model actor with a
 *     needs_human confirmation handoff BEFORE the handler runs. So through
 *     invoke_tool they always return the confirmation hand-off, not a delete.
 *     This test locks that gate.
 */

// Mint a unique sub-domain per module instance so each host project's flow is
// isolated in the shared fake store.
const Domain = `e2e-${Math.random().toString(36).slice(2, 8)}.test`;

let capturedZoneId: string | undefined;
let capturedRecordId: string | undefined;

test('dns_zones_create creates a zone for a unique domain', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_zones_create', {
    domain: Domain,
    nameservers: 'ns1.example.com,ns2.example.com',
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  // The created zone surfaces its domain and an active status.
  expect(result).toHaveTextContent(Domain);
  expect(result).toHaveTextContent('active');

  // Capture the numeric zone id from the returned zone object.
  const text = textOf(result);
  const match = /"id"\s*:\s*(\d+)/.exec(text);
  expect(match).not.toBeNull();
  capturedZoneId = match![1];
  expect(capturedZoneId!.length).toBeGreaterThan(0);
});

test('dns_zones_list now contains the created zone', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_zones_list', {});

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(Domain);
});

test('dns_zones_get resolves the zone by domain name (round-trip)', async ({ mcp }) => {
  // Resolve the zone by its domain (resolveZoneID lists zones and matches
  // .Domain), proving the full invoke_tool -> SDK -> HTTP -> fake chain.
  const result = await invoke(mcp, 'dns_zones_get', { zone: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(Domain);
  expect(result).toHaveTextContent('active');
});

test('dns_records_create adds an A record to the zone', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_records_create', {
    zone: Domain,
    name: 'www',
    type: 'A',
    content: '10.0.0.1',
    ttl: 120,
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('10.0.0.1');
  expect(result).toHaveTextContent('A');

  // Capture the record id (used by delete-by-id flows and printed by list).
  const text = textOf(result);
  const match = /"id"\s*:\s*"([^"]+)"/.exec(text);
  expect(match).not.toBeNull();
  capturedRecordId = match![1];
  expect(capturedRecordId!.length).toBeGreaterThan(0);
});

test('dns_records_list contains the added record', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_records_list', { zone: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent('www');
  expect(result).toHaveTextContent('10.0.0.1');
});

test('dns_records_get resolves the record by name+type (round-trip)', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_records_get', {
    zone: Domain,
    name: 'www',
    type: 'A',
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent('www');
  expect(result).toHaveTextContent('10.0.0.1');
});

test('dns_records_update changes the record content (round-trip)', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_records_update', {
    zone: Domain,
    name: 'www',
    type: 'A',
    content: '10.0.0.2',
  });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('10.0.0.2');

  // The read-back list reflects the new content (server persisted the change).
  const list = await invoke(mcp, 'dns_records_list', { zone: Domain });
  expect(isCleanSuccess(list)).toBe(true);
  expect(list).toHaveTextContent('10.0.0.2');
});

test('dns_records_delete is gated by the destructive confirmation handoff', async ({ mcp }) => {
  // dns_records_delete is SafetyDestructive and the MCP layer refuses it for a
  // model actor, returning a needs_human confirmation hand-off BEFORE the
  // handler runs (even with confirm:true). This is not an error.
  const result = await invoke(mcp, 'dns_records_delete', {
    zone: Domain,
    name: 'www',
    type: 'A',
    confirm: true,
  });

  expect(result.isError).toBeUndefined();
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'needs_human' });
  expect(result).toHaveStructuredContent({ reason: 'confirmation' });

  // Because the destructive gate deferred to the human, the record was NOT
  // removed — the store still holds it.
  const after = await invoke(mcp, 'dns_records_list', { zone: Domain });
  expect(isCleanSuccess(after)).toBe(true);
  expect(after).toHaveTextContent('10.0.0.2');
});

test('dns_zones_validate reports the zone as valid', async ({ mcp }) => {
  const result = await invoke(mcp, 'dns_zones_validate', { zone: Domain });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent('valid');
});
