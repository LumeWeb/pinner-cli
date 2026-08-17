// DOM bootstrap shared by the "Download to File" MCP Apps (IPFS and vault):
// adapt the download machine onto the elements of the Go-rendered download
// shells. A download is a single callServerTool submit: the form gathers the
// source (CID/path or vault path), a name, an optional host output path, and a
// sink; the machine calls the sink-aware tool; then the bootstrap renders the
// fetch_url as a download link (sink=drop) or the written output_path
// (sink=local).
//
// Element contract:
//   #<form>            the <form> that submits the download.
//   #<source>          the source identifier <input> (CID or vault path).
//   #<name>            the optional filename <input>.
//   #<output>          the optional host-side output_path <input> (sink=local).
//   #<sink-local>      the sink radio "local".
//   #<sink-drop>       the sink radio "drop".
//   #<status>          the status element (class "status <state>").
//   #<out-link>        the filedrop <a download> link readout (sink=drop).
//   #<out-path>        the local written-path <code> readout (sink=local).

import { interpret } from "robot3";
import {
  createDownloadMachine,
  currentDownloadState,
  isDownloadTerminal,
  type DownloadConfig,
  type DownloadContext,
  DownloadState,
} from "@/download";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Element ids referenced by the Go-rendered download HTML shell. */
export type DownloadElementIds = {
  form: string;
  source: string;
  name: string;
  output: string;
  sinkLocal: string;
  sinkDrop: string;
  status: string;
  outLink: string;
  outPath: string;
  start: string;
};

/** Data a download app entry contributes, handed to mountDownloadApp verbatim. */
export type DownloadAppEntry = AppDefinition<DownloadConfig, DownloadElementIds>;

export interface DownloadElements {
  form: { addEventListener(type: "submit", listener: (ev: SubmitEvent) => void): void };
  sourceInput: { value: string };
  nameInput: { value: string };
  outputInput: { value: string };
  sinkLocal: { checked: boolean };
  sinkDrop: { checked: boolean };
  statusEl: HTMLElement;
  outLink: HTMLElement & { href?: string; textContent?: string; style?: { display: string } };
  outPath: HTMLElement;
  startBtn: HTMLElement & { disabled?: boolean };
}

export interface DownloadRender {
  statusState: StatusClass | null;
  statusMsg: string | null;
  setOutLink: boolean;
  setOutPath: boolean;
  pending: boolean;
}

/** Map a machine state + context onto the status/result readout. */
export function renderDownload(
  state: DownloadState,
  ctx: DownloadContext,
  cfg: DownloadConfig,
): DownloadRender {
  switch (state) {
    case DownloadState.Downloading:
      return { statusState: StatusClass.Pending, statusMsg: cfg.downloadingMsg, setOutLink: false, setOutPath: false, pending: true };
    case DownloadState.Ok:
      return { statusState: StatusClass.Ok, statusMsg: cfg.downloadedMsg, setOutLink: !!ctx.fetchUrl, setOutPath: !!ctx.outputPathResult, pending: false };
    case DownloadState.Error:
      return { statusState: StatusClass.Error, statusMsg: cfg.failedMsg, setOutLink: false, setOutPath: false, pending: false };
    default:
      return { statusState: null, statusMsg: null, setOutLink: false, setOutPath: false, pending: false };
  }
}

export interface DownloadEntryOptions {
  config: DownloadConfig;
  callTool: CallTool;
  elements: DownloadElements;
}

/**
 * Wire a download machine to the given elements. Returns an object with
 * `submit` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runDownloadEntry(opts: DownloadEntryOptions) {
  const machine = createDownloadMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s) => {
    const state = currentDownloadState(s);
    const ctx = s.context as DownloadContext;
    const r = renderDownload(state, ctx, opts.config);
    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    if (opts.elements.startBtn) opts.elements.startBtn.disabled = r.pending;
    if (r.setOutLink && ctx.fetchUrl) {
      opts.elements.outLink.href = ctx.fetchUrl;
      opts.elements.outLink.textContent = "Save file";
      opts.elements.outLink.style.display = "";
    }
    if (r.setOutPath && ctx.outputPathResult) {
      opts.elements.outPath.textContent = ctx.outputPathResult;
    }
  });

  const submit = (source: string, name: string, outputPath: string, sink: string) => {
    if (!source) {
      setStatus(opts.elements.statusEl, StatusClass.Error, opts.config.noSourceMsg);
      return;
    }
    const st = currentDownloadState(service);
    if (isDownloadTerminal(st)) service.send({ type: "reset" });
    service.send({ type: "start", source, name, outputPath, sink });
  };

  opts.elements.form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const sink = opts.elements.sinkDrop.checked ? "drop" : "local";
    submit(
      opts.elements.sourceInput.value.trim(),
      opts.elements.nameInput.value.trim(),
      opts.elements.outputInput.value.trim(),
      sink,
    );
  });

  return {
    /** Programmatic submit with explicit values (used by tests/demo). */
    submit: (source: string, sink = "local", name = "", outputPath = "") =>
      void submit(source, name, outputPath, sink),
    get state(): DownloadState {
      return currentDownloadState(service);
    },
    service,
  };
}

/**
 * Mount a download app entrypoint: wire the download machine to the Go-rendered
 * elements, and either run synchronously with a caller-supplied `callTool`
 * (tests/demo) or connect to the host over postMessage via bootApp. `def.source`
 * carries the tool + source-field config; `sourceArg` is derived from the type.
 */
export function mountDownloadApp<C extends DownloadConfig>(
  def: DownloadAppEntry,
  root: Document,
  callTool?: CallTool,
) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runDownloadEntry({
      config: def.config,
      callTool: ct,
      elements: {
        form: byId<HTMLFormElement>(root, def.ids.form)!,
        sourceInput: byId<HTMLInputElement>(root, def.ids.source)!,
        nameInput: byId<HTMLInputElement>(root, def.ids.name)!,
        outputInput: byId<HTMLInputElement>(root, def.ids.output)!,
        sinkLocal: byId<HTMLInputElement>(root, def.ids.sinkLocal)!,
        sinkDrop: byId<HTMLInputElement>(root, def.ids.sinkDrop)!,
        statusEl: statusEl!,
        outLink: byId<HTMLLinkElement & { style: { display: string } }>(root, def.ids.outLink)!,
        outPath: byId<HTMLElement>(root, def.ids.outPath)!,
        startBtn: byId<HTMLElement & { disabled?: boolean }>(root, def.ids.start)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}

export type { MachineCurrent };
