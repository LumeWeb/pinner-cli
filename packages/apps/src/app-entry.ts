// Shared app entrypoint helper: build a FlowConfig-driven machine and wire it
// to elements. Each per-app entry (pin, vault-create, vault-restore, auth-sso)
// supplies its own config/ids; the heavy flow logic lives in flow.ts and is
// tested there, so these entries stay thin.

import { createMachine, interpret } from "robot3";
import {
  createLinkMachine,
  currentLinkState,
  LinkState,
  type LinkConfig,
  renderLink,
} from "@/link";
import { createFlowMachine, type CallTool, type FlowConfig, FlowState, isFlowTerminal } from "@/flow";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";
import type { FlowAppEntry } from "@/entries/common";

/** Minimal host bridge; in the real iframe this is the ext-apps App. */
export interface AppBridge {
  callServerTool: CallTool;
}

/**
 * Read the current state of a robot3 service as the typed FlowState union.
 * The service exposes machine.current (a plain string); coerce it onto the
 * enum at this single boundary so consumers never cast or compare literals.
 */
export interface MachineCurrent {
  machine?: { current?: string };
}
export function currentFlowState(service: MachineCurrent): FlowState {
  return (service.machine?.current ?? FlowState.Idle) as FlowState;
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

// Flow-render model: map a flow state + context onto the status/button readout,
// mirroring the pin consumer's renderPin. Produces the status class/message and
// the pending flag; a null statusState means "leave the status element alone"
// (idle). The url readout is refreshed independently in runAppEntry.
export interface FlowRender {
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Whether the flow is mid-flight (starting/polling) — disables the button. */
  pending: boolean;
}

/** Map a flow state + context onto the status/button readout. */
export function renderFlow(
  state: FlowState,
  ctx: { detail?: string; alreadyDone?: boolean },
  cfg: FlowConfig,
): FlowRender {
  switch (state) {
    case FlowState.Ok:
      return {
        statusState: StatusClass.Ok,
        statusMsg: ctx.alreadyDone ? cfg.alreadyDoneMsg : cfg.doneMsg,
        pending: false,
      };
    case FlowState.Dead:
      return {
        statusState: StatusClass.Error,
        statusMsg: ctx.detail || cfg.deadDetailPrefix,
        pending: false,
      };
    case FlowState.Error:
      return { statusState: StatusClass.Error, statusMsg: cfg.startErrorMsg, pending: false };
    case FlowState.Timeout:
      return { statusState: StatusClass.Info, statusMsg: cfg.timeoutMsg, pending: false };
    case FlowState.Starting:
    case FlowState.Polling:
      return { statusState: StatusClass.Pending, statusMsg: cfg.pendingMsg, pending: true };
    default:
      return { statusState: null, statusMsg: null, pending: false };
  }
}

/**
 * Wire a flow machine to the given elements. Returns an object with `start`
 * (programmatic trigger for tests/demo) and `state` getter.
 */
export function runAppEntry(opts: AppEntryOptions) {
  const machine = createFlowMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s) => {
    const stateName = currentFlowState(s);
    const ctx = s.context;
    const btn = opts.elements.startBtn;
    const urlEl = opts.elements.urlEl;
    const statusEl = opts.elements.statusEl;

    const r = renderFlow(stateName, ctx, opts.config);
    // During an in-flight run the button is disabled (no concurrent runs). On a
    // terminal state it is re-enabled so the user can click "retry" / start
    // again.
    if (btn) btn.disabled = r.pending;
    // Stamp the status readout whenever a real state resolves (incl. empty
    // messages — e.g. an empty deadDetailPrefix/doneMsg still clears the text
    // and sets the class), matching the pre-refactor unconditional write. Idle
    // (statusState null) leaves the element untouched.
    if (r.statusState) setStatus(statusEl, r.statusState, r.statusMsg ?? "");

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
      if (isFlowTerminal(st)) {
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
export function mountFlowApp(def: FlowAppEntry, root: Document, callTool?: CallTool) {
  const config: FlowConfig = {
    ...def.config,
    ...def.copy,
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
  return mountApp({ name: def.name, statusEl, wire, callTool });
}

/** Element ids every link app's Go-rendered shell references. */
export type LinkElementIds = {
  startBtn: string;
  urlEl: string;
  statusEl: string;
};

/** Messages a link app contributes on top of LinkConfig core. */
export type LinkCopy = Pick<
  LinkConfig,
  "startLabel" | "openLabel" | "startErrorMsg" | "noUrlMsg" | "alreadyDoneMsg" | "doneMsg"
>;

export type LinkConfigCore = Omit<LinkConfig, keyof LinkCopy>;

/** Data a link app entry contributes, handed to mountLinkApp verbatim. */
export interface LinkAppEntry {
  name: string;
  config: LinkConfigCore;
  ids: LinkElementIds;
  copy: LinkCopy;
}

/**
 * Mount a one-shot deep-link app: call the start tool once, render the returned
 * action_url as a clickable link, and mark done when the page is opened. Used
 * for synchronous out-of-band changes (password / email) with no poll loop.
 */
export function mountLinkApp(def: LinkAppEntry, root: Document, callTool?: CallTool) {
  const config: LinkConfig = { ...def.config, ...def.copy } as LinkConfig;
  const statusEl = byId<HTMLElement>(root, def.ids.statusEl);
  const startBtn = byId<HTMLElement & { disabled?: boolean }>(root, def.ids.startBtn)!;
  const urlEl = byId<HTMLElement>(root, def.ids.urlEl)!;

  const wire = (ct: CallTool) => {
    const machine = createLinkMachine(config, ct);
    const service = interpret(machine, (s) => {
      const st = currentLinkState(s);
      const ctx = s.context;
      const r = renderLink(st, ctx, config);
      if (startBtn) startBtn.disabled = r.pending;
      if (r.status) setStatus(statusEl, r.status as StatusClass, r.message ?? "");
      if (ctx.url) {
        urlEl.textContent = config.openLabel;
        urlEl.setAttribute("href", ctx.url);
        urlEl.classList.add("link-ready");
      }
    });
    if (startBtn) {
      startBtn.addEventListener("click", () => {
        const st = currentLinkState(service);
        if (st === LinkState.Ok || st === LinkState.Norl || st === LinkState.Error) {
          service.send("retry");
        } else {
          service.send("start");
        }
      });
    }
    return { service };
  };

  return mountApp({ name: def.name, statusEl, wire, callTool });
}
