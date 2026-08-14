// Shared test helpers for the apps MCP-bundle vitest suites.

/** Minimal shape of a robot3 service we need to observe a machine's current state. */
interface ServiceLike {
  machine?: { current?: unknown };
}

function currentOf(service: ServiceLike): string {
  return String(service.machine?.current);
}

/**
 * Resolve once the machine reaches `expected`, failing loudly if it never does
 * within `timeoutMs`. robot3 has no "await settled" promise — invoke
 * continuations run on microtasks after a gated call resolves — so we poll the
 * machine's current state each macrotask turn until it matches. This is
 * deterministic where a fixed-round synchronous flush is not: a machine stuck
 * in an unintended state throws here instead of letting a later assertion
 * silently pass against the wrong state.
 */
export async function untilState(
  service: ServiceLike,
  expected: string,
  timeoutMs = 500,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (currentOf(service) === expected) return;
    await new Promise<void>((r) => setTimeout(r, 0));
  }
  throw new Error(`machine did not settle at '${expected}' (last state: ${currentOf(service)})`);
}

/**
 * Resolve once `predicate` holds for the machine's current state, failing
 * loudly on timeout. Use this for loops that drain several polls before a
 * target state, where a single named state isn't known up front.
 */
export async function until(
  service: ServiceLike,
  predicate: (state: string) => boolean,
  timeoutMs = 500,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate(currentOf(service))) return;
    await new Promise<void>((r) => setTimeout(r, 0));
  }
  throw new Error(`machine did not settle (last state: ${currentOf(service)})`);
}
