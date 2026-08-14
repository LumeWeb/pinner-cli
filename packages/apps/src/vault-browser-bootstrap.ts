// DOM bootstrap for the "Vault browser" MCP App: adapt the robot3 browser
// machine onto the elements of the Go-rendered Vault browser HTML shell,
// producing the status line, current path, and listing readout.
//
// Element contract:
//   #vault-status  the status line (class "status <state>").
//   #vault-path    the current vault path label.
//   #vault-list    the <ul> that holds the listing rows.
//   #vault-empty   the "this directory is empty" placeholder.
//   #vault-up      button: go to the parent directory.
//   #vault-root    button: return to the vault root.
//   #vault-refresh button: reload the current directory.

import { interpret } from "robot3";
import {
  createVaultBrowserMachine,
  type VaultBrowserConfig,
  type VaultBrowserContext,
  type VaultListItem,
  BrowserState,
} from "@/vault-browser";
import type { CallTool } from "@/flow";
import type { AppDefinition, MachineCurrent } from "@/app-entry";
import { mountApp } from "@/boot";
import { byId, setStatus, StatusClass } from "@/dom";

/** Read the current state of a robot3 service as the typed BrowserState union. */
export function currentBrowserState(service: MachineCurrent): BrowserState {
  return (service.machine?.current ?? BrowserState.Loading) as BrowserState;
}

/** Element ids referenced by the Go-rendered Vault browser HTML shell. */
export type VaultBrowserElementIds = {
  status: string;
  path: string;
  list: string;
  empty: string;
  up: string;
  root: string;
  refresh: string;
};

/** Data the Vault browser app entry contributes, handed to mountVaultBrowserApp. */
export type VaultBrowserAppEntry = AppDefinition<VaultBrowserConfig, VaultBrowserElementIds>;

export interface VaultBrowserElements {
  statusEl: HTMLElement;
  pathEl: HTMLElement;
  listEl: { replaceChildren(...nodes: Node[]): void };
  emptyEl: HTMLElement;
  upBtn: { disabled: boolean; addEventListener(type: "click", fn: () => void): void };
  rootBtn: { disabled: boolean; addEventListener(type: "click", fn: () => void): void };
  refreshBtn: { disabled: boolean; addEventListener(type: "click", fn: () => void): void };
  /** Create a listing row element from a vault item (dir rows are clickable). */
  createRow?: (item: VaultListItem) => HTMLElement;
}

export interface VaultBrowserRender {
  /** Status-element class+message to stamp; null/null leaves the element alone. */
  statusState: StatusClass | null;
  statusMsg: string | null;
  /** Current path label. */
  pathLabel: string;
  /** Listing rows for the current path (empty when loading/error). */
  items: VaultListItem[];
  /** Whether the ready listing is empty (show the empty placeholder). */
  empty: boolean;
  /** Whether to disable navigation (loading). */
  busy: boolean;
}

/**
 * Return the parent directory of a vault path, preserving any explicit profile
 * authority and canonicalizing the root. This is a faithful port of the Go
 * vault path grammar (internal/core/vault ParseVaultPath): resolve the
 * `vault:` scheme and optional `//authority`, then drop the last slash-delimited
 * segment. The root has no parent (returns the root). "Up" in the browser
 * calls this with the current listing path.
 */
export function parentPath(path: string): string {
  if (!path) return "vault:/";

  let p = path;
  let authority: string | null = null;

  // Optional explicit profile authority: vault://<profile>/<path>.
  if (p.startsWith("vault://")) {
    const rest = p.slice("vault://".length);
    const cut = rest.indexOf("/");
    if (cut >= 0) {
      const auth = rest.slice(0, cut);
      if (auth) authority = auth;
      p = "vault:" + rest.slice(cut);
    } else {
      // "vault://profile" with no path component.
      if (rest) authority = rest;
      p = "vault:/";
    }
  }
  if (!p.startsWith("vault:")) return "vault:/";

  // Strip the scheme, then drop the last path segment (the current directory).
  const trimmed = p
    .slice("vault:".length)
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");

  if (!trimmed) {
    // Root / empty path canonicalizes to the root.
    return authority ? `vault://${authority}/` : "vault:/";
  }

  const segments = trimmed.split("/");
  segments.pop(); // parent = everything before the last segment
  const parent = segments.join("/");

  if (authority) {
    return `vault://${authority}${parent ? `/${parent}/` : "/"}`;
  }
  return parent ? `vault:/${parent}` : "vault:/";
}

/**
 * Append a child name (directory or file) to a vault path, preserving the
 * scheme and any explicit profile authority, with a single `/` separator.
 * `vault:/` + `docs` -> `vault:/docs`; `vault://profile/docs/` + `media` ->
 * `vault://profile/docs/media`.
 */
export function joinDirPath(path: string, name: string): string {
  let p = path;
  // Preserve an explicit authority: vault://profile/... stays vault://profile/<rest>.
  const authority = p.startsWith("vault://") ? "vault://" + p.slice("vault://".length).split("/")[0] : null;
  if (authority) {
    p = "vault:" + p.slice(authority.length);
  }
  const slash = p.endsWith("/") ? "" : "/";
  const joined = p + slash + name;
  return authority ? authority + joined.slice("vault:".length) : joined;
}

/**
 * Map a machine state + context onto the browser readout. In the ready state
 * the full status + listing are surfaced; loading/error drive the status line
 * and clear the listing.
 */
