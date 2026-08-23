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
  /** Optional canonical upload handle already prepared by the model-facing
   *  upload_file tool. When set, submitTool (ipfs_upload_submit) CONTINUES
   *  that same operation (same URL + handle) so this App fulfills the model's
   *  canonical upload instead of starting a sibling. Omit for a standalone
   *  prepare-a-fresh-upload flow. */
  seedHandle?: string;
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
  /** raw lifecycle state of the async upload task as reported by the last
   *  status poll (e.g. "queued", "running", "completed"). Surfaced as the
   *  operational status from the account area while polling. */
  opState: string;
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
    opState: "",
  }));

  // Capture the raw async task lifecycle state returned by a status poll so
  // the UI can render the operational status (queued/running/…) as its label.
  const setOpState = reduce((ctx: IPFSUploadContext, ev: any) => {
    const st = ev?.data?.opState;
    return { ...ctx, opState: typeof st === "string" ? st : "" };
  });

  // Capture the minted presigned URL and, when the submit continued an
  // already-prepared canonical operation, the upload_handle it carries — so the
  // machine polls the SAME handle the model's upload_file prepared (the XHR 202
  // returns the same handle again, but capturing it here keeps the continue path
  // correct even before the byte transfer).
  const setUrl = reduce((ctx: IPFSUploadContext, ev: any) => {
    const sc = dSc(ev);
    const url = sc && typeof sc === "object" ? sc.url : undefined;
    const preHandle = sc && typeof sc === "object" ? sc.upload_handle : undefined;
    return {
      ...ctx,
      url: url != null ? String(url) : "",
      handle: preHandle != null && preHandle !== "" ? String(preHandle) : ctx.handle,
    };
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
    callTool({
      name: cfg.submitTool,
      arguments: {
        name: ctx.name || undefined,
        // Continue the model-prepared operation so this App fulfills the SAME
        // canonical handle (no sibling upload) instead of minting a fresh one.
        ...(cfg.seedHandle ? { handle: cfg.seedHandle } : {}),
      },
    });

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
  // The raw `state` (queued/running/completed/…) rides along as `opState` so
  // the UI can surface the operational status from the account area.
  const pollOnce = async (ctx: IPFSUploadContext): Promise<any> => {
    // Budget exhausted: stop polling and report a terminal failure.
    if (ctx.polled >= cfg.maxPoll) {
      return { isError: false, terminalOk: false, terminalFail: true, opState: "" };
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
      return { isError: false, terminalOk: false, terminalFail: false, opState: "" };
    }
    const sc: any = res.structuredContent || {};
    const state: string = sc.state;
    if (state === "completed") {
      return { isError: false, terminalOk: true, cid: sc.result?.cid ?? sc.result ?? "", opState: state };
    }
    if (state === "failed" || state === "cancelled") {
      return { isError: false, terminalFail: true, opState: state };
    }
    return { isError: false, terminalOk: false, terminalFail: false, opState: state || "" };
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
        transition("done", "ok", guard(doneOk), setResult, setOpState),
        transition("done", "error", guard(doneFail), setOpState),
        transition("done", "polling", stepPoll, setOpState),
        transition("error", "polling", stepPoll),
      ),
      ok: state(transition("reset", "idle")),
      error: state(transition("reset", "idle")),
    },
    () => ({ file: null, name: "", url: "", handle: "", outCid: "", opState: "", polled: 0 }),
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

/** Progress-bar display mode. `uploading` is determinate (Uppy byte-transfer
 *  %), `processing` is indeterminate (async operational phase), `done` fills
 *  to 100%, and `error`/`hidden` drop or hide the bar. */
export type ProgressMode = "hidden" | "uploading" | "processing" | "done" | "error";

/** What a progress bar should present for a given upload state + context. */
export interface IPFSUploadProgress {
  mode: ProgressMode;
  /** Determinate percent (0-100) for `uploading`/`done`. */
  percent?: number;
  /** Human label under the bar: phase, or the operational state while polling. */
  label?: string;
}

/** Map an upload state + context onto a progress-bar presentation. This is pure
 *  (no DOM/Uppy) so it is unit-testable against the machine in node. */
export function progressFor(state: IPFSUploadState, ctx: IPFSUploadContext): IPFSUploadProgress {
  switch (state) {
    case IPFSUploadState.Minting:
      return { mode: "processing", label: "Preparing upload…" };
    case IPFSUploadState.Uploading:
      return { mode: "uploading", percent: 0, label: "Uploading…" };
    case IPFSUploadState.Polling: {
      // Surface the async operational status reported by the account area:
      // "queued"/"running" become "Upload queued…"/"Upload running…".
      const op = ctx.opState === "queued" || ctx.opState === "running" ? ctx.opState : "";
      return { mode: "processing", label: op ? `Upload ${op}…` : "Processing…" };
    }
    case IPFSUploadState.Ok:
      return { mode: "done", percent: 100, label: "Upload complete" };
    case IPFSUploadState.Error:
      return { mode: "error", label: "Upload failed" };
    default:
      return { mode: "hidden" };
  }
}
