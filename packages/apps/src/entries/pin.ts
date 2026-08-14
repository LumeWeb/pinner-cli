// Create Pin MCP App — entrypoint bundle.
//
// Create Pin is a form-driven flow, so it wires the dedicated pin machine
// (src/pin.ts) onto the elements of the Go-rendered Create Pin HTML shell.
// Config (tool names, element ids, message copy) stays data-driven here,
// matching how the other entries keep their per-app values thin.

import { mountPinApp, type PinAppEntry } from "@/pin-bootstrap";
import type { CallTool } from "@/flow";

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

/**
 * Mount the app. With a caller-supplied `callTool` (tests/demo) wires
 * synchronously; otherwise boot connects to the host over postMessage and
 * wires on success, stamping the status element on connect failure.
 */
export function mount(root: Document = document, callTool?: CallTool) {
  return mountPinApp(def, root, callTool);
}

export { def as pinDefinition };
