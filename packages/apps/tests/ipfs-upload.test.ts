// Behavioral tests for the robot3 ipfs-upload machine (tests/ipfs-upload.ts).
// Uses the same deferred (gated) promise pattern as tests/pin.test.ts to pause
// an `invoke` mid-flight and assert intermediate states: mint -> Uppy XHR ->
// status poll -> ok/error. File bytes never appear in any callTool argument;
// only the minted URL and the opaque handle cross the tool channel.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createIPFSUploadMachine,
  currentIPFSUploadState,
  type IPFSUploadConfig,
  type IPFSUploadContext,
  type UploadXhr,
  IPFSUploadState,
} from "@/ipfs-upload";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

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

interface GateItem {
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
}

describe("ipfs-upload machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let xhrCalls: { url: string; file: File }[];
  let gate: GateItem[];
  let machine: ReturnType<typeof createIPFSUploadMachine>;
  let service: Service<ReturnType<typeof createIPFSUploadMachine>>;
  const state = (): IPFSUploadState => currentIPFSUploadState(service);
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
        xhrCalls.push({ url, file });
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }

  beforeEach(() => {
    calls = [];
    xhrCalls = [];
    gate = [];
    machine = createIPFSUploadMachine(baseConfig, scriptedCallTool(), scriptedUploadXhr());
    service = interpret(machine, () => {});
  });

  function start() {
    service.send({ type: "start", file: new File(["abc"], "a.txt"), name: "a.txt" });
  }
  // Mint appears first in the gate; returns the minted URL.
  function mintGate(): GateItem {
    return gate[0];
  }
  // First XHR gate after mint.
  function xhrGate(): GateItem {
    return gate[1];
  }
  // Nth status-poll gate after mint + xhr.
  function pollGate(n: number): GateItem {
    return gate[n + 2];
  }

  it("start → mint → xhr → poll completed → ok with CID, no bytes in tool args", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);

    // Only the mint tool called; no file bytes in arguments.
    expect(calls.map((c) => c.name)).toEqual(["ipfs_upload_submit"]);
    expect(JSON.stringify(calls[0].arguments)).not.toContain("abc");

    mintGate().resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);

    // XHR must be invoked with the minted URL and the real File.
    expect(xhrCalls.length).toBe(1);
    expect(xhrCalls[0].url).toBe("http://host/upload/tok");
    expect(xhrCalls[0].file.name).toBe("a.txt");

    xhrGate().resolve({ handle: "h-42" });
    await untilState(service, IPFSUploadState.Polling);
    expect(ctx().handle).toBe("h-42");

    pollGate(0).resolve(statusOk("completed", "QmDone"));
    await untilState(service, IPFSUploadState.Ok);
    expect(ctx().outCid).toBe("QmDone");
  });

  it("running poll is non-terminal: keeps polling until completed", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    mintGate().resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);
    xhrGate().resolve({ handle: "h-1" });
    await untilState(service, IPFSUploadState.Polling);

    // Poll 1: still running → not terminal → poll again.
    pollGate(0).resolve(statusOk("running"));
    await until(service, () => gate.filter((g, i) => i >= 2).length > 1);
    expect(state()).toBe(IPFSUploadState.Polling);

    // Poll 2: completed → ok.
    pollGate(1).resolve(statusOk("completed", "QmTwo"));
    await untilState(service, IPFSUploadState.Ok);
    expect(ctx().outCid).toBe("QmTwo");
  });

  it("failed poll → error terminal", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    mintGate().resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);
    xhrGate().resolve({ handle: "h-2" });
    await untilState(service, IPFSUploadState.Polling);
    pollGate(0).resolve(statusOk("failed"));
    await untilState(service, IPFSUploadState.Error);
  });

  it("running until budget exhausted → error terminal", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    mintGate().resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);
    xhrGate().resolve({ handle: "h-3" });
    await untilState(service, IPFSUploadState.Polling);

    for (let i = 0; i < baseConfig.maxPoll; i++) {
      pollGate(i).resolve(statusOk("running"));
      if (i < baseConfig.maxPoll - 1) {
        await until(service, () => gate.filter((g, idx) => idx >= 2).length > i + 1);
        expect(state()).toBe(IPFSUploadState.Polling);
      }
    }
    await untilState(service, IPFSUploadState.Error);
  });

  it("mint isError → error terminal without XHR", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    mintGate().resolve({ isError: true, structuredContent: { error: "no endpoint" } });
    await untilState(service, IPFSUploadState.Error);
    expect(xhrCalls.length).toBe(0);
  });

  it("spaces polls so the iteration budget tracks wall-clock time, not round-trips", async () => {
    // A poll interval means success polls are separated by real time, so a
    // small iteration budget spans enough wall-clock for a slow async IPFS
    // task to finish instead of being exhausted by sub-ms loopback round-trips.
    const slowConfig: IPFSUploadConfig = { ...baseConfig, maxPoll: 20, pollIntervalMs: 40 };
    const slowService = interpret(createIPFSUploadMachine(slowConfig, scriptedCallTool(), scriptedUploadXhr()), () => {});
    const slowState = (): IPFSUploadState => currentIPFSUploadState(slowService);

    slowService.send({ type: "start", file: new File(["abc"], "a.txt"), name: "a.txt" });
    await untilState(slowService, IPFSUploadState.Minting);
    gate[0].resolve(mintOk("http://host/upload/tok"));
    await untilState(slowService, IPFSUploadState.Uploading);
    gate[1].resolve({ handle: "h-slow" });
    await untilState(slowService, IPFSUploadState.Polling);

    const t0 = Date.now();
    // The first poll gate appears only after the spacing sleep + callTool.
    await until(slowService, () => gate.length > 2);
    gate[2].resolve(statusOk("running"));
    await until(slowService, () => gate.length > 3);
    const elapsed = Date.now() - t0;
    expect(elapsed).toBeGreaterThanOrEqual(40);

    // Polls 1..18 stay running (gate[k] is poll k-2). Poll 19 (the last before
    // maxPoll=20 is exhausted) completes, so a task that simply takes many
    // spaced polls still lands in Ok and is never spuriously terminal-failed.
    for (let i = 3; i < slowConfig.maxPoll + 1; i++) {
      gate[i].resolve(statusOk("running"));
      await until(slowService, () => gate.length > i + 1);
    }
    gate[slowConfig.maxPoll + 1].resolve(statusOk("completed", "QmSlow"));
    await untilState(slowService, IPFSUploadState.Ok);
    expect(slowState()).toBe(IPFSUploadState.Ok);
  });

  it("XHR rejection → error terminal", async () => {
    start();
    await untilState(service, IPFSUploadState.Minting);
    mintGate().resolve(mintOk("http://host/upload/tok"));
    await untilState(service, IPFSUploadState.Uploading);
    xhrGate().reject(new Error("network down"));
    await untilState(service, IPFSUploadState.Error);
  });
});
