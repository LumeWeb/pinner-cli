// Tests for the Upload to IPFS progress UI: the pure mapping from upload state
// to a progress-bar presentation (progressFor), the machine's capture of the
// async operational status (opState) from status polls, and the DOM bootstrap's
// progress controller wiring. The progress mapping stays in ipfs-upload.ts
// (free of DOM/Uppy) so it is unit-testable in node against the machine.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createIPFSUploadMachine,
  currentIPFSUploadState,
  progressFor,
  type IPFSUploadConfig,
  type IPFSUploadContext,
  type UploadXhr,
  IPFSUploadState,
} from "@/ipfs-upload";
import type { CallTool, ToolResult } from "@/flow";
import { untilState } from "./helpers";

const baseConfig: IPFSUploadConfig = {
  submitTool: "ipfs_upload_submit",
  statusTool: "ipfs_upload_status",
  maxPoll: 3,
  pollIntervalMs: 0,
  noFileMsg: "Select a file to upload.",
  mintingMsg: "Preparing upload...",
  uploadingMsg: "Uploading...",
  uploadingDoneMsg: "Upload complete.",
  polledMsg: "Waiting for pinning...",
  uploadedMsg: "Uploaded.",
  failedMsg: "Upload failed.",
};

function mintOk(url: string): ToolResult {
  return { structuredContent: { url } };
}
function statusOk(state: string, cid?: string): ToolResult {
  const sc: Record<string, unknown> = { state };
  if (cid !== undefined) sc.result = { cid };
  return { structuredContent: sc };
}

describe("ipfs-upload progress mapping (progressFor)", () => {
  const ctx = (opState = ""): IPFSUploadContext => ({
    file: null,
    name: "",
    url: "",
    handle: "",
    outCid: "",
    opState,
    polled: 0,
  });

  it("idle hides the bar", () => {
    expect(progressFor(IPFSUploadState.Idle, ctx())).toEqual({ mode: "hidden" });
  });

  it("minting/processing is indeterminate", () => {
    expect(progressFor(IPFSUploadState.Minting, ctx()).mode).toBe("processing");
  });

  it("uploading is determinate from 0%, labels the byte transfer", () => {
    const p = progressFor(IPFSUploadState.Uploading, ctx());
    expect(p.mode).toBe("uploading");
    expect(p.percent).toBe(0);
    expect(p.label).toContain("Uploading");
  });

  it("polling surfaces the operational status (queued/running) as the label", () => {
    expect(progressFor(IPFSUploadState.Polling, ctx("queued")).label).toBe("Upload queued…");
    expect(progressFor(IPFSUploadState.Polling, ctx("running")).label).toBe("Upload running…");
    // Unknown / empty operational status falls back to a generic label.
    expect(progressFor(IPFSUploadState.Polling, ctx()).label).toBe("Processing…");
    expect(progressFor(IPFSUploadState.Polling, ctx("completed")).label).toBe("Processing…");
    // Still an indeterminate (async) phase regardless of the label.
    expect(progressFor(IPFSUploadState.Polling, ctx("running")).mode).toBe("processing");
  });

  it("ok fills to 100% and completes the label", () => {
    const p = progressFor(IPFSUploadState.Ok, ctx());
    expect(p.mode).toBe("done");
    expect(p.percent).toBe(100);
    expect(p.label).toBe("Upload complete");
  });

  it("error is a distinct failure mode", () => {
    const p = progressFor(IPFSUploadState.Error, ctx());
    expect(p.mode).toBe("error");
    expect(p.label).toBe("Upload failed");
  });
});

describe("ipfs-upload machine captures operational status (opState)", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: { resolve: (v: unknown) => void; reject: (e: unknown) => void }[];
  let service: Service<ReturnType<typeof createIPFSUploadMachine>>;
  const ctx = () => service.context as IPFSUploadContext;

  function scriptedCallTool(): CallTool {
    return (req) =>
      new Promise<ToolResult>((resolve, reject) => {
        calls.push(req);
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }
  function scriptedUploadXhr(): UploadXhr {
    return (url, file) =>
      new Promise<{ handle: string }>((resolve, reject) => {
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }

  beforeEach(() => {
    calls = [];
    gate = [];
    service = interpret(
      createIPFSUploadMachine(baseConfig, scriptedCallTool(), scriptedUploadXhr()),
      () => {},
    );
  });

  function start() {
    service.send({ type: "start", file: new File(["abc"], "a.txt"), name: "a.txt" });
  }

  it("resets opState on start and captures queued → running → completed across polls", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    expect(ctx().opState).toBe("");

    gate[0].resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);
    expect(ctx().opState).toBe("");

    gate[1].resolve({ handle: "h-9" });
    await untilState(service, IPFSUploadState.Polling);

    // Poll 1: queued → opState captured, non-terminal.
    gate[2].resolve(statusOk("queued"));
    await until(service, () => gate.length > 3);
    expect(ctx().opState).toBe("queued");
    expect(state()).toBe(IPFSUploadState.Polling);

    // Poll 2: running → opState updated, non-terminal.
    gate[3].resolve(statusOk("running"));
    await until(service, () => gate.length > 4);
    expect(ctx().opState).toBe("running");

    // Poll 3: completed → terminal ok, opState reflects the final poll.
    gate[4].resolve(statusOk("completed", "QmProg"));
    await untilState(service, IPFSUploadState.Ok);
    expect(ctx().opState).toBe("completed");
    expect(ctx().outCid).toBe("QmProg");
  });

  function state() {
    return currentIPFSUploadState(service);
  }
});

// Local async wait helper (avoids importing robot3 internals in the describe).
async function until(service: { machine?: { current?: string } }, cond: () => boolean) {
  const deadline = Date.now() + 2000;
  while (!cond()) {
    if (Date.now() > deadline) throw new Error("timed out waiting for condition");
    await new Promise((r) => setTimeout(r, 5));
  }
}
