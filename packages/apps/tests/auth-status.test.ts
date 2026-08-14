// Behavioral tests for the auth-status read machine (src/auth-status.ts) and
// its DOM adapter (src/auth-status-bootstrap.ts). Uses the same deferred
// (gated) promise pattern as the other suites to pause an `invoke` mid-flight
// and assert the intermediate state.

import { describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createAuthStatusMachine,
  AuthStatusState,
  type AuthStatusConfig,
  type AuthStatusContext,
  type AuthStatusData,
} from "@/auth-status";
import {
  renderAuthStatus,
  runAuthStatusEntry,
  currentAuthStatusState,
  type AuthStatusElements,
} from "@/auth-status-bootstrap";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

const baseConfig: AuthStatusConfig = {
  statusTool: "auth_status",
  loadingMsg: "Checking account...",
  errorMsg: "Could not read account status",
  refreshLabel: "Refresh",
  authenticatedMsg: "Authenticated.",
  notAuthenticatedMsg: "Not authenticated.",
};

function statusEnvelope(d: AuthStatusData): ToolResult {
  return { structuredContent: { status: "ok", value: d } };
}

interface GateEntry {
  tool: string;
  resolve: (r: ToolResult) => void;
}

describe("auth-status machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: GateEntry[];
  let machine: ReturnType<typeof createAuthStatusMachine>;
  let service: Service<ReturnType<typeof createAuthStatusMachine>>;
  const state = (): AuthStatusState => currentAuthStatusState(service);
  const ctx = (): AuthStatusContext => service.context as AuthStatusContext;

  function scriptedCall(): CallTool {
    return async (req): Promise<ToolResult> => {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };
  }

  function boot() {
    calls = [];
    gate = [];
    machine = createAuthStatusMachine(baseConfig, scriptedCall());
    service = interpret(machine, () => {});
  }

  it("loads auth status and lands in ready when authenticated", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    expect(state()).toBe(AuthStatusState.Loading);
    expect(calls[0].name).toBe("auth_status");
    expect(calls[0].arguments).toEqual({});

    gate[0].resolve(statusEnvelope({ authenticated: true, message: "Welcome back" }));
    await untilState(service, AuthStatusState.Ready);
    expect(ctx().status?.authenticated).toBe(true);
    expect(ctx().errorMsg).toBe("");
  });

  it("handles the not-authenticated outcome", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve(statusEnvelope({ authenticated: false, message: "No token configured" }));
    await untilState(service, AuthStatusState.Ready);
    expect(ctx().status?.authenticated).toBe(false);
    const r = renderAuthStatus(AuthStatusState.Ready, ctx(), baseConfig);
    expect(r.outcomeLabel).toBe("Not authenticated");
    expect(r.statusMsg).toBe(baseConfig.notAuthenticatedMsg);
  });

  it("a failing call lands in error, and refresh recovers", async () => {
    boot();
    service.send({ type: "refresh" });
    await until(service, () => gate.length === 1);
    gate[0].resolve({ isError: true, error: "boom" } as ToolResult);

    await untilState(service, AuthStatusState.Error);
    expect(ctx().errorMsg).toContain("Could not read account status");
    expect(ctx().status).toBeNull();

    service.send({ type: "refresh" });
    await until(service, () => gate.length === 2);
    gate[1].resolve(statusEnvelope({ authenticated: true }));
    await untilState(service, AuthStatusState.Ready);
    expect(ctx().errorMsg).toBe("");
    expect(ctx().status?.authenticated).toBe(true);
  });
});

describe("auth-status render", () => {
  it("loading is busy with the pending message", () => {
    const r = renderAuthStatus(AuthStatusState.Loading, { status: null, errorMsg: "" }, baseConfig);
    expect(r.busy).toBe(true);
    expect(r.statusState).toBe("pending");
    expect(r.outcomeLabel).toBe("");
  });

  it("error surfaces the stored error text", () => {
    const r = renderAuthStatus(AuthStatusState.Error, { status: null, errorMsg: "Could not read account status: boom" }, baseConfig);
    expect(r.statusState).toBe("error");
    expect(r.statusMsg).toBe("Could not read account status: boom");
  });

  it("ready authenticated shows ok with the account message", () => {
    const r = renderAuthStatus(AuthStatusState.Ready, { status: { authenticated: true, message: "Welcome" }, errorMsg: "" }, baseConfig);
    expect(r.statusState).toBe("ok");
    expect(r.statusMsg).toBe(baseConfig.authenticatedMsg);
    expect(r.outcomeLabel).toBe("Authenticated");
    expect(r.message).toBe("Welcome");
    expect(r.busy).toBe(false);
  });
});

describe("runAuthStatusEntry", () => {
  it("auto-loads on mount and exposes refresh()/state()", async () => {
    const gate: GateEntry[] = [];
    const callTool: CallTool = async (req) =>
      await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    const els: AuthStatusElements = {
      statusEl: { textContent: "" } as unknown as HTMLElement,
      outcomeEl: { textContent: "" } as unknown as HTMLElement,
      messageEl: { textContent: "" } as unknown as HTMLElement,
      refreshBtn: { disabled: true, addEventListener: () => {} } as unknown as AuthStatusElements["refreshBtn"],
    };
    const run = runAuthStatusEntry({ config: baseConfig, callTool, elements: els });

    await until({ machine: { current: "" } } as never, () => gate.length === 1, 2_000);
    expect(run.state).toBe(AuthStatusState.Loading);
    gate[0].resolve(statusEnvelope({ authenticated: true }));
    await untilState(run.service, AuthStatusState.Ready);
    expect(run.state).toBe(AuthStatusState.Ready);
    expect(els.outcomeEl.textContent).toBe("Authenticated");
  }, 5_000);
});
