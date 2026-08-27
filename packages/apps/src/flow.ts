// Pure flow state machine for the MCP Apps "start → poll → done" lifecycle,
// authored with robot3.
//
// This is the single source of truth for the flow that the Go-rendered HTML
// shells drive. It is deliberately free of DOM and MCP-transport concerns so it
// can be unit-tested in node with a stubbed callTool. The DOM bootstrap
// (bootstrap.ts) adapts these states onto real elements.
//
// States:
//   idle     — start button enabled; awaiting user "start".
//   starting — startTool invoked (out-of-band flow kicks off).
//   polling  — statusTool polled by handle until terminal.
//   ok       — terminal success.
//   dead     — terminal: hand-off/handle gone; surface restart detail.
//   error    — terminal: startTool failed or no usable hand-off.
//   timeout  — terminal: poll attempts exhausted.
//
// Robot3 `invoke` drives the async tool calls. A self-loop on `polling` via the
// `done` event re-invokes the next poll (with a configurable delay), carrying
// the decremented attempt counter toward `timeout`.

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";

export interface FlowConfig {
  /** Name of the MCP tool that starts the out-of-band flow. */
  startTool: string;
  /** Name of the MCP tool that reports flow status by handle. */
  statusTool: string;
  /** StructuredContent fields, in priority order, that may carry the human URL. */
  urlFields: string[];
  /** Max status polls before timeout. */
  maxAttempts: number;
  /** Delay between polls (ms). */
  pollDelayMs: number;

  // Optional revoke support: a single-flight flow that can already be in-use
  // (auth_sso) exposes how to cancel the in-progress handle and start fresh.
  /** Name of the MCP tool that revokes the in-progress flow by handle, if any. */
  revokeTool?: string;

  // Message copy.
  actionLabel: string;
  startErrorMsg: string;
  alreadyDoneMsg: string;
  noHandlePrefix: string;
  pendingMsg: string;
  doneMsg: string;
  deadDetailPrefix: string;
  timeoutMsg: string;
  retryWord: string;
  /** Shown while polling a login that was already in use (reused, not fresh).
   * Only flow apps that reuse in-use logins (auth_sso) supply it. */
  inUseMsg?: string;
}

/** Tool-result shape the flow reads from. */
export interface ToolResult {
  isError?: boolean;
  structuredContent?: Record<string, unknown>;
}

/**
 * Normalize an MCP tool result into an error message, or "" when the result is
 * not an error. A result is treated as an error iff it is flagged `isError`.
 * The message is read from the top-level `.error` (e.g. a local rejection
 * handler), then from `structuredContent.error` (the Go ErrorResult shape
 * `{status:"error", error:<code>}`), then a caller-supplied generic fallback —
 * so a failed call is never silently treated as a success (e.g. rendering an
 * empty listing).
 */
export function toolError(r: ToolResult | undefined, fallback: string): string {
  if (!r || !r.isError) return "";
  const top = (r as { error?: unknown }).error;
  const sc = r.structuredContent as Record<string, unknown> | undefined;
  const scErr = sc?.error;
  if (typeof top === "string" && top) return top;
  if (typeof scErr === "string" && scErr) return scErr;
  return fallback;
}

/**
 * Wrap a rejected callTool promise as an error ToolResult carrying the reason
 * as a top-level `.error` string, so `toolError` recovers it downstream.
 */
export function rejectToError(e: unknown): ToolResult {
  return { isError: true, error: String(e) } as ToolResult;
}

/** Async MCP tool caller. */
export type CallTool = (req: { name: string; arguments: Record<string, unknown> }) => Promise<ToolResult>;

