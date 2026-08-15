import { entryBoot, boot } from "@/loader";
// Pin list MCP App — entrypoint bundle.
//
// The pin list is a read-only surface: it loads the authenticated account's
// pins (pins_list) and renders them for a human with their status. It never
// mutates pins and drives no hand-off — agents keep using the pins_* catalog
// tools directly. Config (tool names, element ids, message copy) stays
// data-driven here, matching how the other entries stay thin.

import { mountPinListApp, type PinListAppEntry } from "@/pin-list-bootstrap";
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

export default entryBoot(def, mountPinListApp);
boot(entryBoot(def, mountPinListApp));
