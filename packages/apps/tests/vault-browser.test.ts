// Behavioral tests for the vault-browser read machine (src/vault-browser.ts)
// and its DOM adapter (src/vault-browser-bootstrap.ts). Uses the same deferred
// (gated) promise pattern as the other suites to pause an `invoke` mid-flight
// and assert the intermediate state.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import { createVaultBrowserMachine, BrowserState, type VaultBrowserConfig, type VaultBrowserContext, type VaultListItem } from "@/vault-browser";
import {
  parentPath,
  joinDirPath,
  renderVaultBrowser,
  runVaultBrowserEntry,
  currentBrowserState,
  type VaultBrowserElements,
} from "@/vault-browser-bootstrap";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

const baseConfig: VaultBrowserConfig = {
  statusTool: "vault_status",
  listTool: "vault_ls",
  rootPath: "vault:/",
  loadingMsg: "Reading vault...",
  errorMsg: "Could not read vault",
  refreshLabel: "Refresh",
  upLabel: "Up",
  rootLabel: "Root",
  emptyLabel: "This vault directory is empty.",
  remoteDownMsg: "Vault index not reachable — showing local cache.",
};

function statusEnvelope(over: Record<string, unknown> = {}): ToolResult {
  return {
    structuredContent: {
      status: "ok",
      value: {
        unlocked: true,
        remote_reachable: true,
        remote_ready: true,
        storage_used: 100,
        storage_limit: 1000,
        remaining_storage: 900,
        objects_indexed: 3,
        ...over,
      },
    },
  };
}

function listEnvelope(items: VaultListItem[]): ToolResult {
  return { structuredContent: { status: "ok", value: items } };
}

interface GateEntry {
  tool: string;
  resolve: (r: ToolResult) => void;
}

