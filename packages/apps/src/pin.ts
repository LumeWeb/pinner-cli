// Pure pin-flow state machine for the "Create a Pin" MCP App, authored with
// robot3. It drives the submit -> pins_add -> renderResult -> poll pin_status
// lifecycle as a robot3 machine (mirroring the OOB flow machine in flow.ts).
//
// The machine is deliberately free of DOM and MCP-transport concerns so it can
// be unit-tested in node with a stubbed callTool. The DOM bootstrap
// (pin-bootstrap.ts) adapts these states onto real elements.
//
// States:
//   form       — the Create Pin form is shown, awaiting a submit.
//   form_error — a submit arrived with an empty CID; "A CID is required."
//                (form stays submittable). Non-terminal.
//   submitting — pins_add invoked with { cids: [cid], name }.
//   polling    — pin_status polled by cid until terminal. Self-loop carries the
//                decremented attempt budget toward `timeout`.
//   ok         — terminal success (status === "pinned").
//   info       — terminal: status is failed or error ("Current status: X").
//   timeout    — terminal: attempt budget exhausted (missing/other status) or
//                repeated transport failures.
//   error      — terminal: pins_add returned isError (or the call rejected).
//
// Poll semantics:
//   - A terminal status (pinned/failed/error) wins even on the final allowed
//     attempt — it does NOT decrement the budget (guards ordered first).
//   - A *missing* status (an IsError result, e.g. ErrPinNotFound right after a
//     pin is scheduled) is NON-terminal: keep polling until the budget is
//     exhausted.
//   - A rejected pin_status call retries until the budget is exhausted, then
//     falls through to `timeout` (distinguished from the missing-status timeout
//     by the pollError flag so the bootstrap can pick the right message copy).

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

export interface PinConfig {
  /** Name of the MCP tool that adds the pin (pins_add). */
  addTool: string;
  /** Name of the MCP tool that reports pin status by cid (pin_status). */
  statusTool: string;
  /** Max status polls before timeout (default 24). */
  maxAttempts: number;
  /** Delay between polls (ms). */
  pollDelayMs: number;

  // Message copy.
  cidRequiredMsg: string;
  pinningPrefix: string; // "Pinning " (cid + " ..." appended)
  scheduledMsg: string; // "Pin scheduled."
  failedMsg: string; // "Pin failed."
  pinnedMsg: string; // "Pinned."
  currentStatusPrefix: string; // "Current status: "
  timeoutLastPrefix: string; // "Timed out polling pin status (last: "
  timeoutLastSuffix: string; // ")."
  checkingMsg: string; // "Checking pin status..."
  timeoutMsg: string; // "Timed out polling pin status."
}

