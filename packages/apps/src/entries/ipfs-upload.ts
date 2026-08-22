import { entryBoot, boot } from "@/loader";
// Upload to IPFS MCP App — entrypoint bundle.
//
// Upload to IPFS is a mint -> Uppy-XHR -> poll flow, so it wires the dedicated
// ipfs-upload machine (src/ipfs-upload.ts) onto the elements of the Go-rendered
// Upload to IPFS HTML shell. Config (tool names, element ids, message copy)
// stays data-driven here, matching how the other entries keep their per-app
// values thin.

import { mountIPFSUploadApp, type IPFSUploadAppEntry } from "@/ipfs-upload-bootstrap";
export const def: IPFSUploadAppEntry = {
  name: "IPFSUpload",
  config: {
    submitTool: "ipfs_upload_submit",
    statusTool: "ipfs_upload_status",
    maxPoll: 180,
    pollIntervalMs: 500,
    noFileMsg: "Select a file to upload.",
    mintingMsg: "Preparing upload...",
    uploadingMsg: "Uploading...",
    uploadingDoneMsg: "Upload complete.",
    polledMsg: "Waiting for pinning...",
    uploadedMsg: "Uploaded.",
    failedMsg: "Upload failed.",
  },
  ids: {
    form: "ipfs-upload-form",
    file: "file",
    fileName: "file-name",
    name: "name",
    status: "ipfs-upload-status",
    outCid: "out-cid",
    start: "start",
  },
};

export default entryBoot(def, mountIPFSUploadApp);
boot(entryBoot(def, mountIPFSUploadApp));
