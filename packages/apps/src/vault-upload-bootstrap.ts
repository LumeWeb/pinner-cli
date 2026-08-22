// DOM bootstrap for the "Upload to Vault" flow: adapt the vault-upload machine
// onto the elements of the Go-rendered Upload to Vault HTML shell, and bridge
// the machine's out-of-band byte transfer to Uppy's XHR uploader (raw body,
// formData off, HTTP PUT) against the minted presigned endpoint.
//
// Element contract:
//   #vault-upload-form   the <form> that submits the upload.
//   #vfile               the file <input type="file"> (styled composite picker).
//   #vfile-name          the picked-file label span ("No file chosen").
//   #vault-path          the vault destination path <input>.
//   #vault-upload-status the status element (class "status <state>").
//   #out-path            the result stored-path <code>.

import { interpret } from "robot3";
import Uppy from "@uppy/core";
import XHRUpload from "@uppy/xhr-upload";
import {
  createVaultUploadMachine,
  currentVaultUploadState,
  isVaultUploadTerminal,
  type VaultUploadConfig,
  type VaultUploadContext,
  type VaultUploadXhr,
  VaultUploadState,
} from "@/vault-upload";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Element ids referenced by the Go-rendered Upload to Vault HTML shell. */
export type VaultUploadElementIds = {
  form: string;
  file: string;
  fileName: string;
  vaultPath: string;
  status: string;
  outPath: string;
  start: string;
};

/** Data the Upload to Vault app entry contributes, handed to mountVaultUploadApp verbatim. */
export type VaultUploadAppEntry = AppDefinition<VaultUploadConfig, VaultUploadElementIds>;

export interface VaultUploadElements {
  form: { addEventListener(type: "submit", listener: (ev: SubmitEvent) => void): void };
  fileInput: HTMLInputElement;
  fileNameEl: HTMLElement;
  vaultPathInput: { value: string };
  statusEl: HTMLElement;
  outPath: HTMLElement;
  startBtn: HTMLElement & { disabled?: boolean };
}

export interface VaultUploadRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Whether to refresh the #out-path readout from ctx.outPath. */
  setOutPath: boolean;
  /** Whether the upload is mid-flight (disable the submit button). */
  pending: boolean;
}

/** Map a machine state + context onto the status/result readout. */
export function renderVaultUpload(state: VaultUploadState, ctx: VaultUploadContext, cfg: VaultUploadConfig): VaultUploadRender {
  switch (state) {
    case VaultUploadState.Minting:
      return { statusState: StatusClass.Pending, statusMsg: cfg.mintingMsg, setOutPath: false, pending: true };
    case VaultUploadState.Uploading:
      return { statusState: StatusClass.Pending, statusMsg: cfg.uploadingMsg, setOutPath: false, pending: true };
    case VaultUploadState.Ok:
      return { statusState: StatusClass.Ok, statusMsg: cfg.uploadedMsg, setOutPath: true, pending: false };
    case VaultUploadState.Error:
      return { statusState: StatusClass.Error, statusMsg: cfg.failedMsg, setOutPath: false, pending: false };
    default:
      return { statusState: null, statusMsg: null, setOutPath: false, pending: false };
  }
}

/**
 * Run a single out-of-band upload with a fresh Uppy instance: XHR PUT the raw
 * file body (formData off) to the minted presigned endpoint, mirroring the
 * agent's `curl -T`. Resolves with the stored vault path read from the PUT
 * response, or rejects on a failed/short write.
 */
export function createUploadXhr(): VaultUploadXhr {
  return (url, file) =>
    new Promise<{ vaultPath: string }>((resolve, reject) => {
      const uppy = new Uppy({ autoProceed: false });
      uppy.use(XHRUpload, {
        endpoint: url,
        method: "PUT",
        formData: false,
      });
      uppy.on("upload-success", (f) => {
        void uppy.destroy();
        const body: any = (f as any).response?.body;
        const vaultPath = body && typeof body === "object" ? body.vault_path : undefined;
        if (vaultPath != null) {
          resolve({ vaultPath: String(vaultPath) });
        } else {
          reject(new Error("vault URL missing from upload response"));
        }
      });
      uppy.on("error", (err) => {
        void uppy.destroy();
        reject(err);
      });
      const id = uppy.addFile({ name: file.name, type: file.type || "application/octet-stream", data: file });
      uppy.upload().catch((err) => {
        void uppy.destroy();
        reject(err);
      });
    });
}

export interface VaultUploadEntryOptions {
  config: VaultUploadConfig;
  callTool: CallTool;
  elements: VaultUploadElements;
}

/**
 * Wire a vault-upload machine to the given elements. Returns an object with
 * `submit` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runVaultUploadEntry(opts: VaultUploadEntryOptions) {
  const machine = createVaultUploadMachine(opts.config, opts.callTool, createUploadXhr());
  const service = interpret(machine, (s) => {
    const state = currentVaultUploadState(s);
    const ctx = s.context;
    const r = renderVaultUpload(state, ctx, opts.config);
    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    if (opts.elements.startBtn) opts.elements.startBtn.disabled = r.pending;
    if (r.setOutPath && ctx.outPath) opts.elements.outPath.textContent = ctx.outPath;
  });

  // Reflect the picked file's name in the styled picker chrome. The native
  // input is invisible (see file-field/file-input in the theme), so without
  // this the user has no way to see what was selected.
  opts.elements.fileInput.addEventListener("change", () => {
    const picked = opts.elements.fileInput.files?.[0];
    opts.elements.fileNameEl.textContent = picked ? picked.name : "No file chosen";
  });

  const submit = async (file: File | null, vaultPath: string) => {
    if (!file) {
      setStatus(opts.elements.statusEl, StatusClass.Error, opts.config.noFileMsg);
      return;
    }
    if (!vaultPath) {
      setStatus(opts.elements.statusEl, StatusClass.Error, opts.config.noPathMsg);
      return;
    }
    const st = currentVaultUploadState(service);
    if (isVaultUploadTerminal(st)) service.send({ type: "reset" });
    service.send({ type: "start", file, vaultPath });
  };

  opts.elements.form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const file = opts.elements.fileInput.files ? opts.elements.fileInput.files[0] : null;
    const vaultPath = opts.elements.vaultPathInput.value.trim();
    void submit(file, vaultPath);
  });

  return {
    /** Programmatic submit with an explicit File (used by tests/demo). */
    submit: (file: File, vaultPath = "") => void submit(file, vaultPath),
    get state(): VaultUploadState {
      return currentVaultUploadState(service);
    },
    service,
  };
}

/**
 * Mount the Upload to Vault app entrypoint: wire the vault-upload machine to
 * the Go-rendered elements, and either run synchronously with a caller-supplied
 * `callTool` (tests/demo) or connect to the host over postMessage via bootApp,
 * advertising the CLI build version.
 */
export function mountVaultUploadApp(def: VaultUploadAppEntry, root: Document, callTool?: CallTool) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runVaultUploadEntry({
      config: def.config,
      callTool: ct,
      elements: {
        form: byId<HTMLFormElement>(root, def.ids.form)!,
        fileInput: byId<HTMLInputElement>(root, def.ids.file)!,
        fileNameEl: byId<HTMLElement>(root, def.ids.fileName)!,
        vaultPathInput: byId<HTMLInputElement>(root, def.ids.vaultPath)!,
        statusEl: statusEl!,
        outPath: byId<HTMLElement>(root, def.ids.outPath)!,
        startBtn: byId<HTMLElement & { disabled?: boolean }>(root, def.ids.start)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}