export interface PinContext {
  cid: string;
  name: string;
  /** Rendered CID readout (from value.CID, falling back to the submitted cid). */
  outCid: string;
  /** Rendered status readout (from value.Status / last poll status). */
  outStatus: string;
  /** Last status observed from pin_status. */
  status: string;
  /** Remaining poll budget. */
  attempts: number;
  /** True when the last poll rejected (transport error) — drives retry copy. */
  pollError: boolean;
  /** True only on the first entry into polling (right after pins_add ok). */
  fresh: boolean;
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/**
 * Build the pin-flow machine. `callTool` is the injected MCP tool caller; the
 * machine stays testable by passing a stub here.
 */
export function createPinMachine(cfg: PinConfig, callTool: CallTool) {
  // --- event accessors ---------------------------------------------------
  // robot3 invoke resolves as { type: "done", data: <resolved value> }; the
  // resolved value (ToolResult or the pollOnce outcome) lives under ev.data.
  const data = (ev: any) => (ev && typeof ev === "object" ? ev.data : undefined);
  const dStatus = (ev: any): string | undefined => data(ev)?.status;
  const dRemaining = (ev: any): number => data(ev)?.remaining ?? 0;
  const dPollError = (ev: any): boolean => !!data(ev)?.pollError;
  const dIsError = (ev: any): boolean => !!data(ev)?.isError;
  const dSc = (ev: any): any => data(ev)?.structuredContent;

  // --- guards -------------------------------------------------------------
  const hasCid = (_ctx: PinContext, ev: any) => !!ev && typeof ev.cid === "string" && ev.cid.length > 0;
  const withoutCid = (_ctx: PinContext, ev: any) => !hasCid(_ctx, ev);
  const isPinned = (_ctx: PinContext, ev: any) => dStatus(ev) === "pinned";
  const isTerminalOther = (_ctx: PinContext, ev: any) =>
    dStatus(ev) === "failed" || dStatus(ev) === "error";
  const pinExhausted = (_ctx: PinContext, ev: any) => dRemaining(ev) <= 0;
  const pinsAddOk = (_ctx: PinContext, ev: any) => !dIsError(ev);
  const pinsAddErr = (_ctx: PinContext, ev: any) => dIsError(ev);

  // --- reducers -----------------------------------------------------------
  const setCidName = reduce((ctx: PinContext, ev: any) => ({
    ...ctx,
    cid: ev.cid ?? "",
    name: ev.name ?? "",
  }));

  // renderResult: out-cid from value.CID (fallback submitted cid), out-status
  // from value.Status. This runs on the pins_add `done` transition.
  const setAddResult = reduce((ctx: PinContext, ev: any) => {
    const sc = dSc(ev);
    const value = sc && typeof sc === "object" ? sc.value : undefined;
    const rawCid = value && value.CID;
    const rawStatus = value && value.Status;
    return {
      ...ctx,
      outCid: rawCid != null ? String(rawCid) : ctx.cid,
      outStatus: rawStatus != null ? String(rawStatus) : ctx.outStatus,
      status: "",
    };
  });

  // Starting to poll right after a successful pins_add: surface the scheduled
  // status readout on first polling entry.
  const armPoll = reduce((ctx: PinContext) => ({
    ...ctx,
    attempts: cfg.maxAttempts,
    fresh: true,
    pollError: false,
  }));

  // Apply a single poll outcome (status readout + remaining budget + flags).
  // `status` reflects ONLY the current poll's status (cleared when missing), so
  // the timeout message correctly reads `(last: <current-or-unknown>)`.
  const setPollOutcome = reduce((ctx: PinContext, ev: any) => ({
    ...ctx,
    status: dStatus(ev) ?? "",
    outStatus: dStatus(ev) ?? ctx.outStatus,
    attempts: dRemaining(ev),
    pollError: dPollError(ev),
    fresh: false,
  }));

  // --- invoke fns ---------------------------------------------------------
  const submitPin = (ctx: PinContext): Promise<ToolResult> =>
    callTool({ name: cfg.addTool, arguments: { cids: [ctx.cid], name: ctx.name } });

  // One pin_status poll. Returns an outcome understood by the `done`
  // transitions. A terminal status does NOT decrement the budget (it wins on
  // the final allowed attempt); a missing status and a transport error DO
  // decrement so the loop can exhaust toward `timeout`.
  const pollOnce = async (ctx: PinContext) => {
    let res: ToolResult;
    try {
      res = await callTool({ name: cfg.statusTool, arguments: { cid: ctx.cid } });
    } catch (e) {
      return { remaining: ctx.attempts - 1, pollError: true };
    }
    await sleep(cfg.pollDelayMs);
    const sc: any = res && res.structuredContent;
    const st = sc && typeof sc === "object" ? sc.status : undefined;
    const status = typeof st === "string" ? st : undefined;
    const terminal = status === "pinned" || status === "failed" || status === "error";
    return {
      status,
      outStatus: status ?? ctx.outStatus,
      remaining: terminal ? ctx.attempts : ctx.attempts - 1,
      pollError: false,
    };
  };

  // --- machine ------------------------------------------------------------
  return createMachine(
    {
      form: state(
        transition("submit", "submitting", guard(hasCid), setCidName),
        transition("submit", "form_error", guard(withoutCid)),
      ),
      form_error: state(
        transition("submit", "submitting", guard(hasCid), setCidName),
        transition("submit", "form_error", guard(withoutCid)),
      ),

      submitting: invoke(
        submitPin,
        transition("done", "polling", guard(pinsAddOk), setAddResult, armPoll),
        transition("done", "error", guard(pinsAddErr), setAddResult),
        transition("error", "error"),
      ),

      polling: invoke(
        pollOnce,
        // Terminal statuses are checked BEFORE the budget so they win on the
        // final allowed attempt (no decrement in pollOnce when terminal).
        transition("done", "ok", guard(isPinned), setPollOutcome),
        transition("done", "info", guard(isTerminalOther), setPollOutcome),
        transition("done", "timeout", guard(pinExhausted), setPollOutcome),
        transition("done", "polling", setPollOutcome),
        transition("error", "error"),
      ),

      ok: state(transition("reset", "form")),
      info: state(transition("reset", "form")),
      error: state(transition("reset", "form")),
      timeout: state(transition("reset", "form")),
    },
    () => ({
      cid: "",
      name: "",
      outCid: "",
      outStatus: "",
      status: "",
      attempts: cfg.maxAttempts,
      pollError: false,
      fresh: false,
    }),
  );
}

// Pin machine states as a string enum. Values are plain strings (robot3 keys),
// so enum members compare directly against service.machine.current.
export enum PinState {
  Form = "form",
  FormError = "form_error",
  Submitting = "submitting",
  Polling = "polling",
  Ok = "ok",
  Info = "info",
  Error = "error",
  Timeout = "timeout",
}

/** All pin states, for iteration / membership checks. */
export const PIN_STATES: readonly PinState[] = [
  PinState.Form,
  PinState.FormError,
  PinState.Submitting,
  PinState.Polling,
  PinState.Ok,
  PinState.Info,
  PinState.Error,
  PinState.Timeout,
];
