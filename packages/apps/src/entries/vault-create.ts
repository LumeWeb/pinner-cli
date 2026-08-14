// Create Vault MCP App — entrypoint bundle.
import { mountFlowApp } from "@/app-entry";
import type { CallTool } from "@/flow";
import type { FlowAppEntry } from "./common";

export const def: FlowAppEntry = {
  name: "VaultCreate",
  config: {
    startTool: "vault_create",
    statusTool: "vault_create_status",
    urlFields: ["create_url", "action_url"],
    maxAttempts: 60,
    pollDelayMs: 1500,
  },
  ids: { startBtn: "vault-create-start", urlEl: "vault-create-url", statusEl: "vault-create-status" },
  copy: {
    actionLabel: "vault create",
    startErrorMsg: "Vault create did not return a setup handoff.",
    alreadyDoneMsg: "Vault already active.",
    noHandlePrefix: "Could not start vault create.",
    pendingMsg: "Waiting for the device approval and seed save...",
    doneMsg: "Vault created and seed saved.",
    deadDetailPrefix: "The vault create session is no longer valid.",
    timeoutMsg: "Timed out waiting. Click start to retry.",
    retryWord: "start",
  },
};

/**
 * Mount the app. With a caller-supplied `callTool` (tests/demo) wires
 * synchronously; otherwise boot connects to the host over postMessage and
 * wires on success, stamping the status element on connect failure.
 */
export function mount(root: Document = document, callTool?: CallTool) {
  return mountFlowApp(def, def.copy, root, callTool);
}

export { def as vaultCreateDefinition };
