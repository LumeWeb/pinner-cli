// Shared DOM helpers for the MCP Apps. Each app resolves its Go-rendered
// elements by id and writes a status-class + message readout; these helpers
// keep that lookup/write mechanical work in one place.

/** Status readout classes shared by flow and pin apps. */
export type StatusClass = "ok" | "error" | "info" | "pending";

/**
 * Resolve a typed element by id. Returns `null` when the Go-rendered shell is
 * missing the id (guards null-carrying refs like the status element passed to
 * bootApp).
 */
export function byId<T extends HTMLElement>(root: Document, id: string): T | null {
  return root.getElementById(id) as T | null;
}

/**
 * Stamp the status element with `status <cls>` and a message. No-op when the
 * element is absent so mount helpers can tolerate an inert view.
 */
export function setStatus(el: HTMLElement | null, cls: StatusClass, msg: string): void {
  if (!el) return;
  el.className = "status " + cls;
  el.textContent = msg;
}
