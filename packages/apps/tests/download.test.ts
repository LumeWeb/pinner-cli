// Behavioral tests for the robot3 download machine (src/download.ts), shared
// by the "Download from IPFS" and "Download from Vault" MCP Apps. A download is
// a single callServerTool submit: the machine calls the sink-aware tool
// (download_file / vault_get_file) once, then surfaces the fetch_url (sink=drop)
// or the written output_path (sink=local). No file bytes ever cross the tool
// channel — only the source identifier, sink, and optional name/output path.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createDownloadMachine,
  currentDownloadState,
  type DownloadConfig,
  type DownloadContext,
  DownloadState,
} from "@/download";
import type { CallTool, ToolResult } from "@/flow";
import { untilState } from "./helpers";

const baseConfig: DownloadConfig = {
  downloadTool: "download_file",
  sourceArg: "ipfs_path",
  downloadingMsg: "Downloading from IPFS...",
  downloadedMsg: "Downloaded.",
  failedMsg: "Download failed.",
  noSourceMsg: "Enter a CID to download.",
  dropAvailable: true,
};

interface GateItem {
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
}

describe("download machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: GateItem[];
  let machine: ReturnType<typeof createDownloadMachine>;
  let service: Service<ReturnType<typeof createDownloadMachine>>;
  const state = (): DownloadState => currentDownloadState(service);
  const ctx = () => service.context as DownloadContext;

  function scriptedCallTool(): CallTool {
    return (req) =>
      new Promise<ToolResult>((resolve, reject) => {
        calls.push(req);
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }

  beforeEach(() => {
    calls = [];
    gate = [];
    machine = createDownloadMachine(baseConfig, scriptedCallTool());
    service = interpret(machine, () => {});
  });

  function start(source = "bafyabc/doc.txt", sink = "local", name = "", outputPath = "") {
    service.send({ type: "start", source, sink, name, outputPath });
  }

  it("start → callTool with source+sink → ok; renders fetch_url for drop", async () => {
    start("bafyabc/doc.txt", "drop");
    await untilState(service, DownloadState.Downloading);

    // One tool call; dropped bytes never appear; args carry source + sink.
    expect(calls.map((c) => c.name)).toEqual(["download_file"]);
    expect(calls[0].arguments).toEqual({ ipfs_path: "bafyabc/doc.txt", sink: "drop" });
    expect(JSON.stringify(calls[0].arguments)).not.toContain("bytes");

    gate[0].resolve({ structuredContent: { status: "ok", sink: "drop", fetch_url: "http://host/download/tok" } });
    await untilState(service, DownloadState.Ok);
    expect(ctx().fetchUrl).toBe("http://host/download/tok");
    expect(ctx().outputPathResult).toBe("");
  });

  it("local sink surfaces the written host output_path", async () => {
    start("bafyabc/doc.txt", "local", "doc.txt", "/data/out/doc.txt");
    await untilState(service, DownloadState.Downloading);

    expect(calls[0].arguments).toEqual({
      ipfs_path: "bafyabc/doc.txt",
      sink: "local",
      name: "doc.txt",
      output_path: "/data/out/doc.txt",
    });

    gate[0].resolve({ structuredContent: { status: "ok", sink: "local", output_path: "/data/out/doc.txt" } });
    await untilState(service, DownloadState.Ok);
    expect(ctx().outputPathResult).toBe("/data/out/doc.txt");
    expect(ctx().fetchUrl).toBe("");
  });

  it("isError → error terminal", async () => {
    start();
    await untilState(service, DownloadState.Downloading);
    gate[0].resolve({ isError: true, structuredContent: { error: "source down" } });
    await untilState(service, DownloadState.Error);
  });

  it("transport rejection → error terminal", async () => {
    start();
    await untilState(service, DownloadState.Downloading);
    gate[0].reject(new Error("network down"));
    await untilState(service, DownloadState.Error);
  });
});
