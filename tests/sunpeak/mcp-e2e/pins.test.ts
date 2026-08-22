import { test, expect } from 'sunpeak/test';
import { invoke, textOf, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the pins flow is
// stateful (pins_add -> pins_list -> pins_status -> pins_rm share server-side
// pin-store state AND module-level captures). sunpeak's base config sets
// `fullyParallel: true`, which normally gives every test its own worker and
// therefore a fresh module instance (a fresh random CID) — that would break
// the flow. Serial mode forces this file's tests to run in order in ONE
// worker, so the module-level Cid and capturedRequestId stay stable.
test.describe.configure({ mode: 'serial' });

/**
 * Pins domain tools (pins_add / pins_list / pins_status / pins_rm) driven
 * through the host-discovery contract: every call goes through invoke_tool
 * (the progressive-disclosure meta-tool) with { name, args }, never by
 * calling the direct tool name. pins_add/list/status/rm are directly curated
 * onto tools/list, but this suite deliberately routes them through invoke to
 * mirror how a host surfaces them from the catalog.
 *
 * State is SHARED server-side: the fake content API (cmd/mcp-test-server,
 * internal/mcptest/ipfs/server.go) keeps an in-memory pin store keyed by
 * request id, and it starts empty per server process — but that one server is
 * shared by BOTH host projects in a run ([chatgpt] and [claude]), which run
 * this file in separate processes against the SAME store. To keep each
 * project's stateful flow isolated, this file mints its OWN unique valid CID
 * at load time; no other file/project can collide with it. The tests are
 * ORDERED as one stateful flow and must run serially within this file
 * (Playwright runs tests in a file in order):
 *
 *   1. pins_list  -> empty (no pin with THIS file's unique cid)
 *   2. pins_add   -> creates a pin, captures its request_id
 *   3. pins_list  -> now contains the added pin
 *   4. pins_status-> resolves the added pin (round-trip)
 *   5. pins_rm    -> destructive gate (needs_human confirmation handoff)
 *
 * CONTRACT NOTES (captured 2026-08-22 from the running server):
 *   - pins_add takes `cids` (string slice), NOT `cid`. The single-CID request
 *     returns the created pin as
 *     {"status":"ok","value":{"cid":...,"request_id":...,"status":"pinned"}} —
 *     the request id is serialized as `request_id` (the PinResult struct's
 *     RequestID field), and the fake derives it as "req-<cid>".
 *   - pins_status takes a `cid` (it looks a pin up by CID and returns its
 *     status), NOT `request_id`. So the round-trip that proves the full
 *     invoke_tool -> SDK -> HTTP -> fake chain is pins_add(cid) ->
 *     pins_status(cid). We still capture the request_id from pins_add (per the
 *     spec) so it is available, but the tool resolves by CID.
 *   - pins_rm is SafetyDestructive. The MCP dispatch layer refuses destructive
 *     ops invoked by a model actor with a needs_human confirmation handoff
 *     (Reason=confirmation) BEFORE the handler runs — unconditionally, even
 *     when `confirm:true` is passed. So through invoke_tool pins_rm cannot
 *     actually delete; it always returns the confirmation hand-off. This test
 *     locks that gate and asserts the pin survives.
 */

// ---- mint a unique, valid CIDv1 (base32, dashed form "baf...") per file ----
// CIDv1, codec dag-pb (0x70), one-byte identity multihash (0x00 0x01 <byte>).
// The random byte makes the CID unique to this module instance, so each host
// project's flow is isolated in the shared fake store.
const B32 = 'abcdefghijklmnopqrstuvwxyz234567';
function base32(bytes: number[]): string {
  let bits = 0;
  let val = 0;
  let out = '';
  for (const b of bytes) {
    val = (val << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += B32[(val >> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += B32[(val << (5 - bits)) & 31];
  return out;
}
// mint 8 random bytes so the CID has high entropy (1/256^8 collision odds) —
// the fake pin store is SHARED across host projects and test files, so a
// low-entropy CID (single byte) could collide across workers and make one
// project's pin appear in another's list, breaking the stateful assertions.
// The multihash length byte (0x08) MUST match the byte count for a valid CID.
const rnd = Array.from({ length: 8 }, () => Math.floor(Math.random() * 256));
const Cid = 'b' + base32([0x01, 0x70, 0x00, 0x08, ...rnd]);
const Name = 'e2e-pin';

// Captured from pins_add and carried into the later tests.
let capturedRequestId: string | undefined;

test('pins_list starts empty (for this file\'s unique cid)', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_list', {});

  // A clean call: not an error, no auth/network failure marker — proves the
  // content API path is live against the fake.
  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();

  // pinner renders the pin list as an array of pin objects. The store is empty
  // of THIS file's freshly-minted cid, so it must not be listed yet.
  expect(textOf(result)).not.toContain(Cid);
});

test('pins_add creates a pin and returns a request_id', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_add', { cids: [Cid], name: Name });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();

  // The created pin must surface the CID we asked to pin.
  expect(result).toHaveTextContent(Cid);

  // The success payload carries the new pin's request id, serialized as
  // `request_id` (PinResult.RequestID -> snake_case). Do NOT hardcode its
  // exact value — capture it for the flow.
  expect(result).toHaveTextContent('request_id');
  const text = textOf(result);
  const match = /"request_id"\s*:\s*"([^"]+)"/.exec(text);
  expect(match).not.toBeNull();
  capturedRequestId = match![1];
  expect(capturedRequestId!.length).toBeGreaterThan(0);
});

test('pins_list now contains the added pin', async ({ mcp }) => {
  // The fake keeps the pin from the earlier pins_add in this file's store.
  const result = await invoke(mcp, 'pins_list', {});

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(Cid);
});

test('pins_status resolves the added pin (round-trip)', async ({ mcp }) => {
  // Round-trip: the pin created by pins_add above must be resolvable back
  // through the full invoke_tool -> SDK -> HTTP -> fake chain. pins_status
  // resolves by `cid` (its catalog contract), NOT by `request_id`.
  //
  // The fake's GET /pins (internal/mcptest/ipfs/pins.go GetPins) honors the
  // `cid` filter, so the request returns ONLY the pin matching this suite's
  // high-entropy cid — the strict cid echo below is deterministic even though
  // the suite shares one fake across host projects.
  const result = await invoke(mcp, 'pins_status', { cid: Cid });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  // The requested pin is resolvable back with the SAME cid — the round-trip
  // chain carried the request through to the content fake and echoed it.
  expect(result).toHaveTextContent(Cid);
  expect(result).toHaveTextContent('pinned');
});

test('pins_rm is gated by the destructive confirmation handoff', async ({ mcp }) => {
  // pins_rm is SafetyDestructive and the MCP layer refuses it for a model
  // actor, returning a needs_human confirmation hand-off BEFORE the handler
  // runs (even with confirm:true). This is not an error — isError stays
  // unset — it is a clean hand-off to a human.
  const result = await invoke(mcp, 'pins_rm', { cids: [Cid], confirm: true });

  expect(result.isError).toBeUndefined();
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'needs_human' });
  expect(result).toHaveStructuredContent({ reason: 'confirmation' });

  // Because the destructive gate deferred to the human, the pin was NOT
  // removed — the store still holds it.
  const after = await invoke(mcp, 'pins_list', {});
  expect(isCleanSuccess(after)).toBe(true);
  expect(after).toHaveTextContent(Cid);
});
