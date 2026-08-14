// Behavioral tests for the robot3 flow machine. Uses deferred (gated) promises
// to pause an `invoke` mid-flight and assert the intermediate state, per the
// robot3 testing pattern (see react-state-machine-steps skill).

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { interpret } from "robot3";
import { createFlowMachine, type CallTool, type FlowConfig, FlowState, type ToolResult } from "@/flow";
import { currentFlowState, renderFlow } from "@/app-entry";

const baseConfig: FlowConfig = {
  startTool: "start_t",
  statusTool: "status_t",
  urlFields: ["action_url"],
  maxAttempts: 3,
  pollDelayMs: 0,
  actionLabel: "Test",
  startErrorMsg: "start failed",
  alreadyDoneMsg: "already done",
  noHandlePrefix: "no session",
  pendingMsg: "working",
  doneMsg: "done",
  deadDetailPrefix: "dead",
  timeoutMsg: "timeout",
  retryWord: "start",
};

function needsHuman(handle?: string, url?: string, detail?: string): ToolResult {
  const sc: Record<string, unknown> = { status: "needs_human" };
  if (handle) sc.handle = handle;
  if (url) sc.action_url = url;
  if (detail) sc.detail = detail;
  return { structuredContent: sc };
}

describe("flow machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let callTool: CallTool;
  let machine: ReturnType<typeof createFlowMachine>;
  let service: any;
  const state = (): FlowState => currentFlowState(service);

  beforeEach(() => {
    calls = [];
    // Default: no-op caller; tests override per-case via a gated queue.
    callTool = async () => ({ structuredContent: {} });
    machine = createFlowMachine(baseConfig, callTool);
    service = interpret(machine, () => {});
  });

  afterEach(() => {});

  it("idle → start → polling with a handle, then done", async () => {
    const gate: { op: "start" | "poll"; resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: { name: string; arguments: Record<string, unknown> }): Promise<ToolResult> {
      calls.push(req);
      const op = req.name === "start_t" ? "start" : "poll";
      return await new Promise<ToolResult>((resolve) => gate.push({ op, resolve }));
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    // starting invoke pending → pause at starting
    await Promise.resolve();
    expect(state()).toBe("starting");

    // resolve the start call → needs_human with handle+url
    const startEntry = gate.find((g) => g.op === "start");
    expect(startEntry).toBeTruthy();
    startEntry!.resolve(needsHuman("h-1", "https://x.test/approve"));

    // flush: should land in polling (handle present)
    await flush(service);
    expect(state()).toBe("polling");

    // resolve the first poll → done
    const pollEntry = gate.find((g) => g.op === "poll");
    expect(pollEntry).toBeTruthy();
    pollEntry!.resolve({ structuredContent: { status: "done" } });

    await flush(service);
    expect(state()).toBe("ok");
    // Reached ok via POLLING (start only handed off a handle): not already-done.
    expect((service.context as any).alreadyDone).toBe(false);
  });

  it("start returns needs_human WITHOUT handle → dead (no futile polling)", async () => {
    const gate: { op: "start" | "poll"; resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: any): Promise<ToolResult> {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ op: req.name === "start_t" ? "start" : "poll", resolve }));
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await Promise.resolve();
    expect(state()).toBe("starting");

    gate.find((g) => g.op === "start")!.resolve(needsHuman(undefined, undefined, "not configured"));

    await flush(service);
    expect(state()).toBe("dead");
    expect(gate.some((g) => g.op === "poll")).toBe(false); // never polled a handle-less hand-off
  });

  it("poll returns needs_human WITHOUT handle mid-flight → dead", async () => {
    const gate: { op: "start" | "poll"; resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: any): Promise<ToolResult> {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ op: req.name === "start_t" ? "start" : "poll", resolve }));
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await Promise.resolve();
    gate.find((g) => g.op === "start")!.resolve(needsHuman("h-1", "https://x.test/approve"));
    await flush(service);
    expect(state()).toBe("polling");

    gate.find((g) => g.op === "poll")!.resolve(needsHuman(undefined)); // handle dropped
    await flush(service);
    expect(state()).toBe("dead");
  });

  it("poll loop reaches timeout after maxAttempts", async () => {
    const gate: { op: "start" | "poll"; resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: any): Promise<ToolResult> {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ op: req.name === "start_t" ? "start" : "poll", resolve }));
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await Promise.resolve();
    gate.find((g) => g.op === "start")!.resolve(needsHuman("h-1", "https://x.test/approve"));
    await flush(service);
    expect(state()).toBe("polling");

    // Each poll resolves pending-with-handle; the counter decrements per poll
    // until it hits 0 → timeout. Polls are appended in order, so resolve them
    // one at a time by index.
    for (let i = 0; i < baseConfig.maxAttempts; i++) {
      // The i-th poll is the (i+1)-th "poll" entry total.
      const entry = gate.filter((g) => g.op === "poll")[i];
      expect(entry, `poll entry #${i} should exist`).toBeTruthy();
      entry!.resolve(needsHuman("h-1"));
      await flush(service);
    }
    expect(state()).toBe("timeout");
  });

  it("start already done → ok without polling", async () => {
    const gate: { op: "start" | "poll"; resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: any): Promise<ToolResult> {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ op: req.name === "start_t" ? "start" : "poll", resolve }));
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});
    service.send("start");
    await Promise.resolve();
    gate.find((g) => g.op === "start")!.resolve({ structuredContent: { status: "done" } });
    await flush(service);
    expect(state()).toBe("ok");
    expect(gate.some((g) => g.op === "poll")).toBe(false);
    // Start reported the flow already-complete: the UI should show
    // "alreadyDoneMsg" ("Already signed in.") not "doneMsg" ("Signed in.").
    expect((service.context as any).alreadyDone).toBe(true);
  });

  it("start tool rejects → error terminal", async () => {
    async function rejecting(): Promise<ToolResult> {
      throw new Error("boom");
    }
    machine = createFlowMachine(baseConfig, rejecting);
    service = interpret(machine, () => {});
    service.send("start");
    await flush(service);
    expect(state()).toBe("error");
  });

  it("poll transport rejection is NON-terminal: retries until timeout", async () => {
    // Start resolves into polling with a handle; every poll then REJECTS
    // (transient transport error). The view must retry (stay in polling), not
    // go terminal, and only reach `timeout` once the attempt budget exhausts.
    // (With pollDelayMs 0 in tests the retry loop runs in microtasks.)
    let polls = 0;
    async function scriptedCall(req: any): Promise<ToolResult> {
      calls.push(req);
      if (req.name === "start_t") return needsHuman("h-1", "https://x.test/approve");
      polls++;
      throw new Error("transport down");
    }
    machine = createFlowMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});
    service.send("start");
    await flush(service, 300);

    // The view retried the full budget (never went terminal on a rejection)
    // and ended in `timeout`, not `error`.
    expect(polls).toBe(baseConfig.maxAttempts);
    expect(state()).toBe("timeout");
  });
});

