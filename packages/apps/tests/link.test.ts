// Behavioral tests for the robot3 one-shot link machine (link.ts), used by the
// synchronous account password/email change apps. Uses deferred (gated)
// promises to pause an `invoke` mid-flight, per the robot3 pattern.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import { createLinkMachine, currentLinkState, LinkState, renderLink, type CallTool, type LinkConfig, type ToolResult } from "@/link";
import { until, untilState } from "./helpers";

const baseConfig: LinkConfig = {
  startTool: "account_password_update",
  urlField: "action_url",
  startLabel: "minting...",
  openLabel: "Open page",
  startErrorMsg: "start failed",
  noUrlMsg: "no page returned",
  alreadyDoneMsg: "sign in first",
  doneMsg: "done",
};

function linkResult(url?: string, reason?: string): ToolResult {
  const sc: Record<string, unknown> = {};
  if (url) sc.action_url = url;
  if (reason) sc.reason = reason;
  return { structuredContent: sc };
}

describe("link machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let callTool: CallTool;
  let machine: ReturnType<typeof createLinkMachine>;
  let service: Service<ReturnType<typeof createLinkMachine>>;
  const state = (): LinkState => currentLinkState(service);

  beforeEach(() => {
    calls = [];
    callTool = async () => ({ structuredContent: {} });
    machine = createLinkMachine(baseConfig, callTool);
    service = interpret(machine, () => {});
  });

  it("idle → starting → ok with the minted URL, no polling", async () => {
    const gate: { resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(req: { name: string; arguments: Record<string, unknown> }): Promise<ToolResult> {
      calls.push(req);
      expect(req.name).toBe("account_password_update");
      return await new Promise<ToolResult>((resolve) => gate.push({ resolve }));
    }
    machine = createLinkMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await untilState(service, LinkState.Starting);
    expect(state()).toBe(LinkState.Starting);

    gate[0].resolve(linkResult("https://x.test/account/abc123"));
    await untilState(service, LinkState.Ok);
    expect(state()).toBe(LinkState.Ok);
    expect((service.context as any).url).toBe("https://x.test/account/abc123");
    expect(calls.length).toBe(1); // one-shot: no status call
  });

  it("renders a sign-in-first message when the start tool steers elsewhere (reason, no URL)", async () => {
    const gate: { resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(): Promise<ToolResult> {
      return await new Promise<ToolResult>((resolve) => gate.push({ resolve }));
    }
    machine = createLinkMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await untilState(service, LinkState.Starting);

    // Steer-to-sign-in: needs_human with reason but no action_url -> alreadyDone.
    gate[0].resolve(linkResult(undefined, "sso_approval"));
    await untilState(service, LinkState.Norl);
    expect(state()).toBe(LinkState.Norl);
    const r = renderLink(currentLinkState(service), service.context as any, baseConfig);
    expect(r.status).toBe("error");
    expect(r.message).toBe(baseConfig.alreadyDoneMsg);
  });

  it("renders noUrlMsg when the start tool mints nothing and no steer reason", async () => {
    const gate: { resolve: (r: ToolResult) => void }[] = [];
    async function scriptedCall(): Promise<ToolResult> {
      return await new Promise<ToolResult>((resolve) => gate.push({ resolve }));
    }
    machine = createLinkMachine(baseConfig, scriptedCall);
    service = interpret(machine, () => {});

    service.send("start");
    await untilState(service, LinkState.Starting);
    gate[0].resolve({ structuredContent: {} });
    await untilState(service, LinkState.Norl);
    const r = renderLink(currentLinkState(service), service.context as any, baseConfig);
    expect(r.status).toBe("error");
    expect(r.message).toBe(baseConfig.noUrlMsg);
  });

  it("renders done/start-free readout in ok state", async () => {
    // Direct render assertion on a synthetic ok context.
    const r = renderLink(LinkState.Ok, { url: "https://x.test/a" }, baseConfig);
    expect(r.status).toBe("ok");
    expect(r.message).toBe(baseConfig.doneMsg);
    expect(r.pending).toBe(false);
  });
});
