// Shared DOM helpers for the MCP Apps. Each app resolves its Go-rendered
// elements by id and writes a status-class + message readout; these helpers
// keep that lookup/write mechanical work in one place.

/** Status readout classes shared by flow and pin apps. */
export enum StatusClass {
  Ok = "ok",
  Error = "error",
  Info = "info",
  Pending = "pending",
}

/** Tailwind text-color utility per status state (theme token: --color-status-*). */
const STATUS_TEXT_CLASS: Record<StatusClass, string> = {
  [StatusClass.Ok]: "text-status-ok",
  [StatusClass.Error]: "text-status-error",
  [StatusClass.Info]: "text-status-info",
  [StatusClass.Pending]: "text-status-pending",
};

/**
 * Resolve a typed element by id. Returns `null` when the Go-rendered shell is
 * missing the id (guards null-carrying refs like the status element passed to
 * bootApp).
 */
export function byId<T extends HTMLElement>(root: Document, id: string): T | null {
  return root.getElementById(id) as T | null;
}

/**
 * Stamp the status element with the shared base class plus a Tailwind color
 * utility and a message. No-op when the element is absent so mount helpers can
 * tolerate an inert view.
 */
export function setStatus(el: HTMLElement | null, cls: StatusClass, msg: string): void {
  if (!el) return;
  el.className = "status " + (STATUS_TEXT_CLASS[cls] ?? "text-status-info");
  el.textContent = msg;
}
