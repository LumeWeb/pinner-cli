// Per-app flow configuration + element wiring map. Each MCP app is one
// entrypoint with its own bundle; this is the single place its tool names,
// element ids, and message copy live.

import type { FlowConfig } from "@/flow";

export interface AppDefinition {
  name: string;
  config: Omit<FlowConfig, "actionLabel" | "startErrorMsg" | "alreadyDoneMsg" | "noHandlePrefix" | "pendingMsg" | "doneMsg" | "deadDetailPrefix" | "timeoutMsg" | "retryWord">;
  /** Element ids referenced by the Go-rendered HTML shell. */
  ids: {
    startBtn: string;
    urlEl: string;
    statusEl: string;
  };
  /** Message copy. */
  copy: Pick<FlowConfig, "actionLabel" | "startErrorMsg" | "alreadyDoneMsg" | "noHandlePrefix" | "pendingMsg" | "doneMsg" | "deadDetailPrefix" | "timeoutMsg" | "retryWord">;
}

export function toFlowConfig(def: AppDefinition): FlowConfig {
  return {
    ...def.config,
    ...def.copy,
    maxAttempts: def.config.maxAttempts ?? 60,
    pollDelayMs: def.config.pollDelayMs ?? 1500,
  };
}