describe("vault-browser machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let gate: GateEntry[];
  let machine: ReturnType<typeof createVaultBrowserMachine>;
  let service: Service<ReturnType<typeof createVaultBrowserMachine>>;
  const state = (): BrowserState => currentBrowserState(service);
  const ctx = (): VaultBrowserContext => service.context as VaultBrowserContext;

  function scriptedCall(): CallTool {
    return async (req): Promise<ToolResult> => {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };
  }

  beforeEach(() => {
    calls = [];
    gate = [];
    machine = createVaultBrowserMachine(baseConfig, scriptedCall());
    service = interpret(machine, () => {});
  });

  it("mount-loads status + listing and lands in ready", async () => {
    service.send({ type: "load", path: "vault:/" });
    // Both catalog calls are issued and parked on the gate.
    await until(service, () => gate.length === 2);
    expect(state()).toBe("loading");

    const statusG = gate.find((g) => g.tool === "vault_status");
    const listG = gate.find((g) => g.tool === "vault_ls");
    expect(statusG).toBeTruthy();
    expect(listG).toBeTruthy();
    statusG!.resolve(statusEnvelope());
    listG!.resolve(listEnvelope([{ name: "reports", type: "dir" }, { name: "notes.md", type: "file", size: 42 }]));

    await untilState(service, BrowserState.Ready);
    expect(ctx().items).toHaveLength(2);
    expect(ctx().status?.remote_reachable).toBe(true);
    expect(ctx().items[0]).toEqual({ name: "reports", type: "dir" });
  });

  it("navigating into a directory reloads at that path", async () => {
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.length === 2);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve(listEnvelope([{ name: "reports", type: "dir" }]));
    await untilState(service, BrowserState.Ready);

    // Navigate into vault:/reports: a fresh status+list load issues with that path.
    service.send({ type: "load", path: "vault:/reports" });
    await until(service, () => gate.filter((g) => g.tool === "vault_ls").length === 2);
    const lsCalls = calls.filter((c) => c.name === "vault_ls");
    expect(lsCalls[lsCalls.length - 1].arguments).toEqual({ path: "vault:/reports" });
    // Resolve BOTH of the navigate's fresh calls (status + list) so the load settles.
    const statusCalls = gate.filter((g) => g.tool === "vault_status");
    statusCalls[statusCalls.length - 1].resolve(statusEnvelope());
    const listCalls = gate.filter((g) => g.tool === "vault_ls");
    listCalls[listCalls.length - 1].resolve(listEnvelope([{ name: "a.txt", type: "file", size: 7 }]));
    await untilState(service, BrowserState.Ready);
    expect(ctx().path).toBe("vault:/reports");
  });

  it("a failing list call lands in error, and retry recovers", async () => {
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.length === 2);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve({ isError: true, error: "boom" } as ToolResult);

    await untilState(service, BrowserState.Error);
    expect(ctx().errorMsg).toContain("Could not read vault");
    expect(ctx().items).toHaveLength(0);

    // Retry the same path succeeds.
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.filter((g) => g.tool === "vault_ls").length === 2);
    // Resolve the LATEST status + list calls (the retry's, not the first run's).
    const statusCalls = gate.filter((g) => g.tool === "vault_status");
    statusCalls[statusCalls.length - 1].resolve(statusEnvelope());
    const listCalls = gate.filter((g) => g.tool === "vault_ls");
    listCalls[listCalls.length - 1].resolve(listEnvelope([{ name: "x", type: "file", size: 1 }]));
    await untilState(service, BrowserState.Ready);
    expect(ctx().errorMsg).toBe("");
    expect(ctx().items).toHaveLength(1);
  });

  it("a server error result (isError with structuredContent.error, no top-level error) lands in error", async () => {
    // Regression: real server/SDK error results flag isError and carry the
    // message inside structuredContent ({status:"error",error:<code>}), with no
    // top-level .error string. The machine must treat them as failures rather
    // than silently rendering an empty directory.
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.length === 2);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve({
      isError: true,
      structuredContent: { status: "error", error: "profile not unlocked" },
    } as ToolResult);

    await untilState(service, BrowserState.Error);
    expect(ctx().errorMsg).toContain("profile not unlocked");
    expect(ctx().items).toHaveLength(0);
  });

  it("a bare isError result with no message falls back to a generic error", async () => {
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.length === 2);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve({ isError: true } as ToolResult);

    await untilState(service, BrowserState.Error);
    expect(ctx().errorMsg).toContain("vault operation failed");
    expect(ctx().items).toHaveLength(0);
  });

  it("empty directory surfaces the empty label on the ready readout", async () => {
    service.send({ type: "load", path: "vault:/" });
    await until(service, () => gate.length === 2);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve(listEnvelope([]));
    await untilState(service, BrowserState.Ready);
    expect(ctx().items).toHaveLength(0);
    const r = renderVaultBrowser(BrowserState.Ready, ctx(), baseConfig);
    expect(r.empty).toBe(true);
    expect(r.statusMsg).toBe(baseConfig.emptyLabel);
  });
});

describe("vault-browser render", () => {
  it("remote-down clears the ok state and warns on the status line", () => {
    const r = renderVaultBrowser(
      BrowserState.Ready,
      { path: "vault:/", status: { remote_reachable: false }, items: [{ name: "a", type: "file" }], errorMsg: "" },
      baseConfig,
    );
    expect(r.statusState).toBe("info");
    expect(r.statusMsg).toBe(baseConfig.remoteDownMsg);
  });

  it("loading is busy with the pending message", () => {
    const r = renderVaultBrowser(BrowserState.Loading, { path: "vault:/", status: null, items: [], errorMsg: "" }, baseConfig);
    expect(r.busy).toBe(true);
    expect(r.statusState).toBe("pending");
    expect(r.statusMsg).toBe(baseConfig.loadingMsg);
  });

  it("error surfaces the stored error text", () => {
    const r = renderVaultBrowser(BrowserState.Error, { path: "vault:/", status: null, items: [], errorMsg: "Could not read vault: boom" }, baseConfig);
    expect(r.statusState).toBe("error");
    expect(r.statusMsg).toBe("Could not read vault: boom");
  });
});

