// DOM bootstrap for the "Upload to IPFS" flow: adapt the ipfs-upload machine
// onto the elements of the Go-rendered Upload to IPFS HTML shell, and bridge
// the machine's out-of-band byte transfer to Uppy's XHR uploader.
//
// Element contract:
//   #ipfs-upload-form   the <form> that starts the upload.
//   #file               the file <input type="file"> (styled composite picker).
//   #file-name          the picked-file label span ("No file chosen").
//   #name               the upload-name <input>.
//   #ipfs-upload-status the status element (class "status <state>").
//   #out-cid            the result CID <code>.
//   #start              the start <button> (disabled while in-flight).
//
// Byte transport: the machine mints a one-time presigned PUT URL via the
// app-only ipfs_upload_submit helper, then this bootstrap runs Uppy's
// XHRUpload against that URL with an HTTP PUT of the raw file body (formData:
// false — the same out-of-band byte path the agent uses with `curl -T`). The
// 202 response's upload_handle feeds the app-only ipfs_upload_status poll for
// the final CID. No file bytes cross the MCP/LLM channel.

import { interpret } from "robot3";
import Uppy from "@uppy/core";
import XHRUpload from "@uppy/xhr-upload";
import {
  createIPFSUploadMachine,
  currentIPFSUploadState,
  isIPFSUploadTerminal,
  type IPFSUploadConfig,
  type IPFSUploadContext,
  type UploadXhr,
  IPFSUploadState,
} from "@/ipfs-upload";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Element ids referenced by the Go-rendered Upload to IPFS HTML shell. */
export type IPFSUploadElementIds = {
  form: string;
  file: string;
  fileName: string;
  name: string;
  status: string;
  outCid: string;
  start: string;
};

/** Data the Upload to IPFS app entry contributes, handed to mountIPFSUploadApp verbatim. */
export type IPFSUploadAppEntry = AppDefinition<IPFSUploadConfig, IPFSUploadElementIds>;

export interface IPFSUploadElements {
  form: { addEventListener(type: "submit", listener: (ev: SubmitEvent) => void): void };
  fileInput: HTMLInputElement;
  fileNameEl: HTMLElement;
  nameInput: { value: string };
  statusEl: HTMLElement;
  outCid: HTMLElement;
  startBtn: HTMLElement & { disabled?: boolean };
}

export interface IPFSUploadRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Whether to refresh the #out-cid readout from ctx.outCid. */
  setCid: boolean;
  /** Whether the upload is mid-flight (disable the start button). */
  pending: boolean;
}

/** Map a machine state + context onto the status/result readout. */
export function renderIPFSUpload(state: IPFSUploadState, ctx: IPFSUploadContext, cfg: IPFSUploadConfig): IPFSUploadRender {
  switch (state) {
    case IPFSUploadState.Minting:
      return { statusState: StatusClass.Pending, statusMsg: cfg.mintingMsg, setCid: false, pending: true };
    case IPFSUploadState.Uploading:
      return { statusState: StatusClass.Pending, statusMsg: cfg.uploadingMsg, setCid: false, pending: true };
    case IPFSUploadState.Polling:
      return { statusState: StatusClass.Pending, statusMsg: cfg.polledMsg, setCid: true, pending: true };
    case IPFSUploadState.Ok:
      return { statusState: StatusClass.Ok, statusMsg: cfg.uploadedMsg, setCid: true, pending: false };
    case IPFSUploadState.Error:
      return { statusState: StatusClass.Error, statusMsg: cfg.failedMsg, setCid: false, pending: false };
    default:
      return { statusState: null, statusMsg: null, setCid: false, pending: false };
  }
}

/**
 * Build the Uppy-backed XHR uploader: PUT the raw file body to the minted
 * presigned URL (formData: false, method: PUT) and resolve the upload_handle
 * from the 202 response body. This is the out-of-band byte transfer that the
 * machine's `uploading` invoke hands off to.
 */
