// Pure read-only state machine for the "Account" MCP App, authored with
// robot3. On mount it loads the authentication/account status (auth_status)
// and renders whether the human is authenticated plus the account message. It
// is a read surface: it never mutates auth state and drives no hand-off; the
// agent keeps using the auth_* catalog tools directly.
//
// The machine is deliberately free of DOM and MCP-transport concerns so it can
// be unit-tested in node with a stubbed callTool. The DOM bootstrap
// (auth-status-bootstrap.ts) adapts these states onto real elements.
//
// States:
//   loading — the status being fetched.
//   ready   — the authenticated state shown; the human can refresh.
//   error   — the load failed; the error is surfaced and the human can retry.

import { createMachine, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";

export interface AuthStatusConfig {
  /** MCP tool returning the auth status envelope ({status:"ok", value:Status}). */
  statusTool: string;

  // Message copy.
  loadingMsg: string;
  errorMsg: string; // prefix; specific error text appended
  refreshLabel: string;
  authenticatedMsg: string; // shown when authenticated
  notAuthenticatedMsg: string; // shown when not authenticated
}

/** Shape of `auth_status`'s value member (the core AuthStatusResult JSON). */
export interface AuthStatusData {
  authenticated?: boolean;
  portal_url?: string;
  message?: string;
}

export interface AuthStatusContext {
  /** The loaded auth status, or null while loading. */
  status: AuthStatusData | null;
  /** Human-readable load error, for the error state. */
  errorMsg: string;
}

/** A `refresh` event reloads the auth status. */
interface RefreshEvent {
  type: "refresh";
}

// Lift the "value" member of the {status, value} catalog envelope. The value is
// usually a plain object already parsed by the transport, but can arrive as a
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
 * Build the auth-status machine. `callTool` is the injected MCP tool caller;
 * the machine stays testable by passing a stub here.
 */
export function createAuthStatusMachine(cfg: AuthStatusConfig, callTool: CallTool) {
  const dataOf = (ev: any): any => (ev && typeof ev === "object" ? ev.data : undefined);
  const errorOf = (ev: any): string => (ev && ev.error ? String(ev.error) : "");
  const envStatus = (ev: any): AuthStatusData | undefined => {
    const v = envelopeValue(dataOf(ev)?.res?.structuredContent);
    return v && typeof v === "object" ? (v as AuthStatusData) : undefined;
  };

  // --- reducers -----------------------------------------------------------

  // The status loaded: store it and clear any prior error.
  const applyReady = reduce((ctx: AuthStatusContext, ev: any) => ({
    ...ctx,
    status: envStatus(ev) ?? null,
    errorMsg: "",
  }));

  // The load errored: robot3's native `error` event carries the message, so
  // surface it and clear the status.
  const applyError = reduce((ctx: AuthStatusContext, ev: any) => ({
    ...ctx,
    status: null,
    errorMsg: cfg.errorMsg + (errorOf(ev) ? `: ${errorOf(ev)}` : ""),
  }));

  const clearError = reduce((ctx: AuthStatusContext) => ({ ...ctx, errorMsg: "" }));

  // --- invoke fn ----------------------------------------------------------

  // Load the auth status. Failures (an error result, not a reject) reject so
  // robot3 fires its native `error` event and the machine lands deterministically
  // in the error state.
  const load = async (_ctx: AuthStatusContext): Promise<{ res: ToolResult }> => {
    const res = await callTool({ name: cfg.statusTool, arguments: {} }).then(
      (r) => r,
      (e) => ({ isError: true, error: String(e) } as ToolResult),
    );
    const err = res && res.isError && typeof (res as { error?: unknown }).error === "string"
      ? ((res as { error: string }).error || "")
      : "";
    if (err) {
      throw new Error(err);
    }
    return { res };
  };

  // --- machine ------------------------------------------------------------

  return createMachine(
    {
      loading: invoke(
        load,
        transition("done", "ready", applyReady),
        transition("error", "error", applyError),
      ),
      // A single `refresh` event reloads the status from ready or error.
      ready: state(transition("refresh", "loading", clearError)),
      error: state(transition("refresh", "loading", clearError)),
    },
    () => ({
      status: null,
      errorMsg: "",
    }),
  );
}

// Auth-status machine states as a string enum. Values are plain strings
// (robot3 keys), so enum members compare directly against service.machine.current.
export enum AuthStatusState {
  Loading = "loading",
  Ready = "ready",
  Error = "error",
}

/** All auth-status states, for iteration / membership checks. */
export const AUTH_STATUS_STATES: readonly AuthStatusState[] = [
  AuthStatusState.Loading,
  AuthStatusState.Ready,
  AuthStatusState.Error,
];

/** Whether an auth-status state is a settled, data-displaying readout. */
export const isAuthStatusReady = (s: AuthStatusState): boolean => s === AuthStatusState.Ready;