describe("parentPath", () => {
  it("walks up and stops at the root", () => {
    expect(parentPath("vault:/reports/inner")).toBe("vault:/reports");
    expect(parentPath("vault:/reports")).toBe("vault:/");
    expect(parentPath("vault:/")).toBe("vault:/");
  });

  it("preserves an explicit profile authority", () => {
    expect(parentPath("vault://work/a")).toBe("vault://work/");
    expect(parentPath("vault://work/docs/a")).toBe("vault://work/docs/");
  });

  it("normalizes trailing slashes like the Go parser", () => {
    expect(parentPath("vault:/reports/inner/")).toBe("vault:/reports");
  });
});

describe("joinDirPath", () => {
  it("joins a child onto the root and nested paths", () => {
    expect(joinDirPath("vault:/", "docs")).toBe("vault:/docs");
    expect(joinDirPath("vault:/docs", "media")).toBe("vault:/docs/media");
  });

  it("does not duplicate a trailing slash", () => {
    expect(joinDirPath("vault:/docs/", "media")).toBe("vault:/docs/media");
  });

  it("preserves an explicit profile authority", () => {
    expect(joinDirPath("vault://work/docs/", "media")).toBe("vault://work/docs/media");
    expect(joinDirPath("vault://work", "media")).toBe("vault://work/media");
  });
});

