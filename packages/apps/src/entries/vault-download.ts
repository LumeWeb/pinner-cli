import { entryBoot, boot } from "@/loader";
// Download from Vault MCP App — entrypoint bundle.
//
// Download from Vault is a single submit -> callServerTool flow: the form
// gathers the vault path, an optional name, an optional host output_path, and a
// sink; the machine calls vault_get_file; the bootstrap renders the fetch_url
// (sink=drop) as a download link or the written output_path (sink=local).

import { mountDownloadApp, type DownloadAppEntry } from "@/download-bootstrap";
export const def: DownloadAppEntry = {
  name: "VaultDownload",
  config: {
    downloadTool: "vault_get_file",
    sourceArg: "vault_path",
    downloadingMsg: "Downloading from vault...",
    downloadedMsg: "Downloaded.",
    failedMsg: "Download failed.",
    noSourceMsg: "Enter a vault file path to download (e.g. vault:/docs/f.pdf).",
    dropAvailable: true,
  },
  ids: {
    form: "vault-download-form",
    source: "vault-source",
    name: "name",
    output: "output",
    sinkLocal: "sink-local",
    sinkDrop: "sink-drop",
    status: "vault-download-status",
    outLink: "out-link",
    outPath: "out-path",
    start: "start",
  },
};

export default entryBoot(def, mountDownloadApp);
boot(entryBoot(def, mountDownloadApp));
