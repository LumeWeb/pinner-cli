// Behavioral tests for the robot3 vault-upload machine (src/vault-upload.ts).
// Uses the same deferred (gated) promise pattern as tests/ipfs-upload.test.ts
// to pause an `invoke` mid-flight and assert intermediate states: mint the
// presigned endpoint -> Uppy XHR -> ok/error. There is no poll loop (the vault
// write is synchronous), and file bytes never appear in any callTool argument;
// only the minted URL crosses the tool channel.

import { beforeEach, describe, expect, it } from "vitest";
import { interpret, type Service } from "robot3";
import {
  createVaultUploadMachine,
  currentVaultUploadState,
  type VaultUploadConfig,
  type VaultUploadContext,
  type VaultUploadXhr,
  VaultUploadState,
} from "@/vault-upload";
import type { CallTool, ToolResult } from "@/flow";
import { until, untilState } from "./helpers";

const baseConfig: VaultUploadConfig = {
  submitTool: "vault_upload_submit",
  noFileMsg: "Select a file to upload.",
  noPathMsg: "Enter a vault destination path.",
  mintingMsg: "Preparing upload...",
  uploadingMsg: "Uploading...",
  uploadedMsg: "Stored in the vault.",
  failedMsg: "Upload failed.",
};

function mintOk(url: string): ToolResult {
  return { structuredContent: { url } };
}

interface GateItem {
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
}

describe("vault-upload machine", () => {
  let calls: { name: string; arguments: Record<string, unknown> }[];
  let xhrCalls: { url: string; file: File }[];
  let gate: GateItem[];
  let machine: ReturnType<typeof createVaultUploadMachine>;
  let service: Service<ReturnType<typeof createVaultUploadMachine>>;
  const state = (): VaultUploadState => currentVaultUploadState(service);
  const ctx = () => service.context as VaultUploadContext;

  function scriptedCallTool(): CallTool {
    return (req) =>
      new Promise<ToolResult>((resolve, reject) => {
        calls.push(req);
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }
  function scriptedUploadXhr(): VaultUploadXhr {
    return (url, file) =>
      new Promise<{ vaultPath: string }>((resolve, reject) => {
        xhrCalls.push({ url, file });
        gate.push({ resolve: resolve as (v: unknown) => void, reject });
      });
  }

  beforeEach(() => {
    calls = [];
    xhrCalls = [];
    gate = [];
    machine = createVaultUploadMachine(baseConfig, scriptedCallTool(), scriptedUploadXhr());
    service = interpret(machine, () => {});
  });

  function start() {
    service.send({ type: "start", file: new File(["hello"], "a.txt"), vaultPath: "vault:/docs/a.txt" });
  }

  it("start → mint → xhr → ok with stored path; only a URL crosses the tool channel", async () => {
    start();
    await untilState(service, VaultUploadState.Minting);

    // Only the mint tool called; no file bytes in arguments.
    expect(calls.map((c) => c.name)).toEqual(["vault_upload_submit"]);
    expect(calls[0].arguments).toEqual({ vault_path: "vault:/docs/a.txt" });
    expect(JSON.stringify(calls[0].arguments)).not.toContain("hello");

    gate[0].resolve(mintOk("http://host/vault-upload/tok"));
    await untilState(service, VaultUploadState.Uploading);

    // XHR must be invoked with the minted URL and the real File.
    expect(xhrCalls.length).toBe(1);
    expect(xhrCalls[0].url).toBe("http://host/vault-upload/tok");
    expect(xhrCalls[0].file.name).toBe("a.txt");

    gate[1].resolve({ vaultPath: "vault:/docs/a.txt" });
    await untilState(service, VaultUploadState.Ok);
    expect(ctx().outPath).toBe("vault:/docs/a.txt");
  });

  it("mint isError → error terminal without XHR", async () => {
    start();
    await untilState(service, VaultUploadState.Minting);
    gate[0].resolve({ isError: true, structuredContent: { error: "no endpoint" } });
    await untilState(service, VaultUploadState.Error);
    expect(xhrCalls.length).toBe(0);
  });

  it("XHR rejection → error terminal", async () => {
    start();
    await untilState(service, VaultUploadState.Minting);
    gate[0].resolve(mintOk("http://host/vault-upload/tok"));
    await untilState(service, VaultUploadState.Uploading);
    gate[1].reject(new Error("network down"));
    await untilState(service, VaultUploadState.Error);
  });
});
