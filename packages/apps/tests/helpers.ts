// Shared test helpers for the apps MCP-bundle vitest suites.

/**
 * Drive robot3's microtask/promise queue until a machine settles. Both the
 * flow and pin suites interleave resolves (for the gated start/poll/submit
 * calls) with state assertions, and robot3's interpret callbacks run on
 * microtasks; flushing a bounded number of turns settles intermediate states.
 */
export async function flush<T>(service: T, rounds = 50): Promise<void> {
  for (let i = 0; i < rounds; i++) {
    await new Promise<void>((r) => setTimeout(r, 0));
  }
}
