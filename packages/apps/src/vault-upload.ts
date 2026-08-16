// Pure upload orchestration state machine for the "Upload to Vault" MCP App,
// authored with robot3. It drives the post-pick lifecycle: mint a one-time
// presigned PUT endpoint bound to the destination vault path, run the
// out-of-band Uppy XHR upload against it (raw body, formData off, HTTP PUT),
// then surface the vault result. Unlike the IPFS upload app there is no poll
// loop: the vault write is synchronous, so the PUT response itself carries the
// stored vault path or an error.
//
// The machine is free of DOM and MCP-transport concerns so it can be
// unit-tested in node with a stubbed callTool and uploadXhr. The DOM bootstrap
// (vault-upload-bootstrap.ts) adapts these states onto real elements and wires
// the Uppy XHR uploader as the uploadXhr dependency.

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

export interface VaultUploadConfig {
  /** Name of the MCP tool that mints the presigned PUT endpoint. */
  submitTool: string;

  // Message copy.
  noFileMsg: string; // "Select a file to upload."
  noPathMsg: string; // "Enter a vault destination path."
  mintingMsg: string; // "Preparing upload..."
  uploadingMsg: string; // "Uploading..."
  uploadedMsg: string; // "Stored in the vault."
  failedMsg: string; // "Upload failed."
}

export interface VaultUploadContext {
  file: File | null;
  vaultPath: string;
  /** Minted presigned PUT endpoint for Uppy XHR to write into. */
  url: string;
  /** Rendered stored-path readout (from the PUT response). */
  outPath: string;
}

/**
 * The out-of-band byte transfer: given the minted URL and the picked File,
 * run Uppy's XHR uploader (PUT body) and resolve with the vault result when
 * the upload completes. Bytes never cross the MCP/LLM channel.
 */
export type VaultUploadXhr = (url: string, file: File) => Promise<{ vaultPath: string }>;

/**
 * Build the vault-upload machine. `callTool` mints the presigned endpoint and
 * `uploadXhr` runs the Uppy XHR upload; both are injected so the machine stays
 * testable with stubs.
 */
export function createVaultUploadMachine(
  cfg: VaultUploadConfig,
  callTool: CallTool,
  uploadXhr: VaultUploadXhr,
) {
  const dSc = (ev: any): any => ev?.data?.structuredContent;
  const dIsError = (ev: any): boolean => !!ev?.data?.isError;

  const setMeta = reduce((ctx: VaultUploadContext, ev: any) => ({
    ...ctx,
    file: ev?.file ?? null,
    vaultPath: ev?.vaultPath ?? "",
  }));

  // Capture the minted presigned URL (structuredContent.url).
  const setUrl = reduce((ctx: VaultUploadContext, ev: any) => {
    const sc = dSc(ev);
    const url = sc && typeof sc === "object" ? sc.url : undefined;
    return { ...ctx, url: url != null ? String(url) : "" };
  });

  // Capture the stored vault path from the XHR response (resolved value under
  // `.data` on the done event, robot3 shape).
  const setResult = reduce((ctx: VaultUploadContext, ev: any) => {
    const p = ev?.data?.vaultPath;
    return { ...ctx, outPath: p != null ? String(p) : "" };
  });

  const mintOk = (_ctx: VaultUploadContext, ev: any) => !dIsError(ev);
  const mintErr = (_ctx: VaultUploadContext, ev: any) => dIsError(ev);

  const mintUrl = (ctx: VaultUploadContext): Promise<ToolResult> =>
    callTool({
      name: cfg.submitTool,
      arguments: { vault_path: ctx.vaultPath },
    });

  const runXhr = async (ctx: VaultUploadContext): Promise<{ vaultPath: string }> => {
    if (!ctx.file) {
      throw new Error("no file selected");
    }
    return await uploadXhr(ctx.url, ctx.file);
  };

  return createMachine(
    {
      form: state(transition("start", "minting", setMeta)),
      minting: invoke(
        mintUrl,
        transition("done", "uploading", guard(mintOk), setUrl),
        transition("done", "error", guard(mintErr)),
        transition("error", "error"),
      ),
      uploading: invoke(
        runXhr,
        transition("done", "ok", setResult),
        transition("error", "error"),
      ),
      ok: state(transition("reset", "form")),
      error: state(transition("reset", "form")),
    },
    () => ({ file: null, vaultPath: "", url: "", outPath: "" }),
  );
}

export enum VaultUploadState {
  Form = "form",
  Minting = "minting",
  Uploading = "uploading",
  Ok = "ok",
  Error = "error",
}

/**
 * Read the current state of a robot3 service as the typed VaultUploadState.
 */
export function currentVaultUploadState(service: {
  machine?: { current?: string };
}): VaultUploadState {
  return (service.machine?.current ?? VaultUploadState.Form) as VaultUploadState;
}

export const isVaultUploadTerminal = (s: VaultUploadState): boolean =>
  s === VaultUploadState.Ok || s === VaultUploadState.Error;
