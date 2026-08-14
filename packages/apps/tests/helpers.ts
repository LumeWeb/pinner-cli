// Shared test helpers for the apps MCP-bundle vitest suites.

/** A robot3 service exposing its machine's current state as `S`. */
export interface StatefulService<S extends string> {
  machine: { current: S };
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
export async function untilState<S extends string>(
  service: StatefulService<S>,
  expected: S,
  timeoutMs = 500,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (service.machine.current === expected) return;
    await new Promise<void>((r) => setTimeout(r, 0));
  }
  throw new Error(
    `machine did not settle at '${expected}' (last state: ${service.machine.current})`,
  );
}

/**
 * Resolve once `predicate` holds for the machine's current state, failing
 * loudly on timeout. Use this for loops that drain several polls before a
 * target state, where a single named state isn't known up front.
 */
export async function until<S extends string>(
  service: StatefulService<S>,
  predicate: (state: S) => boolean,
  timeoutMs = 500,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate(service.machine.current)) return;
    await new Promise<void>((r) => setTimeout(r, 0));
  }
  throw new Error(`machine did not settle (last state: ${service.machine.current})`);
}
