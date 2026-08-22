import { test, expect } from 'sunpeak/test';
import { invoke, textOf, isCleanSuccess } from './helpers';

// This file MUST run its tests serially in a single worker: the pins flow is
// stateful (pins_add -> pins_list -> pins_status -> pins_update share
// server-side pin-store state AND module-level captures). sunpeak's base
// config sets `fullyParallel: true`, which normally gives every test its own
// worker and therefore a fresh module instance (a fresh random CID) — that
// would break the flow. Serial mode forces this file's tests to run in order
// in ONE worker.
test.describe.configure({ mode: 'serial' });

/**
 * Pins domain tools for pins_update + list filtering, driven through the
 * host-discovery contract: every call goes through invoke_tool (the
 * progressive-disclosure meta-tool) with { name, args }, never by calling the
 * direct tool name.
 *
 * CI-PENDING: this file is verified in CI (it drives tools through the real
 * MCP -> SDK -> fake-API stack). It cannot be run locally on constrained
 * hosts because launching the browser e2e suite OOMs (SIGKILL/exit 137). The
 * Go-side unit tests in internal/mcptest/ipfs/pins_test.go validate the same
 * fake endpoints via `go test -race ./internal/mcptest/...`.
 *
 * This file specifically regresses Task 18's fake-pins work:
 *   - list FILTERING: GET /pins now honors cid/name/status/limit/match
 *     filters instead of returning the whole store.
 *   - fetch-by-cid: pinner's pins_status / pins_update resolve a pin by CID
 *     (Status()/UpdatePin() call GET /pins?cid=... and take results[0]). The
 *     fake previously IGNORED the cid filter, so with more than one pin the
 *     client's results[0] was unreliable. This suite deliberately creates a
 *     TWO-pin store and asserts each CID is resolved to itself.
 *   - pins_update: POST /pins/{requestid} (boxo Replace) renames the pin.
 *
 * STATE SAFETY: the pin store is shared by BOTH host projects in a run
 * ([chatgpt] and [claude]) which run this file in separate processes against
 * the SAME store. To keep each project's flow isolated, this file mints its
 * OWN unique valid CIDs at load time; no other file/project can collide.
 *
 * CONTRACT NOTES (from internal/catalogops/pins.go):
 *   - pins_add takes `cids` (string slice) + optional `name` and returns the
 *     created pin with a `request_id` (derived by the fake as "req-<cid>").
 *   - pins_status takes `cid`.
 *   - pins_update takes `cid` (required) + `name`/`meta`/`clear-meta`; it is
 *     SafetyMutate (NOT destructive), so a model actor may invoke it directly —
 *     no confirmation handoff.
 *   - pins_list takes optional `name`, `search` (server-side substring name
 *     match), `status`, `limit`.
 */

// Mint unique, valid CIDv1 (base32, dashed "baf..." form) per pin — see
// pins.test.ts for the encoding. The random byte makes each CID unique to
// this module instance (and distinct per pin), isolating each host project's
// flow in the shared fake store.
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
const randBytes = () => Array.from({ length: 8 }, () => Math.floor(Math.random() * 256));
// High-entropy CIDs (8 random bytes) so they never collide across the shared
// fake pin store / host projects (a single random byte is only 1/256 odds).
// The multihash length byte (0x08) MUST match the byte count for valid CIDs.
const CidA = 'b' + base32([0x01, 0x70, 0x00, 0x08, ...randBytes()]);
const CidB = 'b' + base32([0x01, 0x70, 0x00, 0x08, ...randBytes()]);
const NameA = 'pins2-alpha';
const NameB = 'pins2-beta';

let capturedRequestIdA: string | undefined;

