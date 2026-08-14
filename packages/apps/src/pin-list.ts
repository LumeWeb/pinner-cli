// Pure read-only state machine for the "Pin list" MCP App, authored with
// robot3. On mount it loads the pin list (pins_list) and renders each pin with
// its status. It is a read surface: it never mutates pins and drives no
// hand-off; the agent keeps using the pins_* catalog tools directly.
//
// The machine is deliberately free of DOM and MCP-transport concerns so it can
// be unit-tested in node with a stubbed callTool. The DOM bootstrap
// (pin-list-bootstrap.ts) adapts these states onto real elements.
//
// States:
//   loading — the pin list being fetched.
//   ready   — the list shown; the human can refresh.
//   error   — the load failed; the error is surfaced and the human can retry.

import { createMachine, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

export interface PinListConfig {
  /** MCP tool returning the pin list envelope ({status:"ok", value:[...]}). */
  listTool: string;

  // Message copy.
  loadingMsg: string;
  errorMsg: string; // prefix; specific error text appended
  refreshLabel: string;
  emptyLabel: string; // shown when there are no pins
}

/** Shape of one `pins_list` value entry (the core pinning.Pin JSON). */
export interface PinRow {
  cid: string;
  name?: string;
  status?: string;
  created?: string;
  request_id?: string;
  metadata?: Record<string, string>;
}

export interface PinListContext {
  /** Pins loaded for the current read. */
  pins: PinRow[];
  /** Human-readable load error, for the error state. */
  errorMsg: string;
}

/** A `refresh` event reloads the pin list. */
interface RefreshEvent {
  type: "refresh";
}

// Lift the "value" member of the {status, value} catalog envelope. The value is
// usually a plain array already parsed by the transport, but can arrive as a
// JSON string; tolerate both.
function envelopeValue(sc: unknown): any {
  if (!sc || typeof sc !== "object") return undefined;
  const v = (sc as Record<string, any>).value;
  if (typeof v === "string") {
    try {
      return JSON.parse(v);
    } catch {
      return undefined;
    }
  }
  return v;
}

/**
 * Build the pin-list machine. `callTool` is the injected MCP tool caller;
 * the machine stays testable by passing a stub here.
 */
export function createPinListMachine(cfg: PinListConfig, callTool: CallTool) {
  const dataOf = (ev: any): any => (ev && typeof ev === "object" ? ev.data : undefined);
  const errorOf = (ev: any): string => (ev && ev.error ? String(ev.error) : "");
  const envPins = (ev: any): PinRow[] | undefined => {
    const v = envelopeValue(dataOf(ev)?.listRes?.structuredContent);
    return Array.isArray(v) ? (v as PinRow[]) : undefined;
  };

  // --- reducers -----------------------------------------------------------

  // The list loaded: store it and clear any prior error.
  const applyReady = reduce((ctx: PinListContext, ev: any) => ({
    ...ctx,
    pins: envPins(ev) ?? [],
    errorMsg: "",
  }));

  // The load errored: robot3's native `error` event carries the message, so
  // surface it and clear the list.
  const applyError = reduce((ctx: PinListContext, ev: any) => ({
    ...ctx,
    pins: [],
    errorMsg: cfg.errorMsg + (errorOf(ev) ? `: ${errorOf(ev)}` : ""),
  }));

  const clearError = reduce((ctx: PinListContext) => ({ ...ctx, errorMsg: "" }));

  // --- invoke fn ----------------------------------------------------------

  // Load the pin list. Failures (an error result, not a reject) reject so
  // robot3 fires its native `error` event and the machine lands deterministically
  // in the error state.
  const load = async (_ctx: PinListContext): Promise<{ listRes: ToolResult }> => {
    const listRes = await callTool({ name: cfg.listTool, arguments: {} }).then(
      (r) => r,
      (e) => ({ isError: true, error: String(e) } as ToolResult),
    );
    const err = listRes && listRes.isError && typeof (listRes as { error?: unknown }).error === "string"
      ? ((listRes as { error: string }).error || "")
      : "";
    if (err) {
      throw new Error(err);
    }
    return { listRes };
  };

  // --- machine ------------------------------------------------------------

  return createMachine(
    {
      loading: invoke(
        load,
        transition("done", "ready", applyReady),
        transition("error", "error", applyError),
      ),
      // A single `refresh` event reloads the list from ready or error.
      ready: state(transition("refresh", "loading", clearError)),
      error: state(transition("refresh", "loading", clearError)),
    },
    () => ({
      pins: [],
      errorMsg: "",
    }),
  );
}

// Pin-list machine states as a string enum. Values are plain strings (robot3
// keys), so enum members compare directly against service.machine.current.
export enum PinListState {
  Loading = "loading",
  Ready = "ready",
  Error = "error",
}

/** All pin-list states, for iteration / membership checks. */
export const PIN_LIST_STATES: readonly PinListState[] = [
  PinListState.Loading,
  PinListState.Ready,
  PinListState.Error,
];

/** Whether a pin-list state is a settled, data-displaying readout. */
export const isPinListReady = (s: PinListState): boolean => s === PinListState.Ready;
