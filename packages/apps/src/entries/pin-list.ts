// Pin list MCP App — entrypoint bundle.
//
// The pin list is a read-only surface: it loads the authenticated account's
// pins (pins_list) and renders them for a human with their status. It never
// mutates pins and drives no hand-off — agents keep using the pins_* catalog
// tools directly. Config (tool names, element ids, message copy) stays
// data-driven here, matching how the other entries stay thin.

import { mountPinListApp, type PinListAppEntry } from "@/pin-list-bootstrap";
import type { CallTool } from "@/flow";

export const def: PinListAppEntry = {
  name: "PinList",
  config: {
    listTool: "pins_list",
    loadingMsg: "Reading pins...",
    errorMsg: "Could not read pins",
    refreshLabel: "Refresh",
    emptyLabel: "No pins yet.",
  },
  ids: {
    status: "pinlist-status",
    count: "pinlist-count",
    table: "pinlist-table",
    empty: "pinlist-empty",
    refresh: "pinlist-refresh",
  },
};

/**
 * Mount the app. With a caller-supplied `callTool` (tests/demo) wires
 * synchronously; otherwise boot connects to the host over postMessage and
 * wires on success, stamping the status element on connect failure.
 */
export function mount(root: Document = document, callTool?: CallTool) {
  return mountPinListApp(def, root, callTool);
}

export { def as pinListDefinition };
