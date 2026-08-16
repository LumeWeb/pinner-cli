// Pure upload orchestration state machine for the "Upload to IPFS" MCP App,
// authored with robot3. It drives the post-pick lifecycle: mint a one-time
// presigned PUT endpoint, run the out-of-band XHR byte transfer (Uppy), then
// poll the shared UploadTaskManager for the final CID.
//
// File bytes never enter this machine, the MCP tool channel, or the LLM
// channel: only the minted presigned URL and an opaque upload_handle do. The
// actual byte transfer is performed by the DOM bootstrap's Uppy XHR uploader
// against that presigned URL (the same out-of-band path the agent uses with
// `curl -T`). The machine stays free of DOM and Uppy so it can be unit-tested
// in node with stubbed callTool + uploadXhr.
//
// States: idle -> minting -> uploading -> polling -> ok | error.
//   minting  : call ipfs_upload_submit (app-only) to mint the presigned URL.
//   uploading: run uploadXhr(url, file) (injected; Uppy XHR PUT). Resolves
//              with the upload_handle from the 202 body.
//   polling  : loop ipfs_upload_status (app-only) until completed (CID) or a
//              terminal failed/cancelled state, within a bounded budget.

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export interface IPFSUploadConfig {
  /** app-only tool that mints the presigned PUT endpoint. */
  submitTool: string;
  /** app-only tool that polls async upload status. */
  statusTool: string;
  /** maximum polling iterations before giving up. */
  maxPoll: number;
  /** delay between status polls (ms). Spacing polls so the iteration budget
   *  tracks elapsed wall-clock time instead of raw round-trips: against a
   *  stdio/loopback transport each poll is sub-millisecond, so a budget
   *  counted only in iterations would exhaust far before a real async IPFS
   *  task completes. */
  pollIntervalMs: number;

  // Message copy.
  noFileMsg: string;
  mintingMsg: string;
  uploadingMsg: string;
  uploadingDoneMsg: string;
  polledMsg: string;
  uploadedMsg: string;
  failedMsg: string;
}

export interface IPFSUploadContext {
  /** Full picked File; only used to hand to the injected uploadXhr (never
   *  serialized over the tool channel). */
  file: File | null;
  /** upload name (defaults server-side). */
  name: string;
  /** minted presigned PUT URL. */
  url: string;
  /** opaque handle returned by the presigned PUT's 202 body. */
  handle: string;
  /** final CID readout. */
  outCid: string;
  polled: number;
}

/** Inject the out-of-band byte transfer. Given the minted presigned URL and the
 *  picked file, PUT the bytes (Uppy XHR) and resolve the upload_handle from the
 *  202 response. */
export type UploadXhr = (url: string, file: File) => Promise<{ handle: string }>;

/**
 * Build the ipfs-upload machine. `callTool` is the injected MCP tool caller and
 * `uploadXhr` the injected Uppy XHR byte transfer; both stay stubbable in tests.
 */
