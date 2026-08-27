import { entryBoot, boot } from "@/loader";
// Auth SSO Sign In MCP App — entrypoint bundle.
import { mountFlowApp } from "@/app-entry";
import type { FlowAppEntry } from "./common";

export const def: FlowAppEntry = {
  name: "AuthSSO",
  config: {
    startTool: "auth_sso",
    statusTool: "auth_sso_status",
    urlFields: ["action_url"],
    maxAttempts: 60,
    pollDelayMs: 1500,
  },
  ids: { startBtn: "sso-start", urlEl: "sso-url", statusEl: "sso-status", revokeBtn: "sso-revoke" },
  copy: {
    actionLabel: "sign-in",
    startErrorMsg: "Auth did not return an approval handoff.",
    alreadyDoneMsg: "Already signed in.",
    noHandlePrefix: "Could not start sign-in.",
    pendingMsg: "Waiting for approval in the browser...",
    inUseMsg: "A sign-in is already in progress. Approve it in the browser, or revoke to start a new one.",
    revokeTool: "auth_sso_revoke",
    doneMsg: "Signed in.",
    deadDetailPrefix: "Sign-in no longer active.",
    timeoutMsg: "Timed out waiting for approval. Click start to retry.",
    retryWord: "sign in",
  },
};

export default entryBoot(def, mountFlowApp);
boot(entryBoot(def, mountFlowApp));