test('pins_add creates two pins for this file (on distinct cids)', async ({ mcp }) => {
  // Pin A.
  const addA = await invoke(mcp, 'pins_add', { cids: [CidA], name: NameA });
  expect(isCleanSuccess(addA)).toBe(true);
  expect(addA).not.toBeError();
  expect(addA).toHaveTextContent(CidA);
  expect(addA).toHaveTextContent('request_id');
  const textA = textOf(addA);
  const match = /"request_id"\s*:\s*"([^"]+)"/.exec(textA);
  expect(match).not.toBeNull();
  capturedRequestIdA = match![1];
  expect(capturedRequestIdA!.length).toBeGreaterThan(0);

  // Pin B on a distinct cid + name, so the store holds >1 pin (the premise
  // behind the fetch-by-cid regression).
  const addB = await invoke(mcp, 'pins_add', { cids: [CidB], name: NameB });
  expect(isCleanSuccess(addB)).toBe(true);
  expect(addB).not.toBeError();
  expect(addB).toHaveTextContent(CidB);
});

test('pins_list does not filter by name returns both pins', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_list', {});
  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(CidA);
  expect(result).toHaveTextContent(CidB);
});

test('pins_list search filter narrows to the matching pin (server-side)', async ({ mcp }) => {
  // search is a server-side substring name match (match=partial); it must
  // exclude the other pin even though both live in the shared store.
  const result = await invoke(mcp, 'pins_list', { search: NameB });
  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(CidB);
  expect(result).not.toHaveTextContent(CidA);
});

test('pins_list exact name filter narrows to the matching pin', async ({ mcp }) => {
  const result = await invoke(mcp, 'pins_list', { name: NameA });
  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveTextContent(CidA);
  expect(result).not.toHaveTextContent(CidB);
});

test('pins_status resolves each cid to itself in a two-pin store (cid filter works)', async ({ mcp }) => {
  // Regression for the known fake gap: before the fix, GET /pins ignored the
  // cid filter, so with a 2-pin store the client's results[0] was whichever
  // pin the map iterated first — status for A could report B. Now each cid
  // must resolve to ITS OWN pin (correct cid echo).
  const statusA = await invoke(mcp, 'pins_status', { cid: CidA });
  expect(isCleanSuccess(statusA)).toBe(true);
  expect(statusA).not.toBeError();
  expect(statusA).toHaveTextContent(CidA);
  expect(statusA).toHaveTextContent('pinned');

  const statusB = await invoke(mcp, 'pins_status', { cid: CidB });
  expect(isCleanSuccess(statusB)).toBe(true);
  expect(statusB).not.toBeError();
  expect(statusB).toHaveTextContent(CidB);
  expect(statusB).toHaveTextContent('pinned');
});

test('pins_update renames a pin by cid (round-trip)', async ({ mcp }) => {
  // pins_update is SafetyMutate — a model actor may invoke it directly (no
  // confirmation handoff). It resolves the pin by cid, then POSTs the new name
  // to /pins/{requestid}.
  const newName = NameA + '-renamed';
  const result = await invoke(mcp, 'pins_update', { cid: CidA, name: newName });

  expect(isCleanSuccess(result)).toBe(true);
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  expect(result).toHaveTextContent(CidA);

  // The rename must be persisted server-side: list by that name surfaces the
  // renamed pin and not the other one.
  const search = await invoke(mcp, 'pins_list', { search: newName });
  expect(isCleanSuccess(search)).toBe(true);
  expect(search).not.toBeError();
  expect(search).toHaveTextContent(CidA);
  expect(search).not.toHaveTextContent(CidB);
});

test('pins_update with an unknown cid returns a not-found error', async ({ mcp }) => {
  const unknownCid = 'b' + base32([0x01, 0x70, 0x00, 0x08, ...randBytes()]);
  const result = await invoke(mcp, 'pins_update', { cid: unknownCid, name: 'nope' });

  // The unknown pin must not silently update a wrong pin: the cid filter
  // returns no match -> the client reports pin-not-found.
  expect(result.isError).toBe(true);
  expect(textOf(result)).toMatch(/not found|ErrPinNotFound|no pin/i);
});
