// Pure read-only state machine for the "Vault browser" MCP App, authored with
// robot3. It drives the status + listing readout: on mount it loads the vault
// status and the current path's listing, then renders them. Navigating into a
// directory re-loads at that path.
//
// The machine is deliberately free of DOM and MCP-transport concerns so it can
// be unit-tested in node with a stubbed callTool. The DOM bootstrap
// (vault-browser-bootstrap.ts) adapts these states onto real elements.
//
// States:
//   loading — status + listing being fetched for the current path.
//   ready   — data shown; the human can navigate into a dir, go up, or refresh.
//   error   — the load failed; the error is surfaced and the human can retry.
//
// This is a read surface: it never mutates the vault. The agent keeps using the
// vault_* catalog tools; this view only re-renders their output for a human.

import { createMachine, invoke, reduce, state, transition } from "robot3";
import type { CallTool, ToolResult } from "./flow";
import { rejectToError, toolError } from "./flow";

export interface VaultBrowserConfig {
  /** MCP tool returning the vault status envelope ({status:"ok", value:Status}). */
  statusTool: string;
  /** MCP tool returning a path listing ({status:"ok", value:ListItem[]}). */
  listTool: string;
  /** Root vault path (e.g. "vault:/"). */
  rootPath: string;

  // Message copy.
  loadingMsg: string;
  errorMsg: string; // prefix; specific error text appended
  refreshLabel: string;
  upLabel: string;
  rootLabel: string;
  emptyLabel: string; // shown when a directory has no entries
  remoteDownMsg: string;
}

/** Shape of `vault_status`'s value member (the core StatusResult JSON). */
export interface VaultStatus {
  unlocked?: boolean;
  remote_reachable?: boolean;
  remote_ready?: boolean;
  remote_error?: string;
  storage_used?: number;
  storage_limit?: number;
  remaining_storage?: number;
  cache_state?: string; // "missing" | "healthy"
  objects_indexed?: number;
  total_bytes?: number;
  last_sync_time?: string;
}

/** Shape of one `vault_ls` value entry (the core ListItem JSON). */
export interface VaultListItem {
  name: string;
  type: string; // "file" | "dir"
  size?: number;
  media_type?: string;
  created_at?: string;
  updated_at?: string;
}

export interface VaultBrowserContext {
  /** Current vault path being listed. */
  path: string;
  /** Vault status for the selected profile, or null while loading. */
  status: VaultStatus | null;
  /** Listing for the current path. */
  items: VaultListItem[];
  /** Human-readable load error, for the error state. */
  errorMsg: string;
}

/** A `load` event carries the target vault path to list. */
interface LoadEvent {
  type: "load";
  path: string;
}

// Lift the "value" member of the {status, value} catalog envelope. The value is
// usually a plain object/array already parsed by the transport, but can arrive
// as a JSON string; tolerate both.
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
 * Build the vault-browser machine. `callTool` is the injected MCP tool caller;
 * the machine stays testable by passing a stub here.
 */
export function createVaultBrowserMachine(cfg: VaultBrowserConfig, callTool: CallTool) {
  const dataOf = (ev: any): any => (ev && typeof ev === "object" ? ev.data : undefined);
  const errorOf = (ev: any): string => (ev && ev.error ? String(ev.error) : "");
  const envStatus = (ev: any): VaultStatus | undefined => {
    const v = envelopeValue(dataOf(ev)?.statusRes?.structuredContent);
    return v && typeof v === "object" ? (v as VaultStatus) : undefined;
  };
  const envList = (ev: any): VaultListItem[] | undefined => {
    const v = envelopeValue(dataOf(ev)?.listRes?.structuredContent);
    return Array.isArray(v) ? (v as VaultListItem[]) : undefined;
  };

  // --- reducers -----------------------------------------------------------

  const setPath = reduce((ctx: VaultBrowserContext, ev: LoadEvent) => ({
    ...ctx,
    path: ev.path || ctx.path,
  }));

  // Both calls succeeded: store the loaded status + listing.
  const applyReady = reduce((ctx: VaultBrowserContext, ev: any) => ({
    ...ctx,
    status: envStatus(ev) ?? ctx.status,
    items: envList(ev) ?? [],
    errorMsg: "",
  }));

  // A call errored: robot3's native `error` event carries the message (not the
  // resolve envelope), so surface it and clear rows.
  const applyError = reduce((ctx: VaultBrowserContext, ev: any) => ({
    ...ctx,
    items: [],
    errorMsg: cfg.errorMsg + (errorOf(ev) ? `: ${errorOf(ev)}` : ""),
  }));

  const clearError = reduce((ctx: VaultBrowserContext) => ({ ...ctx, errorMsg: "" }));

  // --- invoke fn ----------------------------------------------------------

  // Load both the profile status and the current path listing. If either call
  // fails (an error result, not a reject), reject so robot3 fires its native
  // `error` event and the machine lands deterministically in the error state.
  const load = async (ctx: VaultBrowserContext): Promise<{ statusRes: ToolResult; listRes: ToolResult }> => {
    const one = (name: string, args: Record<string, unknown>): Promise<ToolResult> =>
      callTool({ name, arguments: args }).then((r) => r, rejectToError);
    const [statusRes, listRes] = await Promise.all([
      one(cfg.statusTool, {}),
      one(cfg.listTool, { path: ctx.path }),
    ]);
    // Any result flagged isError is a failure (see toolError for the message
    // extraction). A failed call must surface rather than render empty rows.
    const firstError = toolError(statusRes, "vault operation failed") || toolError(listRes, "vault operation failed");
    if (firstError) {
      throw new Error(firstError);
    }
    return { statusRes, listRes };
  };

  // --- machine ------------------------------------------------------------

  return createMachine(
    {
      loading: invoke(
        load,
        transition("done", "ready", applyReady),
        transition("error", "error", applyError),
      ),
      // A single `load` event drives all navigation — into a dir, up, or
      // refresh — distinguished only by the path the bootstrap computes.
      ready: state(transition("load", "loading", setPath, clearError)),
      error: state(transition("load", "loading", setPath, clearError)),
    },
    () => ({
      path: cfg.rootPath,
      status: null,
      items: [],
      errorMsg: "",
    }),
  );
}

// Vault-browser machine states as a string enum. Values are plain strings
// (robot3 keys), so enum members compare directly against service.machine.current.
export enum BrowserState {
  Loading = "loading",
  Ready = "ready",
  Error = "error",
}

/** All vault-browser states, for iteration / membership checks. */
export const BROWSER_STATES: readonly BrowserState[] = [
  BrowserState.Loading,
  BrowserState.Ready,
  BrowserState.Error,
];

/** Whether a browser state is a settled, data-displaying readout. */
export const isBrowserReady = (s: BrowserState): boolean => s === BrowserState.Ready;