describe("runVaultBrowserEntry", () => {
  // Minimal element harness: DOM-free stand-ins that record what the adapter
  // would write, so we can drive it with a programmatic load() and callTool.
  function makeElements(rows: { name: string; type: string }[]) {
    const addClick: Array<(fn: () => void) => void> = [];
    const el = <T extends object>(over: T, isList = false) => {
      if (isList) {
        return { replaceChildren: (...n: unknown[]) => {} , ...over } as unknown as VaultBrowserElements["listEl"];
      }
      return { disabled: false, addEventListener: (t: string, fn: () => void) => { if (t === "click") addClick.push(fn); } } as unknown as T & { addEventListener(t: string, fn: () => void): void; disabled: boolean };
    };
    return {
      els: {
        statusEl: el({ textContent: "" }) as unknown as HTMLElement,
        pathEl: el({ textContent: "" }) as unknown as HTMLElement,
        listEl: { replaceChildren: (...n: unknown[]) => {} },
        emptyEl: el({ hidden: true }) as unknown as HTMLElement,
        upBtn: el({ disabled: true }),
        rootBtn: el({ disabled: true }),
        refreshBtn: el({ disabled: true }),
      },
      addClick,
    };
  }

  it("auto-loads on mount and exposes load()/state()", async () => {
    const gate: GateEntry[] = [];
    const callTool: CallTool = async (req) =>
      await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    const h = makeElements([]);
    const run = runVaultBrowserEntry({
      config: baseConfig,
      callTool,
      elements: h.els as unknown as VaultBrowserElements,
    });

    // Mount issued status + list for the root.
    await until({ machine: { current: "" } }, () => gate.length === 2, 2_000);
    expect(run.state).toBe(BrowserState.Loading);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve(listEnvelope([{ name: "d", type: "dir" }]));
    await untilState(run.service, BrowserState.Ready);
    expect(run.state).toBe(BrowserState.Ready);
  }, 5_000);

  it("wires a click handler on dir rows that loads into that directory", async () => {
    // Regression: dir rows must be clickable so the human can drill into a
    // directory. Rendered dir rows get a click listener that issues load() with
    // the child path joined onto the currently listed path.
    const gate: GateEntry[] = [];
    const calls: { name: string; arguments: Record<string, unknown> }[] = [];
    const callTool: CallTool = async (req) => {
      calls.push(req);
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };

    // Fake row element capturing click handlers; listEl captures rendered rows.
    const clickHandlers: Record<string, (() => void) | undefined> = {};
    const makeRow = (name: string) => ({
      name,
      addEventListener: (t: string, fn: () => void) => {
        if (t === "click") clickHandlers[name] = fn;
      },
    });
    let renderedRows: unknown[] = [];
    const elements = {
      statusEl: { textContent: "" } as unknown as HTMLElement,
      pathEl: { textContent: "" } as unknown as HTMLElement,
      listEl: { replaceChildren: (...n: unknown[]) => { renderedRows = n; } },
      emptyEl: { textContent: "", hidden: true } as unknown as HTMLElement,
      upBtn: { disabled: true, addEventListener: () => {} },
      rootBtn: { disabled: true, addEventListener: () => {} },
      refreshBtn: { disabled: true, addEventListener: () => {} },
      createRow: (item: VaultListItem) => makeRow(item.name),
    } as unknown as VaultBrowserElements;

    const run = runVaultBrowserEntry({ config: baseConfig, callTool, elements });

    await until({ machine: { current: "" } }, () => gate.length === 2, 2_000);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve(
      listEnvelope([{ name: "reports", type: "dir" }, { name: "notes.md", type: "file", size: 42 }]),
    );
    await untilState(run.service, BrowserState.Ready);

    // A dir row was rendered and is clickable; a file row is not.
    expect(renderedRows.map((r) => (r as { name: string }).name)).toEqual(["reports", "notes.md"]);
    expect(typeof clickHandlers["reports"]).toBe("function");
    expect(clickHandlers["notes.md"]).toBeUndefined();

    // Clicking the dir row issues a fresh vault_ls load at the joined child path.
    clickHandlers["reports"]!();
    await until({ machine: { current: "" } }, () => gate.filter((g) => g.tool === "vault_ls").length === 2, 2_000);
    const lsCalls = calls.filter((c) => c.name === "vault_ls");
    expect(lsCalls[lsCalls.length - 1].arguments).toEqual({ path: "vault:/reports" });

    // Resolve the navigate's fresh status + list calls so the load settles.
    const statusCalls = gate.filter((g) => g.tool === "vault_status");
    statusCalls[statusCalls.length - 1].resolve(statusEnvelope());
    const listCalls = gate.filter((g) => g.tool === "vault_ls");
    listCalls[listCalls.length - 1].resolve(listEnvelope([{ name: "inner", type: "dir" }]));
    await untilState(run.service, BrowserState.Ready);
    expect((elements.pathEl as { textContent: string }).textContent).toBe("vault:/reports");
  }, 5_000);

  it("reveals the empty placeholder (hidden=false) when a directory is empty", async () => {
    const gate: GateEntry[] = [];
    const calls: { name: string; arguments: Record<string, unknown> }[] = [];
    const callTool: CallTool = async (req) => {
      calls.push({ name: req.name, arguments: req.arguments });
      return await new Promise<ToolResult>((resolve) => gate.push({ tool: req.name, resolve }));
    };
    const elements = {
      statusEl: { textContent: "" } as unknown as HTMLElement,
      pathEl: { textContent: "" } as unknown as HTMLElement,
      listEl: { replaceChildren: () => {} },
      emptyEl: { textContent: "", hidden: true } as unknown as HTMLElement,
      upBtn: { disabled: true, addEventListener: () => {} },
      rootBtn: { disabled: true, addEventListener: () => {} },
      refreshBtn: { disabled: true, addEventListener: () => {} },
    } as unknown as VaultBrowserElements;

    const run = runVaultBrowserEntry({ config: baseConfig, callTool, elements });

    await until({ machine: { current: "" } }, () => gate.length === 2, 2_000);
    gate.find((g) => g.tool === "vault_status")!.resolve(statusEnvelope());
    gate.find((g) => g.tool === "vault_ls")!.resolve(listEnvelope([]));
    await untilState(run.service, BrowserState.Ready);

    expect((elements.emptyEl as { hidden: boolean }).hidden).toBe(false);
  }, 5_000);
});
