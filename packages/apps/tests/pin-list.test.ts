// Behavioral tests for the pin-list read machine (src/pin-list.ts) and its DOM
// adapter (src/pin-list-bootstrap.ts). Uses the same deferred (gated) promise
// pattern as the other suites to pause an `invoke` mid-flight and assert the
// intermediate state.

import { describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createPinListMachine,
  PinListState,
  type PinListConfig,
  type PinListContext,
  type PinRow,
} from "@/pin-list";
import {
  renderPinList,
  runPinListEntry,
  currentPinListState,
  shortCid,
  type PinListElements,
} from "@/pin-list-bootstrap";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

const baseConfig: PinListConfig = {
  listTool: "pins_list",
  loadingMsg: "Reading pins...",
  errorMsg: "Could not read pins",
  refreshLabel: "Refresh",
  emptyLabel: "No pins yet.",
};

function listEnvelope(pins: PinRow[]): ToolResult {
  return { structuredContent: { status: "ok", value: pins } };
}

const samplePins: PinRow[] = [
  { cid: "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi", name: "site", status: "pinned" },
  { cid: "QmPZ9gcCEpqKTo6aq61g2nXGUhM4iCL3ewB6LDXZCtioEB", status: "queued" },
];

interface GateEntry {
  tool: string;
  resolve: (r: ToolResult) => void;
}

describe("pin-list machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: GateEntry[];
  let machine: ReturnType<typeof createPinListMachine>;
  let service: Service<ReturnType<typeof createPinListMachine>>;
  const state = (): PinListState => currentPinListState(service);
  const ctx = (): PinListContext => service.context as PinListContext;

  function scriptedCall(): CallTool {
    return async (req): Promise<ToolResult> => {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };
  }

  function boot() {
    calls = [];
    gate = [];
    machine = createPinListMachine(baseConfig, scriptedCall());
    service = interpret(machine, () => {});
  }

  it("loads the pin list and lands in ready", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    expect(state()).toBe(PinListState.Loading);
    expect(calls[0].name).toBe("pins_list");
    expect(calls[0].arguments).toEqual({});

    gate[0].resolve(listEnvelope(samplePins));
    await untilState(service, PinListState.Ready);
    expect(ctx().pins).toHaveLength(2);
    expect(ctx().pins[0].status).toBe("pinned");
    expect(ctx().errorMsg).toBe("");
  });

  it("a failing list call lands in error, and refresh recovers", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve({ isError: true, error: "boom" } as ToolResult);

    await untilState(service, PinListState.Error);
    expect(ctx().errorMsg).toContain("Could not read pins");
    expect(ctx().pins).toHaveLength(0);

    service.send({ type: "refresh" });
    await until(service, () => gate.length === 2);
    gate[1].resolve(listEnvelope([{ cid: "abc", status: "pinned" }]));
    await untilState(service, PinListState.Ready);
    expect(ctx().errorMsg).toBe("");
    expect(ctx().pins).toHaveLength(1);
  });

  it("a server error result (isError with structuredContent.error, no top-level error) lands in error", async () => {
    // Regression: real server/SDK error results flag isError and carry the
    // message inside structuredContent ({status:"error",error:<code>}), with no
    // top-level .error string. The machine must treat them as failures rather
    // than silently rendering a misleading "No pins yet." empty readout.
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve({
      isError: true,
      structuredContent: { status: "error", error: "not authorized" },
    } as ToolResult);

    await untilState(service, PinListState.Error);
    expect(ctx().errorMsg).toContain("not authorized");
    expect(ctx().pins).toHaveLength(0);
  });

  it("a bare isError result with no message falls back to a generic error", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve({ isError: true } as ToolResult);

    await untilState(service, PinListState.Error);
    expect(ctx().errorMsg).toContain("could not load pins");
    expect(ctx().pins).toHaveLength(0);
  });

  it("an empty list lands in ready and renders the empty label", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve(listEnvelope([]));
    await untilState(service, PinListState.Ready);
    expect(ctx().pins).toHaveLength(0);

    const r = renderPinList(PinListState.Ready, ctx(), baseConfig);
    expect(r.empty).toBe(true);
    expect(r.statusMsg).toBe(baseConfig.emptyLabel);
  });
});

describe("pin-list render", () => {
  const ctxOk = (pins: PinRow[] = samplePins): PinListContext => ({ pins, errorMsg: "" });

  it("loading is busy with the pending message", () => {
    const r = renderPinList(PinListState.Loading, { pins: [], errorMsg: "" }, baseConfig);
    expect(r.busy).toBe(true);
    expect(r.statusState).toBe("pending");
    expect(r.statusMsg).toBe(baseConfig.loadingMsg);
  });

  it("error surfaces the stored error text and clears rows", () => {
    const r = renderPinList(PinListState.Error, { pins: [], errorMsg: "Could not read pins: boom" }, baseConfig);
    expect(r.statusState).toBe("error");
    expect(r.statusMsg).toBe("Could not read pins: boom");
    expect(r.pins).toHaveLength(0);
  });

  it("ready shows a count summary", () => {
    const r = renderPinList(PinListState.Ready, ctxOk(samplePins), baseConfig);
    expect(r.statusState).toBe("ok");
    expect(r.statusMsg).toBe("2 pins");
    expect(r.countSummary).toBe("2 pins");
    expect(r.pins).toHaveLength(2);
    expect(r.busy).toBe(false);
  });
});

describe("shortCid", () => {
  it("truncates long CIDs to a readable prefix", () => {
    const long = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi";
    const s = shortCid(long);
    expect(s.length).toBeLessThan(long.length);
    expect(s.startsWith("bafybeigdyrz")).toBe(true);
  });

  it("leaves short CIDs unchanged", () => {
    expect(shortCid("abc123")).toBe("abc123");
  });
});

describe("runPinListEntry", () => {
  it("auto-loads on mount and exposes refresh()/state()", async () => {
    const gate: GateEntry[] = [];
    const callTool: CallTool = async (req) =>
      await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    const els: PinListElements = {
      statusEl: { textContent: "" } as unknown as HTMLElement,
      countEl: { textContent: "" } as unknown as HTMLElement,
      tableEl: { replaceChildren: () => {} },
      emptyEl: { style: { display: "" } } as unknown as HTMLElement & { style: { display: string } },
      refreshBtn: { disabled: true, addEventListener: () => {} } as unknown as PinListElements["refreshBtn"],
    };
    const run = runPinListEntry({ config: baseConfig, callTool, elements: els });

    // Mount issued a single pins_list call (predicate polls the gate, not state).
    await until({ machine: { current: "" } } as never, () => gate.length === 1, 2_000);
    expect(run.state).toBe(PinListState.Loading);
    gate[0].resolve(listEnvelope(samplePins));
    await untilState(run.service, PinListState.Ready);
    expect(run.state).toBe(PinListState.Ready);
    expect(els.countEl.textContent).toBe("2 pins");
  }, 5_000);
});
