import { entryBoot, boot } from "@/loader";
// Create Vault MCP App — entrypoint bundle.
import { mountFlowApp } from "@/app-entry";
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

export default entryBoot(def, mountFlowApp);
boot(entryBoot(def, mountFlowApp));
