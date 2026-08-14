// Behavioral tests for the robot3 pin-flow machine (tests/pin.ts). Uses the
// same deferred (gated) promise pattern as tests/flow.test.ts to pause an
// `invoke` mid-flight and assert the intermediate state, per the robot3
// testing pattern (see react-state-machine-steps skill).

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import { createPinMachine, type PinConfig, type PinContext, PinState } from "@/pin";
import { renderPin, currentPinState } from "@/pin-bootstrap";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

const baseConfig: PinConfig = {
  addTool: "pins_add",
  statusTool: "pin_status",
  maxAttempts: 3,
  pollDelayMs: 0,
  cidRequiredMsg: "A CID is required.",
  pinningPrefix: "Pinning ",
  scheduledMsg: "Pin scheduled.",
  failedMsg: "Pin failed.",
  pinnedMsg: "Pinned.",
  currentStatusPrefix: "Current status: ",
  timeoutLastPrefix: "Timed out polling pin status (last: ",
  timeoutLastSuffix: ").",
  checkingMsg: "Checking pin status...",
  timeoutMsg: "Timed out polling pin status.",
};

function pinsAddOk(cid: string, status = "queued"): ToolResult {
  return { structuredContent: { status: "ok", value: { CID: cid, Status: status } } };
}
function pinsStatus(status?: string): ToolResult {
  return status === undefined
    ? { structuredContent: {} }
    : { structuredContent: { status } };
}

interface GatedCall {
  tool: string;
  resolve: (r: ToolResult) => void;
}

describe("pin flow machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: GatedCall[];
  let machine: ReturnType<typeof createPinMachine>;
  let service: Service<ReturnType<typeof createPinMachine>>;
  const state = (): PinState => currentPinState(service);
  const ctx = () => service.context as PinContext;
  const sendSubmit = (cid: string, name = "") => service.send({ type: "submit", cid, name });

  function scriptedCall(): CallTool {
    return async (req): Promise<ToolResult> => {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };
  }

  beforeEach(() => {
    calls = [];
    gate = [];
    machine = createPinMachine(baseConfig, scriptedCall());
    service = interpret(machine, () => {});
  });

  it("submit valid → pins_add -> scheduled readout -> poll -> pinned", async () => {
    sendSubmit("Qm1", "file");
    // submitting invoke pending → pause at submitting
    await untilState(service, PinState.Submitting);

    // Only pins_add should have been called (no status poll yet).
    expect(calls.map((c) => c.name)).toEqual(["pins_add"]);
    expect(calls[0].arguments).toEqual({ cids: ["Qm1"], name: "file" });

    // Resolve pins_add → ok → first polling entry (fresh: "Pin scheduled.").
    gate[0].resolve(pinsAddOk("Qm1", "queued"));
    await untilState(service, PinState.Polling);
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBe("Pin scheduled.");
    expect(renderPin(state(), ctx(), baseConfig).statusState).toBe("ok");
    // pins_add readout: out-cid = value.CID, out-status = value.Status.
    expect(ctx().outCid).toBe("Qm1");
    expect(ctx().outStatus).toBe("queued");

    // Resolve the first status poll → pinned → terminal ok.
    const poll = gate.find((g) => g.tool === "pin_status");
    expect(poll).toBeTruthy();
    expect(calls.filter((c) => c.name === "pin_status").length).toBe(1);
    poll!.resolve(pinsStatus("pinned"));
    await untilState(service, PinState.Ok);
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBe("Pinned.");
    expect(ctx().outStatus).toBe("pinned");
  });

  it("missing status is NON-terminal: keeps polling until terminal", async () => {
    sendSubmit("Qm2", "");
    await untilState(service, PinState.Submitting);
    gate[0].resolve(pinsAddOk("Qm2", "queued"));
    await untilState(service, PinState.Polling);

    let pollIdx = 0;
    function nextPoll(): GatedCall {
      return gate.filter((g) => g.tool === "pin_status")[pollIdx++];
    }

    // Poll 1: missing status (IsError result) → NOT terminal → keep polling.
    nextPoll().resolve(pinsStatus(undefined));
    await until(service, () => gate.filter((g) => g.tool === "pin_status").length > 1);
    expect(state()).toBe("polling");
    // Non-terminal poll refreshes #out-status only, leaves status element alone.
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBeNull();

    // Poll 2: missing again → budget decrements.
    nextPoll().resolve(pinsStatus(undefined));
    await until(service, () => gate.filter((g) => g.tool === "pin_status").length > 2);
    expect(state()).toBe("polling");

    // Poll 3: pinned → terminal ok despite earlier missing statuses.
    nextPoll().resolve(pinsStatus("pinned"));
    await untilState(service, PinState.Ok);
    expect(calls.filter((c) => c.name === "pin_status").length).toBe(3);
  });

  it("missing status until budget exhausted → timeout (last-status message)", async () => {
    sendSubmit("Qm3", "");
    await untilState(service, PinState.Submitting);
    gate[0].resolve(pinsAddOk("Qm3", "queued"));
    await untilState(service, PinState.Polling);

    for (let i = 0; i < baseConfig.maxAttempts; i++) {
      const p = gate.filter((g) => g.tool === "pin_status")[i];
      expect(p, `poll entry #${i} should exist`).toBeTruthy();
      // First two poll with a defined-but-non-terminal status, last without.
      p!.resolve(pinsStatus(i < 2 ? "queued" : undefined));
      if (i < baseConfig.maxAttempts - 1) {
        await until(service, () => gate.filter((g) => g.tool === "pin_status").length > i + 1);
        expect(state()).toBe("polling");
      }
    }
    await untilState(service, PinState.Timeout);
    expect(renderPin(state(), ctx(), baseConfig).statusState).toBe("info");
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBe(
      "Timed out polling pin status (last: unknown).",
    );
  });

  it("pins_add isError → error terminal with Pin failed. readout", async () => {
    sendSubmit("Qm4", "");
    await untilState(service, PinState.Submitting);

    // isError pins_add result → error (no pin_status polling starts).
    gate[0].resolve({ isError: true, structuredContent: { status: "ok", value: { Status: "failed" } } });
    await untilState(service, PinState.Error);
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBe("Pin failed.");
    expect(renderPin(state(), ctx(), baseConfig).statusState).toBe("error");
    // Even on error, renderResult still surfaced the value readout (CID fallback).
    expect(ctx().outCid).toBe("Qm4");
    expect(calls.some((c) => c.name === "pin_status")).toBe(false);
  });

  it("empty CID → form_error without calling any tool", async () => {
    sendSubmit("", "");
    await untilState(service, PinState.FormError);
    expect(calls.length).toBe(0);
    expect(renderPin(state(), ctx(), baseConfig).statusMsg).toBe("A CID is required.");
    expect(renderPin(state(), ctx(), baseConfig).statusState).toBe("error");

    // Form stays submittable: a subsequent valid submit proceeds.
    sendSubmit("Qm5", "");
    await untilState(service, PinState.Submitting);
  });

  it("pins_add rejection → error terminal", async () => {
    machine = createPinMachine(baseConfig, async () => {
      throw new Error("boom");
    });
    service = interpret(machine, () => {});
    sendSubmit("Qm6", "");
    await untilState(service, PinState.Error);
  });
});
