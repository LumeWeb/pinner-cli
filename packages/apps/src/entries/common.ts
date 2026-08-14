// Flow-app entry data: each OOB "start -> poll -> done" MCP App is one
// entrypoint with its own bundle, and differs only in its tool names, element
// ids, and message copy. This file names that data so an entry stays a plain
// object consumed by the shared mountFlowApp helper.

import type { FlowConfigCore, FlowCopy, FlowElementIds } from "@/app-entry";

/** Data an OOB flow app entry contributes, handed to mountFlowApp verbatim. */
export interface FlowAppEntry {
  name: string;
  config: FlowConfigCore & { maxAttempts?: number; pollDelayMs?: number };
  ids: FlowElementIds;
  copy: FlowCopy;
}
