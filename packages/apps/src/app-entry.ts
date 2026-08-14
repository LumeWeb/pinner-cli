// Shared app entrypoint helper: build a FlowConfig-driven machine and wire it
// to elements. Each per-app entry (pin, vault-create, vault-restore, auth-sso)
// supplies its own config/ids; the heavy flow logic lives in flow.ts and is
// tested there, so these entries stay thin.

import { createMachine, interpret } from "robot3";
import { createFlowMachine, type CallTool, type FlowConfig } from "./flow";

/** Minimal host bridge; in the real iframe this is the ext-apps App. */
export interface AppBridge {
  callServerTool: CallTool;
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
    const state: string = s.machine?.current ?? "";
    const ctx = s.context as { url: string; detail: string };
    const btn = opts.elements.startBtn;
    const urlEl = opts.elements.urlEl;
    const statusEl = opts.elements.statusEl;

    const stateName = state as string;
    const pending = stateName === "starting" || stateName === "polling";
    const terminal = ["ok", "dead", "error", "timeout"].includes(stateName);
    // During an in-flight run the button is disabled (no concurrent runs,
    // matching the template's `if (startBtn.disabled) return;` guard). On a
    // terminal state it is re-enabled (like finishFlow) so the user can click
    // "retry" / start again.
    if (btn) btn.disabled = pending;

    if (stateName === "ok") statusEl.className = "status ok", (statusEl.textContent = opts.config.doneMsg);
    else if (stateName === "dead") statusEl.className = "status error", (statusEl.textContent = (ctx.detail || opts.config.deadDetailPrefix));
    else if (stateName === "error") statusEl.className = "status error", (statusEl.textContent = opts.config.startErrorMsg);
    else if (stateName === "timeout") statusEl.className = "status info", (statusEl.textContent = opts.config.timeoutMsg);
    else if (pending) statusEl.className = "status pending", (statusEl.textContent = opts.config.pendingMsg);

    if (ctx.url) {
      urlEl.textContent = ctx.url;
      urlEl.setAttribute("href", ctx.url);
    }
  });

  if (opts.elements.startBtn) {
    opts.elements.startBtn.addEventListener("click", () => {
      const st: string = (service.machine as any)?.current ?? "";
      // From a terminal state the machine's `retry` transition returns to idle;
      // a subsequent click (now that the button is re-enabled) sends `start`.
      if (["ok", "dead", "error", "timeout"].includes(st)) {
        service.send("retry");
      } else {
        service.send("start");
      }
    });
  }

  return {
    start: () => service.send("start"),
    get state(): string {
      return (service.machine as any)?.current ?? "";
    },
    service,
  };
}
