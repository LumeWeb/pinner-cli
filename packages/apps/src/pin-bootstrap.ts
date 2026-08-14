// DOM bootstrap for the "Create a Pin" flow: adapt the robot3 pin machine onto
// the elements of the Go-rendered Create Pin HTML shell, and reproduce the
// renderResult readout of pin_app.js.tmpl.
//
// Element contract (matches pins_app_templ.go):
//   #pin-form   the <form> that submits the pin.
//   #cid        the CID <input>.
//   #name       the optional Name <input>.
//   #pin-status the status element (class "status <state>").
//   #out-cid    the result CID <code>.
//   #out-status the result Status <code>.

import { interpret } from "robot3";
import { createPinMachine, type PinConfig, type PinContext } from "./pin";
import type { CallTool } from "./flow";

export interface PinElements {
  form: { addEventListener(type: "submit", listener: (ev: SubmitEvent) => void): void };
  cidInput: { value: string };
  nameInput: { value: string };
  statusEl: HTMLElement;
  outCid: HTMLElement;
  outStatus: HTMLElement;
}

export interface PinRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: "pending" | "ok" | "info" | "error" | null;
  statusMsg: string | null;
  /** Whether to refresh the #out-cid readout from ctx.outCid. */
  setOutCid: boolean;
  /** Whether to refresh the #out-status readout from ctx.outStatus. */
  setOutStatus: boolean;
}

/**
 * Map a machine state + context onto the status/result readout, reproducing
 * pin_app.js.tmpl's renderResult + pollStatus DOM writes.
 */
export function renderPin(state: string, ctx: PinContext, cfg: PinConfig): PinRender {
  switch (state) {
    case "form_error":
      return { statusState: "error", statusMsg: cfg.cidRequiredMsg, setOutCid: false, setOutStatus: false };
    case "submitting":
      return { statusState: "pending", statusMsg: cfg.pinningPrefix + ctx.cid + " ...", setOutCid: false, setOutStatus: false };
    case "polling":
      // First entry right after pins_add ok: the pins_add result readout.
      if (ctx.fresh) return { statusState: "ok", statusMsg: cfg.scheduledMsg, setOutCid: true, setOutStatus: true };
      // A transport-error retry shows "Checking pin status...".
      if (ctx.pollError) return { statusState: "pending", statusMsg: cfg.checkingMsg, setOutCid: false, setOutStatus: false };
      // Normal non-terminal poll: refresh #out-status only, no status message.
      return { statusState: null, statusMsg: null, setOutCid: false, setOutStatus: true };
    case "ok":
      return { statusState: "ok", statusMsg: cfg.pinnedMsg, setOutCid: false, setOutStatus: true };
    case "info":
      return { statusState: "info", statusMsg: cfg.currentStatusPrefix + ctx.status, setOutCid: false, setOutStatus: true };
    case "timeout":
      return {
        statusState: "info",
        statusMsg: ctx.pollError
          ? cfg.timeoutMsg
          : cfg.timeoutLastPrefix + (ctx.status || "unknown") + cfg.timeoutLastSuffix,
        setOutCid: false,
        setOutStatus: false,
      };
    case "error":
      return { statusState: "error", statusMsg: cfg.failedMsg, setOutCid: true, setOutStatus: true };
    default:
      return { statusState: null, statusMsg: null, setOutCid: false, setOutStatus: false };
  }
}

export interface PinEntryOptions {
  config: PinConfig;
  callTool: CallTool;
  elements: PinElements;
}

/**
 * Wire a pin machine to the given elements. Returns an object with `submit`
 * (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runPinEntry(opts: PinEntryOptions) {
  const machine = createPinMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s: any) => {
    const state: string = s.machine?.current ?? "";
    const ctx = s.context as PinContext;
    const r = renderPin(state, ctx, opts.config);
    if (r.statusState && r.statusMsg) {
      opts.elements.statusEl.className = "status " + r.statusState;
      opts.elements.statusEl.textContent = r.statusMsg;
    }
    if (r.setOutCid && ctx.outCid) opts.elements.outCid.textContent = ctx.outCid;
    if (r.setOutStatus && ctx.outStatus) opts.elements.outStatus.textContent = ctx.outStatus;
  });

  const TERMINAL = ["ok", "info", "error", "timeout"];
  opts.elements.form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const cid = opts.elements.cidInput.value.trim();
    const name = opts.elements.nameInput.value.trim();
    const st: string = (service.machine as any)?.current ?? "";
    // From a terminal state, reset first so a follow-up submission starts a
    // fresh flow (matches the template's re-submittable form after finish).
    if (TERMINAL.includes(st)) service.send({ type: "reset" });
    service.send({ type: "submit", cid, name });
  });

  return {
    /** Programmatic submit with explicit cid/name (used by tests/demo). */
    submit: (cid: string, name = "") => {
      const st: string = (service.machine as any)?.current ?? "";
      if (TERMINAL.includes(st)) service.send({ type: "reset" });
      service.send({ type: "submit", cid, name });
    },
    get state(): string {
      return (service.machine as any)?.current ?? "";
    },
    service,
  };
}