export interface FlowContext {
  url: string;
  handle: string;
  detail: string;
  attempts: number;
  /** True when `ok` was reached because the start tool immediately reported the
   * flow already-complete (status "done" on start), so the UI shows
   * `alreadyDoneMsg` instead of `doneMsg`. */
  alreadyDone: boolean;
  /** True when the flow is polling a login that was ALREADY in use (reused by
   * single-flight start) rather than freshly started. Surfaces the in-use state
   * and reveals the revoke affordance. */
  inUse: boolean;
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export function createFlowMachine(cfg: FlowConfig, callTool: CallTool) {
  // --- structured-content accessors ------------------------------------

  const statusOf = (ev: any): string | undefined => {
    const sc = ev?.data?.structuredContent;
    return sc && typeof sc === "object" ? sc.status : undefined;
  };
  const strOf = (ev: any, key: string): string => {
    const sc = ev?.data?.structuredContent;
    return sc && typeof sc === "object" && typeof sc[key] === "string" ? sc[key] : "";
  };
  const remainingOf = (ev: any): number => ev?.data?.remaining ?? 0;

  // --- guards -------------------------------------------------------------

  const hasStatus = (status: string) => (ctx: FlowContext, ev: any) => statusOf(ev) === status;
  const hasHandle = (ctx: FlowContext, ev: any) => !!strOf(ev, "handle");
  const withoutHandle = (ctx: FlowContext, ev: any) => !strOf(ev, "handle");
  const exhausted = (ctx: FlowContext, ev: any) => remainingOf(ev) <= 0;
  const isInUse = (ctx: FlowContext, ev: any) => {
    const sc = ev?.data?.structuredContent;
    return !!(sc && typeof sc === "object" && sc.in_use === true);
  };

  // --- reducers -----------------------------------------------------------

  const setUrl = reduce((ctx: FlowContext, ev: any) => {
    const sc = ev?.data?.structuredContent;
    let url = "";
    if (sc && typeof sc === "object") {
      for (const f of cfg.urlFields) {
        if (typeof sc[f] === "string" && sc[f].length > 0) {
          url = sc[f];
          break;
        }
      }
    }
    return { ...ctx, url, detail: "" };
  });

  const setHandle = reduce((ctx: FlowContext, ev: any) => ({
    ...ctx,
    handle: strOf(ev, "handle"),
  }));

  const setDetail = reduce((ctx: FlowContext, ev: any) => ({
    ...ctx,
    detail: strOf(ev, "detail"),
  }));

  const setRemaining = reduce((ctx: FlowContext, ev: any) => ({
    ...ctx,
    attempts: remainingOf(ev),
  }));

  const armStart = reduce((ctx: FlowContext) => ({ ...ctx, handle: "", url: "", detail: "", attempts: cfg.maxAttempts, alreadyDone: false, inUse: false }));

  const reset = reduce((ctx: FlowContext) => ({ ...ctx, handle: "", url: "", detail: "", alreadyDone: false, inUse: false }));

  // Mark that `ok` was reached via an already-complete start hand-off, so the
  // UI renders alreadyDoneMsg ("Already signed in.") rather than doneMsg.
  const markAlreadyDone = reduce((ctx: FlowContext) => ({ ...ctx, alreadyDone: true }));

  // Mark that the current polling flow is polling an ALREADY in-use login
  // (single-flight start reused an in-flight handle). Reveals the revoke
  // affordance and renders the in-use message instead of the generic pending.
  const markInUse = reduce((ctx: FlowContext) => ({ ...ctx, inUse: true }));

  // --- invoke fns ----------------------------------------------------------

  const startFlow = () => callTool({ name: cfg.startTool, arguments: {} });

  // Revoke the in-progress flow by its handle (single-flight SSO). Only present
  // when the config supplies a revoke tool (auth_sso).
  const revokeFlow = (ctx: FlowContext) => callTool({ name: cfg.revokeTool!, arguments: { handle: ctx.handle } });

  // A single poll. A transient transport rejection is NOT terminal: return a
  // non-terminal outcome with a decremented budget so the loop retries until
  // the attempt budget is exhausted. A real `done` result carries its
  // structuredContent.
  const pollOnce = async (ctx: FlowContext) => {
    let res: ToolResult;
    try {
      res = await callTool({ name: cfg.statusTool, arguments: { handle: ctx.handle } });
    } catch (_e) {
      // Transient transport rejection: pause like the success path, then return
      // a non-terminal outcome so the loop retries until the budget exhausts.
      await sleep(cfg.pollDelayMs);
      return { remaining: ctx.attempts - 1 };
    }
    await sleep(cfg.pollDelayMs);
    return { ...res, remaining: ctx.attempts - 1 };
  };

  // --- machine ----------------------------------------------------------

  return createMachine(
    {
      idle: state(transition("start", "starting", armStart)),

      starting: invoke(
        startFlow,
        transition("done", "ok", guard(hasStatus("done")), markAlreadyDone),
        transition("done", "dead", guard(hasStatus("needs_human")), guard(withoutHandle), setDetail),
        // A needs_human hand-off flagged in_use reuses an already-in-flight
        // login: adopt its handle/URL and poll it (single-flight), rather than
        // treating it as a fresh start. Render it as "in use" with revoke.
        transition("done", "polling", guard(hasStatus("needs_human")), guard(hasHandle), guard(isInUse), setUrl, setHandle, markInUse),
        transition("done", "polling", guard(hasStatus("needs_human")), guard(hasHandle), setUrl, setHandle),
        transition("done", "error"),
        transition("error", "error"),
      ),

      polling: invoke(
        pollOnce,
        transition("done", "ok", guard(hasStatus("done"))),
        transition("done", "dead", guard(hasStatus("needs_human")), guard(withoutHandle), setDetail),
        transition("done", "timeout", guard(exhausted)),
        transition("done", "polling", setRemaining),
        transition("revoke", "revoking"),
        transition("error", "error"),
      ),

      revoking: invoke(
        revokeFlow,
        transition("done", "idle", reset),
        transition("done", "error"),
        transition("error", "error"),
      ),

      ok: state(transition("retry", "idle", reset)),
      dead: state(transition("retry", "idle", reset)),
      error: state(transition("retry", "idle", reset)),
      timeout: state(transition("retry", "idle", reset)),
    },
    () => ({ url: "", handle: "", detail: "", attempts: cfg.maxAttempts, alreadyDone: false, inUse: false }),
  );
}

// Flow machine states as a string enum. Values are plain strings (robot3 keys),
// so enum members compare directly against service.machine.current.
export enum FlowState {
  Idle = "idle",
  Starting = "starting",
  Polling = "polling",
  Ok = "ok",
  Dead = "dead",
  Error = "error",
  Timeout = "timeout",
  Revoking = "revoking",
}

/** All flow states, for iteration / membership checks. */
export const FLOW_STATES: readonly FlowState[] = [
  FlowState.Idle,
  FlowState.Starting,
  FlowState.Polling,
  FlowState.Ok,
  FlowState.Dead,
  FlowState.Error,
  FlowState.Timeout,
  FlowState.Revoking,
];

/** States where an out-of-band flow is mid-flight (button disabled). */
export const FLOW_PENDING: readonly FlowState[] = [FlowState.Starting, FlowState.Polling, FlowState.Revoking];

/** Terminal flow states (no more polling or user action needed until retry). */
export const FLOW_TERMINAL: readonly FlowState[] = [
  FlowState.Ok,
  FlowState.Dead,
  FlowState.Error,
  FlowState.Timeout,
];

/** Whether a flow state is mid-flight (starting/polling). */
export const isFlowPending = (s: FlowState): boolean => FLOW_PENDING.includes(s);

/** Whether a flow state is terminal and awaiting a retry. */
export const isFlowTerminal = (s: FlowState): boolean => FLOW_TERMINAL.includes(s);
