import { entryBoot, boot } from "@/loader";
// Upload to Vault MCP App — entrypoint bundle.
//
// Upload to Vault is a form-driven single-shot flow, so it wires the dedicated
// vault-upload machine (src/vault-upload.ts) onto the elements of the
// Go-rendered Upload to Vault HTML shell. Config (tool names, element ids,
// message copy) stays data-driven here, matching how the other entries keep
// their per-app values thin.

import { mountVaultUploadApp, type VaultUploadAppEntry } from "@/vault-upload-bootstrap";
export const def: VaultUploadAppEntry = {
  name: "VaultUpload",
  config: {
    submitTool: "vault_upload_submit",
    noFileMsg: "Select a file to upload.",
    noPathMsg: "Enter a vault destination path.",
    mintingMsg: "Preparing upload...",
    uploadingMsg: "Uploading...",
    uploadedMsg: "Stored in the vault.",
    failedMsg: "Upload failed.",
  },
  ids: {
    form: "vault-upload-form",
    file: "vfile",
    vaultPath: "vault-path",
    status: "vault-upload-status",
    outPath: "out-path",
    start: "vstart",
  },
};

export default entryBoot(def, mountVaultUploadApp);
boot(entryBoot(def, mountVaultUploadApp));
