// One-shot "deep link" flow for synchronous out-of-band MCP Apps.
//
// Most OOB apps are async: start -> poll status by handle -> done. The account
// credential change apps (password / email) are the exception: the whole change
// runs synchronously in the human's browser on a hosted page, so after the tool
// mints the page there is nothing for the app to poll. The model calls the
// start tool, gets back a needs_human hand-off carrying an action_url, and the
// human opens that page, fills the form, and is done. This machine drives that
// "click -> mint -> show the deep link" lifecycle with no poll loop.

import { createMachine, guard, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "@/flow";
import { toolError } from "@/flow";

export interface LinkConfig {
  /** Name of the MCP tool that mints the out-of-band page. */
  startTool: string;
  /** StructuredContent field carrying the human action URL. */
  urlField: string;
  /** Message copy. */
  startLabel: string;
  openLabel: string;
  startErrorMsg: string;
  noUrlMsg: string;
  alreadyDoneMsg: string;
  doneMsg: string;
}

export interface LinkContext {
  url: string;
  alreadyDone: boolean;
}

export enum LinkState {
  Idle = "idle",
  Starting = "starting",
  Ok = "ok",
  Norl = "nourl", // start returned no usable URL
  Error = "error",
}

// Map a link state + context onto the status/detail readout.
export function renderLink(
  state: LinkState,
  ctx: { url: string; alreadyDone?: boolean },
  cfg: LinkConfig,
): { status: string | null; message: string | null; pending: boolean } {
  switch (state) {
    case LinkState.Ok:
      return {
        status: "ok",
        message: ctx.alreadyDone ? cfg.alreadyDoneMsg : cfg.doneMsg,
        pending: false,
      };
    case LinkState.Norl:
      return {
        status: "error",
        message: ctx.alreadyDone ? cfg.alreadyDoneMsg : cfg.noUrlMsg,
        pending: false,
      };
    case LinkState.Error:
      return { status: "error", message: cfg.startErrorMsg, pending: false };
    case LinkState.Starting:
      return { status: "pending", message: cfg.startLabel, pending: true };
    default:
      return { status: null, message: null, pending: false };
  }
}

export function createLinkMachine(cfg: LinkConfig, callTool: CallTool) {
  const strOf = (ev: any, key: string): string => {
    const sc = ev?.data?.structuredContent;
    return sc && typeof sc === "object" && typeof sc[key] === "string" ? sc[key] : "";
  };
  const steerOf = (ev: any): string | undefined => {
    const sc = ev?.data?.structuredContent;
    return sc && typeof sc === "object" ? (sc["reason"] as string | undefined) : undefined;
  };
  // A start result is "already done" when the flow steered the human elsewhere
  // (e.g. not signed in -> auth_sso) rather than minting a fresh page.
  const arm = reduce((ctx: LinkContext) => ({ ...ctx, url: "", alreadyDone: false }));
  const reset = reduce((ctx: LinkContext) => ({ ...ctx, url: "", alreadyDone: false }));
  const setUrl = reduce((ctx: LinkContext, ev: any) => ({
    ...ctx,
    url: strOf(ev, cfg.urlField),
    alreadyDone: !strOf(ev, cfg.urlField) && !!steerOf(ev),
  }));
  const hasUrl = (ctx: LinkContext, ev: any) => !!strOf(ev, cfg.urlField);
  const lacksUrl = (ctx: LinkContext, ev: any) => !strOf(ev, cfg.urlField);

  const startFlow = () => callTool({ name: cfg.startTool, arguments: {} });

  return createMachine(
    {
      idle: state(transition("start", "starting", arm)),
      starting: invoke(
        startFlow,
        transition("done", "ok", guard(hasUrl), setUrl),
        transition("done", "nourl", guard(lacksUrl), setUrl),
        transition("error", "error"),
      ),
      ok: state(transition("retry", "idle", reset)),
      nourl: state(transition("retry", "idle", reset)),
      error: state(transition("retry", "idle", reset)),
    },
    () => ({ url: "", alreadyDone: false }),
  );
}

/** Coerce a link-state name to the LinkState enum (single boundary). */
export function currentLinkState(sm: { machine?: { current?: string } }): LinkState {
  return (sm.machine?.current ?? LinkState.Idle) as LinkState;
}

// Keep toolError a live reference so callers can reuse the message contract.
export const linkToolError = toolError;
export type { ToolResult, CallTool };
