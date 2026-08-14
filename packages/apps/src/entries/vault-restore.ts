// Restore Vault MCP App — entrypoint bundle.
import { runAppEntry } from "../app-entry";
import { bootApp } from "../boot";
import { toFlowConfig, type AppDefinition } from "./common";

const def: AppDefinition = {
  name: "VaultRestore",
  config: { startTool: "vault_restore", statusTool: "vault_restore_status", urlFields: ["restore_url", "action_url"], maxAttempts: 60, pollDelayMs: 1500 },
  ids: { startBtn: "vault-restore-start", urlEl: "vault-restore-url", statusEl: "vault-restore-status" },
  copy: {
    actionLabel: "vault restore",
    startErrorMsg: "Vault restore did not return a setup handoff.",
    alreadyDoneMsg: "Vault already restored.",
    noHandlePrefix: "Could not start vault restore.",
    pendingMsg: "Waiting for the recovery seed submission...",
    doneMsg: "Vault restored.",
    deadDetailPrefix: "The vault restore session is no longer valid.",
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

export { def as vaultRestoreDefinition };
