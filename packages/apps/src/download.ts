// Pure download orchestration state machine for the "Download to File" MCP
// Apps (IPFS and vault), authored with robot3. A download is the reverse of an
// upload but far simpler: one callServerTool submit against the sink-aware
// download_file / vault_get_file tool, then render the result. The bytes never
// enter this machine or the LLM channel — sink=local returns a host-side
// output_path (written on the MCP server's own disk), and sink=drop returns a
// one-time HTTP GET filedrop link the human pulls out of band (curl -o or a
// browser <a download>).
//
// States: idle -> downloading -> ok | error.
//   downloading: call the download tool with the source plus the chosen sink,
//                then surface the fetch_url (drop) or output_path (local).

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

export interface DownloadConfig {
  /** the sink-aware download tool to call (download_file or vault_get_file). */
  downloadTool: string;
  /** field carrying the source identifier: "ipfs_path" or "vault_path". */
  sourceArg: string;
  downloadingMsg: string;
  downloadedMsg: string;
  failedMsg: string;
  noSourceMsg: string;
  /** whether the filedrop (drop) sink is advertised; if false the form only
   *  offers local. */
  dropAvailable: boolean;
}

export interface DownloadContext {
  source: string;
  name: string;
  outputPath: string;
  sink: string;
  /** result readouts surfaced to the DOM. */
  fetchUrl: string;
  outputPathResult: string;
}

/** Normalize the tool's canonical envelope into simple readouts. */
function extractResult(sc: any): { fetchUrl: string; outputPathResult: string } {
  if (!sc || typeof sc !== "object") return { fetchUrl: "", outputPathResult: "" };
  const s = sc as Record<string, unknown>;
  return {
    fetchUrl: typeof s.fetch_url === "string" ? s.fetch_url : "",
    outputPathResult:
      typeof s.output_path === "string"
        ? s.output_path
        : typeof s.output_path === "number"
          ? String(s.output_path)
          : "",
  };
}

/** Build the download machine. `callTool` is the injected MCP tool caller. */
export function createDownloadMachine(cfg: DownloadConfig, callTool: CallTool) {
  const dSc = (ev: any): any => ev?.data?.structuredContent;
  const dIsError = (ev: any): boolean => !!ev?.data?.isError;

  const setMeta = reduce((ctx: DownloadContext, ev: any) => ({
    ...ctx,
    source: ev?.source ?? "",
    name: ev?.name ?? "",
    outputPath: ev?.outputPath ?? "",
    sink: ev?.sink ?? "local",
  }));

  const setResult = reduce((ctx: DownloadContext, ev: any) => ({
    ...ctx,
    ...extractResult(dSc(ev)),
  }));

  // One-shot call to the sink-aware download tool.
  const doDownload = (ctx: DownloadContext): Promise<ToolResult> => {
    const args: Record<string, any> = { [cfg.sourceArg]: ctx.source, sink: ctx.sink };
    if (ctx.name) args.name = ctx.name;
    if (ctx.outputPath) args.output_path = ctx.outputPath;
    return callTool({ name: cfg.downloadTool, arguments: args });
  };

  const downloadOk = (_ctx: DownloadContext, ev: any) => !dIsError(ev);
  const downloadErr = (_ctx: DownloadContext, ev: any) => dIsError(ev);

  return createMachine(
    {
      idle: state(transition("start", "downloading", setMeta)),
      downloading: invoke(
        doDownload,
        transition("done", "ok", guard(downloadOk), setResult),
        transition("done", "error", guard(downloadErr)),
        transition("error", "error"),
      ),
      ok: state(transition("reset", "idle")),
      error: state(transition("reset", "idle")),
    },
    () => ({ source: "", name: "", outputPath: "", sink: "local", fetchUrl: "", outputPathResult: "" }),
  );
}

export enum DownloadState {
  Idle = "idle",
  Downloading = "downloading",
  Ok = "ok",
  Error = "error",
}

/** Read the current state of a robot3 service as the typed DownloadState. */
export function currentDownloadState(service: {
  machine?: { current?: string };
}): DownloadState {
  return (service.machine?.current ?? DownloadState.Idle) as DownloadState;
}

export const isDownloadTerminal = (s: DownloadState): boolean =>
  s === DownloadState.Ok || s === DownloadState.Error;
