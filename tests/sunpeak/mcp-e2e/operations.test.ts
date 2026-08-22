import { test, expect } from 'sunpeak/test';
import { invoke } from './helpers';

/**
 * Operations domain tools (operations_list / operations_get) driven through
 * the host-discovery contract: every call goes through invoke_tool with
 * { name, args } — the same path a ChatGPT/Claude host uses.
 *
 * These read the seeded operations (internal/mcptest/account SeedOperations
 * seeds two deterministic rows: id 1 = completed pin, id 2 = running upload),
 * proving the full invoke_tool -> MCP -> SDK -> fake-API chain returns real
 * operation data (not a 501 stub or a generic error).
 */
test.describe.configure({ mode: 'serial' });

test('operations_list returns the seeded operations with real fields', async ({ mcp }) => {
  const result = await invoke(mcp, 'operations_list', {});
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  // Two seeded operations: a completed pin (id 1) and a running upload (id 2).
  expect(result).toHaveTextContent('pin');
  expect(result).toHaveTextContent('upload');
  expect(result).toHaveTextContent('completed');
});

test('operations_list filters by status', async ({ mcp }) => {
  const result = await invoke(mcp, 'operations_list', { status: 'completed' });
  expect(result).not.toBeError();
  // Only the completed pin is returned when filtering by status.
  expect(result).toHaveTextContent('pin');
  expect(result).not.toHaveTextContent('upload');
});

test('operations_get returns the detail for a seeded id', async ({ mcp }) => {
  const result = await invoke(mcp, 'operations_get', { id: 1 });
  expect(result).not.toBeError();
  expect(result).toHaveStructuredContent({ status: 'ok' });
  // The seeded id-1 operation is the completed "pin" op.
  expect(result).toHaveTextContent('pin');
  expect(result).toHaveStructuredContent({ value: { id: 1 } });
});

test('operations_get with an unknown id returns a not-found error', async ({ mcp }) => {
  const result = await invoke(mcp, 'operations_get', { id: 9999 });
  expect(result).toBeError();
});
