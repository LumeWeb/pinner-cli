// Shared app entrypoint helper: build a FlowConfig-driven machine and wire it
// to elements. Each per-app entry (pin, vault-create, vault-restore, auth-sso)
// supplies its own config/ids; the heavy flow logic lives in flow.ts and is
// tested there, so these entries stay thin.

import { createMachine, interpret } from "robot3";
import { createFlowMachine, type CallTool, type FlowConfig, type FlowState } from "@/flow";
import { bootApp } from "@/boot";
import { APP_VERSION } from "@/version";
import { byId, setStatus } from "@/dom";

/** Minimal host bridge; in the real iframe this is the ext-apps App. */
export interface AppBridge {
  callServerTool: CallTool;
}

/** Terminal flow states (no more polling or user action needed until retry). */
export const FLOW_TERMINAL: readonly FlowState[] = ["ok", "dead", "error", "timeout"];

/** Read the current state of a robot3 service as the typed FlowState union. */
export function currentFlowState(service: { machine?: { current?: string } }): FlowState {
  return ((service.machine?.current ?? "") as FlowState);
}

/**
 * Generic app definition: any MCP App is a `name` plus a machine `config` plus
 * its element-id map. Flow apps and the Create Pin app differ only in the
 * concrete `Config` and `Ids` types, so a single parameterized shape covers
 * both and factors out the per-app boilerplate.
 */
export interface AppDefinition<Config, Ids extends Record<string, string>> {
  name: string;
  config: Config;
  /** Element ids referenced by the Go-rendered HTML shell. */
  ids: Ids;
}

export interface AppEntryOptions {
  config: FlowConfig;
  callTool: CallTool;
  elements: {
    startBtn: HTMLElement & { disabled?: boolean };
    urlEl: HTMLElement;
    statusEl: HTMLElement;
  };
}

/**
 * Wire a flow machine to the given elements. Returns an object with `start`
 * (programmatic trigger for tests/demo) and `state` getter.
 */
export function runAppEntry(opts: AppEntryOptions) {
  const machine = createFlowMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s: any) => {
    const stateName: FlowState = currentFlowState(s);
    const ctx = s.context as { url: string; detail: string; alreadyDone: boolean };
    const btn = opts.elements.startBtn;
    const urlEl = opts.elements.urlEl;
    const statusEl = opts.elements.statusEl;

    const pending = stateName === "starting" || stateName === "polling";
    const terminal = FLOW_TERMINAL.includes(stateName);
    // During an in-flight run the button is disabled (no concurrent runs). On a
    // terminal state it is re-enabled so the user can click "retry" / start
    // again.
    if (btn) btn.disabled = pending;

    switch (stateName) {
      case "ok":
        setStatus(statusEl, "ok", ctx.alreadyDone ? opts.config.alreadyDoneMsg : opts.config.doneMsg);
        break;
      case "dead":
        setStatus(statusEl, "error", ctx.detail || opts.config.deadDetailPrefix);
        break;
      case "error":
        setStatus(statusEl, "error", opts.config.startErrorMsg);
        break;
      case "timeout":
        setStatus(statusEl, "info", opts.config.timeoutMsg);
        break;
      case "starting":
      case "polling":
        setStatus(statusEl, "pending", opts.config.pendingMsg);
        break;
      default:
        break;
    }

    if (ctx.url) {
      urlEl.textContent = ctx.url;
      urlEl.setAttribute("href", ctx.url);
    }
  });

  if (opts.elements.startBtn) {
    opts.elements.startBtn.addEventListener("click", () => {
      const st = currentFlowState(service);
      // From a terminal state the machine's `retry` transition returns to idle;
      // a subsequent click (now that the button is re-enabled) sends `start`.
      if (FLOW_TERMINAL.includes(st)) {
        service.send("retry");
      } else {
        service.send("start");
      }
    });
  }

  return {
    start: () => service.send("start"),
    get state(): FlowState {
      return currentFlowState(service);
    },
    service,
  };
}

/** Element ids every flow app's Go-rendered shell references. */
export type FlowElementIds = {
  startBtn: string;
  urlEl: string;
  statusEl: string;
};

/** Messages + defaults a flow app entry contributes on top of FlowConfig core. */
export type FlowCopy = Pick<
  FlowConfig,
  | "actionLabel"
  | "startErrorMsg"
  | "alreadyDoneMsg"
  | "noHandlePrefix"
  | "pendingMsg"
  | "doneMsg"
  | "deadDetailPrefix"
  | "timeoutMsg"
  | "retryWord"
>;

/** Core, non-copy FlowConfig fields a flow app entry supplies. */
export type FlowConfigCore = Omit<
  FlowConfig,
  keyof FlowCopy | "maxAttempts" | "pollDelayMs"
>;

/**
 * Mount a flow app entrypoint: build its FlowConfig (core config + copy +
 * defaults), wire the flow machine to the Go-rendered elements, and either run
 * synchronously with a caller-supplied `callTool` (tests/demo) or connect to
 * the host over postMessage via bootApp, advertising the CLI build version.
 */
export function mountFlowApp<Ids extends FlowElementIds>(
  def: AppDefinition<FlowConfigCore & Partial<FlowConfig>, Ids>,
  copy: FlowCopy,
  root: Document,
  callTool?: CallTool,
) {
  const config: FlowConfig = {
    ...def.config,
    ...copy,
    maxAttempts: def.config.maxAttempts ?? 60,
    pollDelayMs: def.config.pollDelayMs ?? 1500,
  } as FlowConfig;
  const statusEl = byId<HTMLElement>(root, def.ids.statusEl);
  const wire = (ct: CallTool) =>
    runAppEntry({
      config,
      callTool: ct,
      elements: {
        startBtn: byId<HTMLElement & { disabled?: boolean }>(root, def.ids.startBtn)!,
        urlEl: byId<HTMLElement>(root, def.ids.urlEl)!,
        statusEl: statusEl!,
      },
    });
  if (callTool) return wire(callTool);
  bootApp({ name: def.name, version: APP_VERSION }, wire, statusEl);
}
