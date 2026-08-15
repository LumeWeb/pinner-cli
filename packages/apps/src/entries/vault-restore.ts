import { entryBoot, boot } from "@/loader";
// Restore Vault MCP App — entrypoint bundle.
import { mountFlowApp } from "@/app-entry";
import type { FlowAppEntry } from "./common";

export const def: FlowAppEntry = {
  name: "VaultRestore",
  config: {
    startTool: "vault_restore",
    statusTool: "vault_restore_status",
    urlFields: ["restore_url", "action_url"],
    maxAttempts: 60,
    pollDelayMs: 1500,
  },
  ids: { startBtn: "vault-restore-start", urlEl: "vault-restore-url", statusEl: "vault-restore-status" },
  copy: {
    actionLabel: "vault restore",
    startErrorMsg: "Vault restore did not return a setup handoff.",
    alreadyDoneMsg: "Vault already restored.",
    noHandlePrefix: "Could not start vault restore.",
    pendingMsg: "Waiting for the device approval...",
    doneMsg: "Vault restored.",
    deadDetailPrefix: "The vault restore session is no longer valid.",
    timeoutMsg: "Timed out waiting. Click start to retry.",
    retryWord: "start",
  },
};

export default entryBoot(def, mountFlowApp);
boot(entryBoot(def, mountFlowApp));