export function createIPFSUploadMachine(cfg: IPFSUploadConfig, callTool: CallTool, uploadXhr: UploadXhr) {
  const dSc = (ev: any): any => ev?.data?.structuredContent;
  const dIsError = (ev: any): boolean => !!ev?.data?.isError;

  // --- reducers -----------------------------------------------------------

  const setMeta = reduce((ctx: IPFSUploadContext, ev: any) => ({
    ...ctx,
    file: ev?.file ?? null,
    name: ev?.name ?? "",
  }));

  // Capture the minted presigned URL.
  const setUrl = reduce((ctx: IPFSUploadContext, ev: any) => {
    const sc = dSc(ev);
    const url = sc && typeof sc === "object" ? sc.url : undefined;
    return { ...ctx, url: url != null ? String(url) : "" };
  });

  // Capture the upload_handle returned by the XHR's 202 response. The invoke
  // done event carries the resolved value under `.data` (robot3 shape).
  const setHandle = reduce((ctx: IPFSUploadContext, ev: any) => ({
    ...ctx,
    handle: ev?.data?.handle ?? "",
  }));

  // Capture the CID from a completed poll result. The poll invoke resolves a
  // plain {terminalOk, cid} object, so read it from the done event's `.data`.
  const setResult = reduce((ctx: IPFSUploadContext, ev: any) => {
    const cid = ev?.data?.cid;
    return { ...ctx, outCid: cid != null ? String(cid) : "" };
  });

  // Increment the poll counter on each non-terminal poll round.
  const stepPoll = reduce((ctx: IPFSUploadContext) => ({
    ...ctx,
    polled: ctx.polled + 1,
  }));

  // --- guards -------------------------------------------------------------

  const mintOk = (_ctx: IPFSUploadContext, ev: any) => !dIsError(ev);
  const mintErr = (_ctx: IPFSUploadContext, ev: any) => dIsError(ev);
  const doneOk = (_ctx: IPFSUploadContext, ev: any) => ev?.data?.terminalOk;
  const doneFail = (_ctx: IPFSUploadContext, ev: any) => ev?.data?.terminalFail;

  // --- invokes ------------------------------------------------------------

  const mintUrl = (ctx: IPFSUploadContext): Promise<ToolResult> =>
    callTool({ name: cfg.submitTool, arguments: { name: ctx.name || undefined } });

  // Run the out-of-band byte transfer (Uppy XHR) against the minted presigned
  // URL and resolve the upload_handle from the 202 response. Throws on any
  // transfer failure so robot3 fires its native `error` event (deterministic),
  // per the invoke-done guard-data pitfall.
  const runXhr = async (ctx: IPFSUploadContext): Promise<{ handle: string }> => {
    if (!ctx.file) throw new Error(cfg.failedMsg);
    return uploadXhr(ctx.url, ctx.file);
  };

  // One status poll. Returns an outcome understood by the `done` transitions.
  // A completed/failed/cancelled state is terminal and wins; a running state
  // and a transport error both loop (bounded by maxPoll in the budget reducer).
  const pollOnce = async (ctx: IPFSUploadContext): Promise<any> => {
    // Budget exhausted: stop polling and report a terminal failure.
    if (ctx.polled >= cfg.maxPoll) {
      return { isError: false, terminalOk: false, terminalFail: true };
    }
    // Space polls so the iteration budget tracks elapsed wall-clock time
    // rather than raw round-trips (see pollIntervalMs on the config). A zero
    // interval leaves the invoke synchronous, which the fast tests rely on.
    if (cfg.pollIntervalMs > 0) {
      await sleep(cfg.pollIntervalMs);
    }
    let res: ToolResult;
    try {
      res = await callTool({ name: cfg.statusTool, arguments: { handle: ctx.handle } });
    } catch (e) {
      return { isError: false, terminalOk: false, terminalFail: false };
    }
    const sc: any = res.structuredContent || {};
    const state: string = sc.state;
    if (state === "completed") {
      return { isError: false, terminalOk: true, cid: sc.result?.cid ?? sc.result ?? "" };
    }
    if (state === "failed" || state === "cancelled") {
      return { isError: false, terminalFail: true };
    }
    return { isError: false, terminalOk: false, terminalFail: false };
  };

  // --- machine ------------------------------------------------------------

  return createMachine(
    {
      idle: state(transition("start", "minting", setMeta)),
      minting: invoke(
        mintUrl,
        transition("done", "uploading", guard(mintOk), setUrl),
        transition("done", "error", guard(mintErr)),
        transition("error", "error"),
      ),
      uploading: invoke(
        runXhr,
        transition("done", "polling", setHandle),
        transition("error", "error"),
      ),
      polling: invoke(
        pollOnce,
        transition("done", "ok", guard(doneOk), setResult),
        transition("done", "error", guard(doneFail)),
        transition("done", "polling", stepPoll),
        transition("error", "polling", stepPoll),
      ),
      ok: state(transition("reset", "idle")),
      error: state(transition("reset", "idle")),
    },
    () => ({ file: null, name: "", url: "", handle: "", outCid: "", polled: 0 }),
  );
}

export enum IPFSUploadState {
  Idle = "idle",
  Minting = "minting",
  Uploading = "uploading",
  Polling = "polling",
  Ok = "ok",
  Error = "error",
}

/** Read the current state of a robot3 service as the typed IPFSUploadState. */
export function currentIPFSUploadState(service: {
  machine?: { current?: string };
}): IPFSUploadState {
  return (service.machine?.current ?? IPFSUploadState.Idle) as IPFSUploadState;
}

export const isIPFSUploadTerminal = (s: IPFSUploadState): boolean =>
  s === IPFSUploadState.Ok || s === IPFSUploadState.Error;
