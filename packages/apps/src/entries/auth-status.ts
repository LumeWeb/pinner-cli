// Account (auth status) MCP App — entrypoint bundle.
//
// The account strip is a read-only surface: it loads the authentication status
// (auth_status) and renders whether the human is authenticated plus the account
// message. It never mutates auth state and drives no hand-off — agents keep
// using the auth_* catalog tools directly. Config (tool name, element ids,
// message copy) stays data-driven here, matching how the other entries stay
// thin.

import { mountAuthStatusApp, type AuthStatusAppEntry } from "@/auth-status-bootstrap";
import type { CallTool } from "@/flow";

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

/**
 * Mount the app. With a caller-supplied `callTool` (tests/demo) wires
 * synchronously; otherwise boot connects to the host over postMessage and
 * wires on success, stamping the status element on connect failure.
 */
export function mount(root: Document = document, callTool?: CallTool) {
  return mountAuthStatusApp(def, root, callTool);
}

export { def as authStatusDefinition };
