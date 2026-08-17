import { entryBoot, boot } from "@/loader";
// Download from IPFS MCP App — entrypoint bundle.
//
// Download from IPFS is a single submit -> callServerTool flow: the form
// gathers the CID/path, an optional name, an optional host output_path, and a
// sink; the machine calls download_file; the bootstrap renders the fetch_url
// (sink=drop) as a download link or the written output_path (sink=local).

import { mountDownloadApp, type DownloadAppEntry } from "@/download-bootstrap";
export const def: DownloadAppEntry = {
  name: "IPFSDownload",
  config: {
    downloadTool: "download_file",
    sourceArg: "ipfs_path",
    downloadingMsg: "Downloading from IPFS...",
    downloadedMsg: "Downloaded.",
    failedMsg: "Download failed.",
    noSourceMsg: "Enter a CID or CID/path to download.",
    dropAvailable: true,
  },
  ids: {
    form: "ipfs-download-form",
    source: "ipfs-source",
    name: "name",
    output: "output",
    sinkLocal: "sink-local",
    sinkDrop: "sink-drop",
    status: "ipfs-download-status",
    outLink: "out-link",
    outPath: "out-path",
    start: "start",
  },
};

export default entryBoot(def, mountDownloadApp);
boot(entryBoot(def, mountDownloadApp));
