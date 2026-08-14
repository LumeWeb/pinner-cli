// DOM bootstrap for the "Account" MCP App: adapt the robot3 auth-status machine
// onto the elements of the Go-rendered Account HTML shell, producing the status
// line, an authenticated/not-authenticated readout, and the account message.
//
// Element contract:
//   #authstatus-status   the status line (class "status <state>").
//   #authstatus-outcome  the authenticated/not-authenticated label.
//   #authstatus-message  the account message line (or portal hint).
//   #authstatus-refresh  button: reload the auth status.

import { interpret } from "robot3";
import {
  createAuthStatusMachine,
  AuthStatusState,
  type AuthStatusConfig,
  type AuthStatusContext,
  type AuthStatusData,
} from "@/auth-status";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Read the current state of a robot3 service as the typed AuthStatusState union. */
export function currentAuthStatusState(service: MachineCurrent): AuthStatusState {
  return (service.machine?.current ?? AuthStatusState.Loading) as AuthStatusState;
}

/** Element ids referenced by the Go-rendered Account HTML shell. */
export type AuthStatusElementIds = {
  status: string;
  outcome: string;
  message: string;
  refresh: string;
};

/** Data the Account app entry contributes, handed to mountAuthStatusApp. */
export type AuthStatusAppEntry = AppDefinition<AuthStatusConfig, AuthStatusElementIds>;

export interface AuthStatusElements {
  statusEl: HTMLElement;
  outcomeEl: HTMLElement;
  messageEl: HTMLElement;
  refreshBtn: { disabled: boolean; addEventListener(type: "click", fn: () => void): void };
}

export interface AuthStatusRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Outcome label ("Authenticated" | "Not authenticated" | "" when loading). */
  outcomeLabel: string;
  /** Account message line (empty while loading). */
  message: string;
  /** Whether to disable the refresh button (loading). */
  busy: boolean;
}

/**
 * Map a machine state + context onto the account readout. In the ready state
 * the authenticated/not-authenticated outcome and message are surfaced;
 * loading/error drive the status line.
 */
export function renderAuthStatus(
  state: AuthStatusState,
  ctx: AuthStatusContext,
  cfg: AuthStatusConfig,
): AuthStatusRender {
  switch (state) {
    case AuthStatusState.Loading:
      return {
        statusState: StatusClass.Pending,
        statusMsg: cfg.loadingMsg,
        outcomeLabel: "",
        message: "",
        busy: true,
      };
    case AuthStatusState.Error:
      return {
        statusState: StatusClass.Error,
        statusMsg: ctx.errorMsg || cfg.errorMsg,
        outcomeLabel: "",
        message: "",
        busy: false,
      };
    case AuthStatusState.Ready: {
      const authed = ctx.status?.authenticated === true;
      return {
        statusState: authed ? StatusClass.Ok : StatusClass.Info,
        statusMsg: authed ? cfg.authenticatedMsg : cfg.notAuthenticatedMsg,
        outcomeLabel: authed ? "Authenticated" : "Not authenticated",
        message: ctx.status?.message || "",
        busy: false,
      };
    }
    default:
      return {
        statusState: null,
        statusMsg: null,
        outcomeLabel: "",
        message: "",
        busy: false,
      };
  }
}

export interface AuthStatusEntryOptions {
  config: AuthStatusConfig;
  callTool: CallTool;
  elements: AuthStatusElements;
}

/**
 * Wire an auth-status machine to the given elements. Returns an object with
 * `refresh` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runAuthStatusEntry(opts: AuthStatusEntryOptions) {
  const machine = createAuthStatusMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s) => {
    const state = currentAuthStatusState(s);
    const ctx = s.context;
    const r = renderAuthStatus(state, ctx, opts.config);

    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    opts.elements.outcomeEl.textContent = r.outcomeLabel;
    opts.elements.messageEl.textContent = r.message;
    opts.elements.refreshBtn.disabled = r.busy;
  });

  const sendRefresh = () => service.send({ type: "refresh" });

  opts.elements.refreshBtn.addEventListener("click", sendRefresh);

  // Entry-triggered load: with callTool set, start reading immediately.
  sendRefresh();

  return {
    /** Programmatic refresh of the auth status (used by tests/demo). */
    refresh: sendRefresh,
    get state(): AuthStatusState {
      return currentAuthStatusState(service);
    },
    service,
  };
}

/**
 * Mount the Account app entrypoint: wire the auth-status machine to the
 * Go-rendered elements, and either run synchronously with a caller-supplied
 * `callTool` (tests/demo) or connect to the host over postMessage via bootApp,
 * advertising the CLI build version.
 */
export function mountAuthStatusApp(def: AuthStatusAppEntry, root: Document, callTool?: CallTool) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runAuthStatusEntry({
      config: def.config,
      callTool: ct,
      elements: {
        statusEl: statusEl!,
        outcomeEl: byId<HTMLElement>(root, def.ids.outcome)!,
        messageEl: byId<HTMLElement>(root, def.ids.message)!,
        refreshBtn: byId<HTMLElement & { disabled: boolean }>(root, def.ids.refresh)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}
