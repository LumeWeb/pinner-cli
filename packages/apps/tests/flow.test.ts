// Behavioral tests for the robot3 flow machine. Uses deferred (gated) promises
// to pause an `invoke` mid-flight and assert the intermediate state, per the
// robot3 testing pattern (see react-state-machine-steps skill).

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import { createFlowMachine, type CallTool, type FlowConfig, FlowState, type ToolResult } from "@/flow";
import { currentFlowState, renderFlow } from "@/app-entry";
import { until, untilState } from "./helpers";

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
  let service: Service<ReturnType<typeof createFlowMachine>>;
  const state = (): FlowState => currentFlowState(service);

  beforeEach(() => {
    calls = [];
    // Default: no-op caller; tests override per-case via a gated queue.
    callTool = async () => ({ structuredContent: {} });
    machine = createFlowMachine(baseConfig, callTool);
    service = interpret(machine, () => {});
  });

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
    await untilState(service, FlowState.Starting);

    // resolve the start call → needs_human with handle+url
    const startEntry = gate.find((g) => g.op === "start");
    expect(startEntry).toBeTruthy();
    startEntry!.resolve(needsHuman("h-1", "https://x.test/approve"));

    // wait until the start hand-off lands in polling (handle present)
    await untilState(service, FlowState.Polling);

    // resolve the first poll → done
    const pollEntry = gate.find((g) => g.op === "poll");
    expect(pollEntry).toBeTruthy();
    pollEntry!.resolve({ structuredContent: { status: "done" } });

    await untilState(service, FlowState.Ok);
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
    await untilState(service, FlowState.Starting);

    gate.find((g) => g.op === "start")!.resolve(needsHuman(undefined, undefined, "not configured"));

    await untilState(service, FlowState.Dead);
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
    await untilState(service, FlowState.Starting);
    gate.find((g) => g.op === "start")!.resolve(needsHuman("h-1", "https://x.test/approve"));
    await untilState(service, FlowState.Polling);

    gate.find((g) => g.op === "poll")!.resolve(needsHuman(undefined)); // handle dropped
    await untilState(service, FlowState.Dead);
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
    await untilState(service, FlowState.Starting);
    gate.find((g) => g.op === "start")!.resolve(needsHuman("h-1", "https://x.test/approve"));
    await untilState(service, FlowState.Polling);

    // Each poll resolves pending-with-handle; the counter decrements per poll
    // until it hits 0 → timeout. Polls are appended in order, so resolve them
    // one at a time by index, waiting for the machine to issue the next poll
    // before the next iteration reads it from the gate.
    for (let i = 0; i < baseConfig.maxAttempts; i++) {
      const entry = gate.filter((g) => g.op === "poll")[i];
      expect(entry, `poll entry #${i} should exist`).toBeTruthy();
      entry!.resolve(needsHuman("h-1"));
      if (i < baseConfig.maxAttempts - 1) {
        await until(
          service,
          () => gate.filter((g) => g.op === "poll").length > i + 1,
        );
      }
    }
    await untilState(service, FlowState.Timeout);
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
    await untilState(service, FlowState.Starting);
    gate.find((g) => g.op === "start")!.resolve({ structuredContent: { status: "done" } });
    await untilState(service, FlowState.Ok);
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
    await untilState(service, FlowState.Error);
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
    await untilState(service, FlowState.Timeout);

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

  it("empty messages still resolve to a real state so the status is stamped", () => {
    // Matches runAppEntry's apply guard: it stamps whenever statusState is set
    // (not when the message is truthy), so an empty doneMsg/deadDetailPrefix
    // must NOT fall back to "leave the element alone".
    const empty = { ...baseConfig, doneMsg: "", deadDetailPrefix: "" };
    expect(renderFlow(FlowState.Ok, { alreadyDone: false }, empty).statusState).not.toBeNull();
    expect(renderFlow(FlowState.Dead, {}, empty).statusState).not.toBeNull();
  });
});
