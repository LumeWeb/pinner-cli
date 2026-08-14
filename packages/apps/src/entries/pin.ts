// Create Pin MCP App — entrypoint bundle.
//
// Create Pin is a form-driven flow, so it wires the dedicated pin machine
// (src/pin.ts) onto the elements of the Go-rendered Create Pin HTML shell.
// Config (tool names, element ids, message copy) stays data-driven here,
// matching how the other entries keep their per-app values thin.

import { runPinEntry } from "@/pin-bootstrap";
import { bootApp } from "@/boot";
import type { PinConfig } from "@/pin";
import type { CallTool } from "@/flow";

export interface PinDefinition {
  name: string;
  config: PinConfig;
  /** Element ids referenced by the Go-rendered HTML shell. */
  ids: {
    form: string;
    cid: string;
    name: string;
    status: string;
    outCid: string;
    outStatus: string;
  };
}

const def: PinDefinition = {
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
export function mount(
  root: Document = document,
  callTool?: CallTool,
) {
  const statusEl = root.getElementById(def.ids.status) as HTMLElement | null;
  const wire = (ct: CallTool) =>
    runPinEntry({
      config: def.config,
      callTool: ct,
      elements: {
        form: root.getElementById(def.ids.form) as HTMLFormElement,
        cidInput: root.getElementById(def.ids.cid) as HTMLInputElement,
        nameInput: root.getElementById(def.ids.name) as HTMLInputElement,
        statusEl: statusEl as HTMLElement,
        outCid: root.getElementById(def.ids.outCid) as HTMLElement,
        outStatus: root.getElementById(def.ids.outStatus) as HTMLElement,
      },
    });
  if (callTool) return wire(callTool);
  bootApp({ name: def.name, version: "1.0.0" }, wire, statusEl);
}

export { def as pinDefinition };
