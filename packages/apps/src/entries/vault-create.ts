// Create Vault MCP App — entrypoint bundle.
import { runAppEntry } from "@/app-entry";
import { bootApp } from "@/boot";
import { toFlowConfig, type AppDefinition } from "./common";

const def: AppDefinition = {
  name: "VaultCreate",
  config: { startTool: "vault_create", statusTool: "vault_create_status", urlFields: ["create_url", "action_url"], maxAttempts: 60, pollDelayMs: 1500 },
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
export function mount(root: Document = document, callTool?: Parameters<typeof runAppEntry>[0]["callTool"]) {
  const cfg = toFlowConfig(def);
  const statusEl = root.getElementById(def.ids.statusEl) as HTMLElement | null;
  const wire = (ct: Parameters<typeof runAppEntry>[0]["callTool"]) =>
    runAppEntry({
      config: cfg,
      callTool: ct,
      elements: {
        startBtn: root.getElementById(def.ids.startBtn) as HTMLElement & { disabled?: boolean },
        urlEl: root.getElementById(def.ids.urlEl) as HTMLElement,
        statusEl: statusEl as HTMLElement,
      },
    });
  if (callTool) return wire(callTool);
  bootApp({ name: def.name, version: "1.0.0" }, wire, statusEl);
}

export { def as vaultCreateDefinition };
