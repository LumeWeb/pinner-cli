import { entryBoot, boot } from "@/loader";
// Vault browser MCP App — entrypoint bundle.
//
// The vault browser is a read-only surface: it loads the vault status and the
// current path's listing and renders them for a human, letting the human
// navigate into directories. It never mutates the vault — agents keep using the
// vault_* catalog tools directly. Config (tool names, element ids, message
// copy) stays data-driven here, matching how the other entries stay thin.

import { mountVaultBrowserApp, type VaultBrowserAppEntry } from "@/vault-browser-bootstrap";
export const def: VaultBrowserAppEntry = {
  name: "VaultBrowser",
  config: {
    statusTool: "vault_status",
    listTool: "vault_ls",
    rootPath: "vault:/",
    loadingMsg: "Reading vault...",
    errorMsg: "Could not read vault",
    refreshLabel: "Refresh",
    upLabel: "Up",
    rootLabel: "Root",
    emptyLabel: "This vault directory is empty.",
    remoteDownMsg: "Vault index not reachable — showing local cache.",
  },
  ids: {
    status: "vault-status",
    path: "vault-path",
    list: "vault-list",
    empty: "vault-empty",
    up: "vault-up",
    root: "vault-root",
    refresh: "vault-refresh",
  },
};

export default entryBoot(def, mountVaultBrowserApp);
boot(entryBoot(def, mountVaultBrowserApp));
