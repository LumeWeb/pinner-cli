import type { McpFixture, CallToolResult } from 'sunpeak/test';

/**
 * Shared MCP-driver helpers for the Sunpeak e2e suite.
 *
 * Pinner's public MCP surface uses progressive disclosure: tools/list only
 * advertises `search_tools`, `describe_tool`, and `invoke_tool`. Domain tools
 * (`account_info`, `pins_list`, `dns_*`, …) are reachable ONLY by calling
 * `invoke_tool` with `{ name, args }`. These helpers centralize that
 * boilerplate so every per-tool test file stays declarative.
 */

/** Call a domain tool through the progressive-disclosure invoke_tool path. */
export async function invoke(
  mcp: McpFixture,
  name: string,
  args?: Record<string, unknown>,
): Promise<CallToolResult> {
  return mcp.callTool('invoke_tool', { name, arguments: args ?? {} });
}

/** Concatenate all text blocks of a CallToolResult. */
export function textOf(result: CallToolResult): string {
  return (result.content ?? []).map((c) => c.text ?? '').join('');
}

/**
 * Return true when result is a clean success: not flagged as an error and
 * carrying none of the auth/network failure markers.
 *
 * The marker scan is CONTEXT-AWARE rather than naive substring matching: a
 * bare `401` or `authenticat` legitimately appears INSIDE successful payloads
 * (a nanosecond timestamp like `.024015512Z`, or the word `authenticated:
 * true`), so naive substring tests false-positive and flake (this is what
 * broke websites/ipns/pins lists in CI when returned data happened to contain
 * a `401` digit run). Only unambiguous failure shapes count: a word-bounded
 * `401` status code, `unauthor(ized)`, `connection refused`,
 * `authentication failed/required/...`, or `not authenticated`.
 */
export function isCleanSuccess(result: CallToolResult): boolean {
  if (result.isError === true) {
    return false;
  }
  return !/\b401\b|unauthor(?:ized|ised|ization)?|connection\s+refused|authentication\s+(?:failed|required|error|invalid|missing|problem)|not\s+authenticated/i.test(
    textOf(result),
  );
}

/** Call describe_tool for a domain tool, returning its CallToolResult. */
export function describeTool(mcp: McpFixture, name: string): Promise<CallToolResult> {
  return mcp.callTool('describe_tool', { name });
}

/** Call search_tools with a query (+ optional category). */
export function searchTool(
  mcp: McpFixture,
  query: string,
  category?: string,
  limit?: number,
): Promise<CallToolResult> {
  const args: Record<string, unknown> = { query };
  if (category !== undefined) {
    args.category = category;
  }
  if (limit !== undefined) {
    args.limit = limit;
  }
  return mcp.callTool('search_tools', args);
}