describe("renderFlow", () => {
  it("already-complete start renders alreadyDoneMsg, not doneMsg", () => {
    const ok = renderFlow(FlowState.Ok, { alreadyDone: true }, baseConfig);
    expect(ok.statusState).toBe("ok");
    expect(ok.statusMsg).toBe(baseConfig.alreadyDoneMsg);
    expect(ok.pending).toBe(false);
  });

  it("done-after-polling renders doneMsg", () => {
    const ok = renderFlow(FlowState.Ok, { alreadyDone: false }, baseConfig);
    expect(ok.statusMsg).toBe(baseConfig.doneMsg);
  });

  it("pending states are mid-flight and stamp the pending message", () => {
    for (const st of [FlowState.Starting, FlowState.Polling]) {
      const r = renderFlow(st, {}, baseConfig);
      expect(r.pending).toBe(true);
      expect(r.statusState).toBe("pending");
      expect(r.statusMsg).toBe(baseConfig.pendingMsg);
    }
  });

  it("terminal error/dead/timeout map to their messages", () => {
    expect(renderFlow(FlowState.Dead, {}, baseConfig).statusMsg).toBe(baseConfig.deadDetailPrefix);
    expect(renderFlow(FlowState.Dead, { detail: "custom" }, baseConfig).statusMsg).toBe("custom");
    expect(renderFlow(FlowState.Error, {}, baseConfig).statusMsg).toBe(baseConfig.startErrorMsg);
    expect(renderFlow(FlowState.Timeout, {}, baseConfig).statusMsg).toBe(baseConfig.timeoutMsg);
  });

  it("idle leaves the status element alone and not pending", () => {
    const r = renderFlow(FlowState.Idle, {}, baseConfig);
    expect(r.statusState).toBeNull();
    expect(r.pending).toBe(false);
  });
});

/** Drive robot3's microtask/promise queue until the machine settles. */
async function flush(service: any, rounds = 50): Promise<void> {
  for (let i = 0; i < rounds; i++) {
    await new Promise<void>((r) => setTimeout(r, 0));
  }
}
