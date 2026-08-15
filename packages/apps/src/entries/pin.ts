import { entryBoot, boot } from "@/loader";
// Create Pin MCP App — entrypoint bundle.
//
// Create Pin is a form-driven flow, so it wires the dedicated pin machine
// (src/pin.ts) onto the elements of the Go-rendered Create Pin HTML shell.
// Config (tool names, element ids, message copy) stays data-driven here,
// matching how the other entries keep their per-app values thin.

import { mountPinApp, type PinAppEntry } from "@/pin-bootstrap";
export const def: PinAppEntry = {
  name: "CreatePin",
  config: {
    addTool: "pins_add",
    statusTool: "pin_status",
    maxAttempts: 24,
    pollDelayMs: 1500,
    cidRequiredMsg: "A CID is required.",
    pinningPrefix: "Pinning ",
    scheduledMsg: "Pin scheduled.",
    failedMsg: "Pin failed.",
    pinnedMsg: "Pinned.",
    currentStatusPrefix: "Current status: ",
    timeoutLastPrefix: "Timed out polling pin status (last: ",
    timeoutLastSuffix: ").",
    checkingMsg: "Checking pin status...",
    timeoutMsg: "Timed out polling pin status.",
  },
  ids: {
    form: "pin-form",
    cid: "cid",
    name: "name",
    status: "pin-status",
    outCid: "out-cid",
    outStatus: "out-status",
  },
};

export default entryBoot(def, mountPinApp);
boot(entryBoot(def, mountPinApp));