export function renderVaultBrowser(
  state: BrowserState,
  ctx: VaultBrowserContext,
  cfg: VaultBrowserConfig,
): VaultBrowserRender {
  switch (state) {
    case BrowserState.Loading:
      return {
        statusState: StatusClass.Pending,
        statusMsg: cfg.loadingMsg,
        pathLabel: ctx.path,
        items: [],
        empty: false,
        busy: true,
      };
    case BrowserState.Error:
      return {
        statusState: StatusClass.Error,
        statusMsg: ctx.errorMsg || cfg.errorMsg,
        pathLabel: ctx.path,
        items: [],
        empty: false,
        busy: false,
      };
    case BrowserState.Ready: {
      // Remote not reachable surfaces a warning on the status line but the
      // local listing still renders.
      const remoteWarn =
        ctx.status && ctx.status.remote_reachable === false ? cfg.remoteDownMsg : null;
      const count = ctx.items.length;
      const summary =
        count === 0
          ? cfg.emptyLabel
          : `${count} ${count === 1 ? "entry" : "entries"}`;
      return {
        statusState: remoteWarn ? StatusClass.Info : StatusClass.Ok,
        statusMsg: remoteWarn || summary,
        pathLabel: ctx.path,
        items: ctx.items,
        empty: count === 0,
        busy: false,
      };
    }
    default:
      return { statusState: null, statusMsg: null, pathLabel: ctx.path, items: [], empty: false, busy: false };
  }
}

export interface VaultBrowserEntryOptions {
  config: VaultBrowserConfig;
  callTool: CallTool;
  elements: VaultBrowserElements;
}

/**
 * Wire a vault-browser machine to the given elements. Returns an object with
 * `load(path)` (programmatic trigger for tests/demo) and a `state` getter.
 */
export function runVaultBrowserEntry(opts: VaultBrowserEntryOptions) {
  const machine = createVaultBrowserMachine(opts.config, opts.callTool);
  // Track the path the machine is currently listing so the up/refresh handlers
  // can derive their target without reaching into the service's internals.
  let current = opts.config.rootPath;
  const service = interpret(machine, (s) => {
    const state = currentBrowserState(s);
    const ctx = s.context;
    current = ctx.path;
    const r = renderVaultBrowser(state, ctx, opts.config);

    if (r.statusState && r.statusMsg) setStatus(opts.elements.statusEl, r.statusState, r.statusMsg);
    opts.elements.pathEl.textContent = r.pathLabel;

    for (const btn of [opts.elements.upBtn, opts.elements.rootBtn, opts.elements.refreshBtn]) {
      btn.disabled = r.busy;
    }

    // Rebuild the listing. Always clear so a stale prior path never lingers.
    // Dir rows are clickable so the human can drill into a directory; up/root/
    // refresh handle the other navigation moves. Wiring lives here (not in the
    // row builders) so both the default and any entry-provided createRow render
    // navigable rows, and the click target is derived from the currently listed
    // path.
    const rows = r.items.map((item) => {
      const create = opts.elements.createRow;
      const row = create ? create(item) : defaultRow(item);
      if (item.type === "dir" && typeof (row as HTMLElement).addEventListener === "function") {
        (row as HTMLElement).addEventListener("click", () =>
          sendLoad(joinDirPath(current, item.name)),
        );
      }
      return row;
    });
    opts.elements.listEl.replaceChildren(...rows);
    opts.elements.emptyEl.style.display = r.empty ? "" : "none";
  });

  const sendLoad = (path: string) => service.send({ type: "load", path });

  opts.elements.upBtn.addEventListener("click", () => sendLoad(parentPath(current)));
  opts.elements.rootBtn.addEventListener("click", () => sendLoad(opts.config.rootPath));
  opts.elements.refreshBtn.addEventListener("click", () => sendLoad(current));

  // Entry-triggered load: with callTool set, start reading immediately.
  sendLoad(opts.config.rootPath);

  return {
    /** Programmatic load of a vault path (used by tests/demo). */
    load: sendLoad,
    get state(): BrowserState {
      return currentBrowserState(service);
    },
    service,
  };
}

// --- helpers ---------------------------------------------------------------

/** Default row builder when the entry supplies none (tests/demo). */
function defaultRow(item: VaultListItem): HTMLElement {
  const li = document.createElement("li");
  const isDir = item.type === "dir";
  li.textContent = `${isDir ? "[dir] " : ""}${item.name}${isDir ? "/" : ""}`;
  return li;
}

/**
 * Mount the Vault browser app entrypoint: wire the browser machine to the
 * Go-rendered elements, and either run synchronously with a caller-supplied
 * `callTool` (tests/demo) or connect to the host over postMessage via bootApp,
 * advertising the CLI build version.
 */
export function mountVaultBrowserApp(
  def: VaultBrowserAppEntry,
  root: Document,
  callTool?: CallTool,
) {
  const statusEl = byId<HTMLElement>(root, def.ids.status);
  const wire = (ct: CallTool) =>
    runVaultBrowserEntry({
      config: def.config,
      callTool: ct,
      elements: {
        statusEl: statusEl!,
        pathEl: byId<HTMLElement>(root, def.ids.path)!,
        listEl: byId<HTMLUListElement>(root, def.ids.list)!,
        emptyEl: byId<HTMLElement>(root, def.ids.empty)!,
        upBtn: byId<HTMLElement & { disabled: boolean }>(root, def.ids.up)!,
        rootBtn: byId<HTMLElement & { disabled: boolean }>(root, def.ids.root)!,
        refreshBtn: byId<HTMLElement & { disabled: boolean }>(root, def.ids.refresh)!,
      },
    });
  return mountApp({ name: def.name, statusEl, wire, callTool });
}