export function makeUppyUploadXhr(): UploadXhr {
  return async (url: string, file: File) => {
    const uppy = new Uppy();
    uppy.use(XHRUpload, {
      endpoint: url,
      method: "PUT",
      formData: false,
      fieldName: "file",
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- Uppy 5 bins carry opaque metadata.
    const id = uppy.addFile({ name: file.name, type: file.type, data: file } as any);
    await uppy.upload();
    const f = uppy.getFile(id) as any;
    const body = f?.response?.body ?? {};
    uppy.destroy();
    const handle = String(body?.upload_handle ?? "");
    if (!handle) throw new Error("upload did not return an upload handle");
    return { handle };
  };
}

export interface IPFSUploadEntryOptions {
  config: IPFSUploadConfig;
  callTool: CallTool;
  elements: IPFSUploadElements;
  /** Overridden in tests; defaults to a real Uppy XHR uploader. */
  uploadXhr?: UploadXhr;
}

/**
 * Wire an ipfs-upload machine to the given elements. Returns an object with
 * `submit` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runIPFSUploadEntry(opts: IPFSUploadEntryOptions) {
  const uploadXhr = opts.uploadXhr ?? makeUppyUploadXhr();
  const machine = createIPFSUploadMachine(opts.config, opts.callTool, uploadXhr);
  const service = interpret(machine, (s) => {
    const state = currentIPFSUploadState(s);
    const ctx = s.context;
    const r = renderIPFSUpload(state, ctx, opts.config);
    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    if (opts.elements.startBtn) opts.elements.startBtn.disabled = r.pending;
    if (r.setCid) opts.elements.outCid.textContent = ctx.outCid || opts.elements.outCid.textContent;
  });

  // Reflect the picked file's name in the styled picker chrome. The native
  // input is invisible (see file-field/file-input in the theme), so without
  // this the user has no way to see what was selected.
  opts.elements.fileInput.addEventListener("change", () => {
    const picked = opts.elements.fileInput.files?.[0];
    opts.elements.fileNameEl.textContent = picked ? picked.name : "No file chosen";
  });

  const submit = (file: File | null, name: string) => {
    if (!file) {
      setStatus(opts.elements.statusEl, StatusClass.Error, opts.config.noFileMsg);
      return;
    }
    const st = currentIPFSUploadState(service);
    if (isIPFSUploadTerminal(st)) service.send({ type: "reset" });
    service.send({ type: "start", file, name });
  };

  opts.elements.form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const file = opts.elements.fileInput.files ? opts.elements.fileInput.files[0] : null;
    submit(file, opts.elements.nameInput.value.trim());
  });

  return {
    /** Programmatic submit with an explicit File (used by tests/demo). */
    submit: (file: File, name = "") => submit(file, name),
    get state(): IPFSUploadState {
      return currentIPFSUploadState(service);
    },
    service,
  };
}

/**
 * Mount the Upload to IPFS app entrypoint: wire the ipfs-upload machine to the
 * Go-rendered elements, and either run synchronously with a caller-supplied
 * `callTool` (tests/demo) or connect to the host over postMessage via bootApp,
 * advertising the CLI build version.
 */
export function mountIPFSUploadApp(def: IPFSUploadAppEntry, root: Document, callTool?: CallTool) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runIPFSUploadEntry({
      config: def.config,
      callTool: ct,
      elements: {
        form: byId<HTMLFormElement>(root, def.ids.form)!,
        fileInput: byId<HTMLInputElement>(root, def.ids.file)!,
        fileNameEl: byId<HTMLElement>(root, def.ids.fileName)!,
        nameInput: byId<HTMLInputElement>(root, def.ids.name)!,
        statusEl: statusEl!,
        outCid: byId<HTMLElement>(root, def.ids.outCid)!,
        startBtn: byId<HTMLElement & { disabled?: boolean }>(root, def.ids.start)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}
