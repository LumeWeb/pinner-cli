import { entryBoot, boot } from "@/loader";
// Account (auth status) MCP App — entrypoint bundle.
//
// The account strip is a read-only surface: it loads the authentication status
// (auth_status) and renders whether the human is authenticated plus the account
// message. It never mutates auth state and drives no hand-off — agents keep
// using the auth_* catalog tools directly. Config (tool name, element ids,
// message copy) stays data-driven here, matching how the other entries stay
// thin.

import { mountAuthStatusApp, type AuthStatusAppEntry } from "@/auth-status-bootstrap";
export const def: AuthStatusAppEntry = {
  name: "AuthStatus",
  config: {
    statusTool: "auth_status",
    loadingMsg: "Checking account...",
    errorMsg: "Could not read account status",
    refreshLabel: "Refresh",
    authenticatedMsg: "Authenticated.",
    notAuthenticatedMsg: "Not authenticated.",
  },
  ids: {
    status: "authstatus-status",
    outcome: "authstatus-outcome",
    message: "authstatus-message",
    refresh: "authstatus-refresh",
  },
};

export default entryBoot(def, mountAuthStatusApp);
boot(entryBoot(def, mountAuthStatusApp));
