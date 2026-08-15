// DOM bootstrap for the "Pin list" MCP App: adapt the robot3 pin-list machine
// onto the elements of the Go-rendered Pin list HTML shell, producing the
// status line, a count summary, and the pin table.
//
// Element contract:
//   #pinlist-status   the status line (class "status <state>").
//   #pinlist-count    the pin count summary line.
//   #pinlist-table    the <tbody> that holds the pin rows.
//   #pinlist-empty    the "no pins yet" placeholder.
//   #pinlist-refresh  button: reload the pin list.

import { interpret } from "robot3";
import {
  createPinListMachine,
  PinListState,
  type PinListConfig,
  type PinListContext,
  type PinRow,
} from "@/pin-list";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Read the current state of a robot3 service as the typed PinListState union. */
export function currentPinListState(service: MachineCurrent): PinListState {
  return (service.machine?.current ?? PinListState.Loading) as PinListState;
}

/** Element ids referenced by the Go-rendered Pin list HTML shell. */
export type PinListElementIds = {
  status: string;
  count: string;
  table: string;
  empty: string;
  refresh: string;
};

/** Data the Pin list app entry contributes, handed to mountPinListApp. */
export type PinListAppEntry = AppDefinition<PinListConfig, PinListElementIds>;

export interface PinListElements {
  statusEl: HTMLElement;
  countEl: HTMLElement;
  tableEl: { replaceChildren(...nodes: Node[]): void };
  emptyEl: HTMLElement;
  refreshBtn: { disabled: boolean; addEventListener(type: "click", fn: () => void): void };
  /** Create a table row element from a pin (returns null to skip the row). */
  createRow?: (pin: PinRow) => HTMLElement | null;
}

export interface PinListRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Count summary line (e.g. "3 pins"). */
  countSummary: string;
  /** Pins for the current read (empty when loading/error). */
  pins: PinRow[];
  /** Whether the ready list is empty (show the empty placeholder). */
  empty: boolean;
  /** Whether to disable the refresh button (loading). */
  busy: boolean;
}

/**
 * Map a machine state + context onto the pin-list readout. In the ready state
 * the full list is surfaced; loading/error drive the status line and clear the
 * list.
 */
export function renderPinList(
  state: PinListState,
  ctx: PinListContext,
  cfg: PinListConfig,
): PinListRender {
  switch (state) {
    case PinListState.Loading:
      return {
        statusState: StatusClass.Pending,
        statusMsg: cfg.loadingMsg,
        countSummary: "",
        pins: [],
        empty: false,
        busy: true,
      };
    case PinListState.Error:
      return {
        statusState: StatusClass.Error,
        statusMsg: ctx.errorMsg || cfg.errorMsg,
        countSummary: "",
        pins: [],
        empty: false,
        busy: false,
      };
    case PinListState.Ready: {
      const count = ctx.pins.length;
      return {
        statusState: StatusClass.Ok,
        statusMsg: count === 0 ? cfg.emptyLabel : `${count} ${count === 1 ? "pin" : "pins"}`,
        countSummary: count === 0 ? "" : `${count} ${count === 1 ? "pin" : "pins"}`,
        pins: ctx.pins,
        empty: count === 0,
        busy: false,
      };
    }
    default:
      return {
        statusState: null,
        statusMsg: null,
        countSummary: "",
        pins: [],
        empty: false,
        busy: false,
      };
  }
}

export interface PinListEntryOptions {
  config: PinListConfig;
  callTool: CallTool;
  elements: PinListElements;
}

/**
 * Wire a pin-list machine to the given elements. Returns an object with
 * `refresh` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runPinListEntry(opts: PinListEntryOptions) {
  const machine = createPinListMachine(opts.config, opts.callTool);
  const service = interpret(machine, (s) => {
    const state = currentPinListState(s);
    const ctx = s.context;
    const r = renderPinList(state, ctx, opts.config);

    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    opts.elements.countEl.textContent = r.countSummary;
    opts.elements.refreshBtn.disabled = r.busy;

    // Rebuild the table. Always clear so a stale prior list never lingers.
    const rows = [];
    for (const pin of r.pins) {
      const create = opts.elements.createRow;
      const el = create ? create(pin) : defaultRow(pin);
      if (el) rows.push(el);
    }
    opts.elements.tableEl.replaceChildren(...rows);
    // The empty placeholder is authored with the HTML `hidden` attribute, so
    // toggle that boolean attribute directly; the UA's `[hidden]{display:none}`
    // rule keeps it hidden when `hidden` is present, regardless of inline style.
    opts.elements.emptyEl.hidden = !r.empty;
  });

  const sendRefresh = () => service.send({ type: "refresh" });

  opts.elements.refreshBtn.addEventListener("click", sendRefresh);

  // Entry-triggered load: with callTool set, start reading immediately.
  sendRefresh();

  return {
    /** Programmatic refresh of the pin list (used by tests/demo). */
    refresh: sendRefresh,
    get state(): PinListState {
      return currentPinListState(service);
    },
    service,
  };
}

// --- helpers ---------------------------------------------------------------

/** Default row builder when the entry supplies none (tests/demo, prod default). */
function defaultRow(pin: PinRow): HTMLElement {
  const tr = document.createElement("tr");
  tr.className = "table-row";
  const name = pin.name || shortCid(pin.cid);
  const status = pin.status || "—";
  const created = pin.created ? new Date(pin.created).toLocaleString() : "—";
  for (const [text, cls] of [
    [name, "table-cell"],
    [status, "table-cell muted"],
    [created, "table-cell muted"],
  ] as const) {
    const td = document.createElement("td");
    td.textContent = text;
    td.className = cls;
    tr.appendChild(td);
  }
  return tr;
}

/** Truncate a long CID to a readable prefix. */
export function shortCid(cid: string): string {
  return cid.length > 20 ? `${cid.slice(0, 12)}…${cid.slice(-6)}` : cid;
}

/**
 * Mount the Pin list app entrypoint: wire the pin-list machine to the
 * Go-rendered elements, and either run synchronously with a caller-supplied
 * `callTool` (tests/demo) or connect to the host over postMessage via bootApp,
 * advertising the CLI build version.
 */
export function mountPinListApp(def: PinListAppEntry, root: Document, callTool?: CallTool) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runPinListEntry({
      config: def.config,
      callTool: ct,
      elements: {
        statusEl: statusEl!,
        countEl: byId<HTMLElement>(root, def.ids.count)!,
        tableEl: byId<HTMLElement>(root, def.ids.table)!,
        emptyEl: byId<HTMLElement>(root, def.ids.empty)!,
        refreshBtn: byId<HTMLElement & { disabled: boolean }>(root, def.ids.refresh)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}
