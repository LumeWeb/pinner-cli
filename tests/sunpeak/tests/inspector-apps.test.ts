import { test, expect } from 'sunpeak/test';
import type { FrameLocator } from '@playwright/test';

/**
 * The upload apps (Upload to IPFS / Upload to Vault) ship Uppy's XHR uploader
 * inlined into a single self-contained ESM bundle. The sandboxed ui:// iframe
 * serves each bundle as one inline <script type="module"> with no importer, so
 * NO bare module specifier may survive the build — a leaked `import ... from
 * "@uppy/core"` throws "Failed to resolve module specifier" and kills the app
 * before it boots (a real regression that hit Claude's host). These tests
 * render the upload apps in the real browser and assert the module actually
 * EXECUTED: the shell injects `window.__PINNER_CLI_VERSION__` as the first
 * statement of the same module script, and ES module instantiation resolves
 * every import before running any statement. If an import could not resolve,
 * the version global would never be set and the app body stays inert — which
 * is exactly the failure mode we guard against.
 */

/** The version global the shell injects as the first statement of each app's module. */
const VERSION_GLOBAL = '__PINNER_CLI_VERSION__';

// bootedVersion reads the version global in the app (sandboxed iframe) window,
// evaluating inside that frame. A non-empty string proves the module graph
// instantiated (all imports resolved) and then executed its first statement.
async function appVersion(app: FrameLocator): Promise<unknown> {
  return app
    .locator('body')
    .evaluate((el, globalName) => {
      const doc = el.ownerDocument;
      const win = doc?.defaultView as (Window & Record<string, unknown>) | null;
      return win ? win[globalName] : undefined;
    }, VERSION_GLOBAL);
}

/**
 * Host-iframe rendering of the pinner ui:// MCP Apps via the sunpeak
 * `inspector` fixture. THIS is where the real browser (chromium) runs: each
 * app is loaded into a simulated ChatGPT / Claude host and rendered inside the
 * double-iframe sandbox, exactly as an end user's host would render it.
 *
 * The `mcp` fixture (see protocol-surface.test.ts) talks raw protocol and never
 * opens a tab; these tests prove the runtime actually renders our apps.
 *
 * We use the auth-flow apps (auth_sso) which render a deterministic shell
 * without requiring a valid authenticated Pinner session, so the assertions are
 * stable and account-free.
 */

test('auth_sso app renders its sign-in view inside the host iframe', async ({ inspector }) => {
  const result = await inspector.renderTool('auth_sso', {});
  // result.app() locates through the double iframe to the app body.
  const app = result.app();
  const body = await app.locator('body').innerText();
  expect(body).toContain('Sign In');
});

// assertUploadAppBoots renders an upload tool's ui:// view and verifies the app
// actually booted: the static HTML shell is server-rendered (it would render
// even if the module failed), so the real signal is that the inline module ran
// (window.__PINNER_CLI_VERSION__ is set). A leaked bare import — e.g. Uppy
// left external as `import ... from "@uppy/core"` — fails module instantiation,
// leaving the global unset and the app's wiring inert.
async function assertUploadAppBoots(tool: string, heading: string, inspector: { renderTool: (n: string, i: unknown) => Promise<{ app(): FrameLocator }> }) {
  const result = await inspector.renderTool(tool, {});
  const app = result.app();

  // The static shell still renders its heading even when the module is broken,
  // so presence alone is necessary but not sufficient.
  const body = await app.locator('body').innerText();
  expect(body).toContain(heading);

  // The module must have instantiated and executed. This is the assertion that
  // catches an unresolved bare import (the @uppy/core regression).
  const version = await appVersion(app);
  expect(version).toEqual(expect.stringMatching(/^\d+\.\d+\.\d+/));
}

test('upload_file app boots (ipfs-upload bundle has no unmet imports)', async ({ inspector }) => {
  await assertUploadAppBoots('upload_file', 'Upload to IPFS', inspector);
});

test('vault_put_file app boots (vault-upload bundle has no unmet imports)', async ({ inspector }) => {
  await assertUploadAppBoots('vault_put_file', 'Upload to Vault', inspector);
});

/**
 * The upload forms use a styled composite file picker: the native
 * <input type="file"> is pinned invisible under a "Choose file" button + a
 * picked-file label, because the browser's native "Choose file/No file chosen"
 * chrome cannot be themed. Verify the chrome renders and that picking a file
 * updates the label (the bootstraps wire a change listener for that).
 */
async function assertStyledFilePicker(
  tool: string,
  fileId: string,
  labelId: string,
  inspector: { renderTool: (n: string, i: unknown) => Promise<{ app(): FrameLocator }> },
) {
  const result = await inspector.renderTool(tool, {});
  const app = result.app();
  const body = await app.locator('body').innerText();
  expect(body).toContain('Choose file');
  expect(body).toContain('No file chosen');
  expect(body).toContain('Upload');

  // Actually select a file through the hidden native input and assert the
  // picker label adopts the filename (proves the change listener fired and
  // the machine is wired, not just the static HTML shell).
  const fileInput = app.locator(`#${fileId}`);
  await fileInput.setInputFiles({
    name: 'report.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.from('pdf-bytes'),
  });
  await expect(app.locator(`#${labelId}`)).toHaveText('report.pdf');
}

test('upload_file app renders the styled file picker and reflects the picked name', async ({ inspector }) => {
  await assertStyledFilePicker('upload_file', 'file', 'file-name', inspector);
});

test('vault_put_file app renders the styled file picker and reflects the picked name', async ({ inspector }) => {
  await assertStyledFilePicker('vault_put_file', 'vfile', 'vfile-name', inspector);
});

/**
 * BROWSER regression test for the presigned-upload CORS bug.
 *
 * An MCP host renders the "Upload to IPFS" app inside a sandboxed iframe whose
 * origin — without `allow-same-origin` — is the OPAQUE origin, which a browser
 * serializes to the literal string `"null"` in the Origin header. The app's
 * Uppy XHR uploader PUTs the raw file bytes to the minted presigned
 * `/upload/<token>` endpoint from that `"null"` origin, so the server MUST
 * reflect `Access-Control-Allow-Origin` for it or the browser blocks the upload
 * with "No 'Access-Control-Allow-Origin' header" (the exact error we hit).
 *
 * This test reproduces that real-browser condition exactly:
 *   1. Mint a real one-time presigned endpoint through the `ipfs_upload_submit`
 *      helper (the same one the app calls).
 *   2. Issue a cross-origin PUT to it from a browser iframe sandboxed WITHOUT
 *      `allow-same-origin` — so it genuinely has an opaque `"null"` origin,
 *      mirroring the host-rendered app frame.
 *   3. Assert the fetch resolves (CORS granted) with 202.
 *      Arbitrary-origin refusal is asserted at the HTTP layer in Go
 *      (TestCORSOriginOpaqueNull / TestIPFSUploadCORSOpaqueNull), because the
 *      sunpeak inspector serves the whole page from an opaque "null" origin —
 *      there is no way to originate a non-null attacker request here.
 */

type McpFixture = { callTool: (name: string, input?: Record<string, unknown>) => Promise<any> };

// presignedUrlOf extracts the minted presigned PUT url from an
// ipfs_upload_submit result (present in structuredContent or a JSON text block).
function presignedUrlOf(result: any): string {
  const url =
    result?.structuredContent?.url ??
    result?.content
      ?.flatMap((c: any) => (c?.text ? [c.text] : []))
      .join('')
      .match(/"url"\s*:\s*"([^"]+)"/)?.[1];
  if (!url) throw new Error('mint result had no presigned url: ' + JSON.stringify(result));
  return url;
}

test('cross-origin presigned upload PUT is allowed from the opaque "null" sandbox origin', async ({
  mcp,
  page,
}: {
  mcp: McpFixture;
  page: import('@playwright/test').Page;
}) => {
  const mint = await mcp.callTool('ipfs_upload_submit', { name: 'cors-opaque.bin' });
  const url = presignedUrlOf(mint);

  // Positive: an opaque-origin iframe (sandbox without allow-same-origin) can
  // cross-origin PUT the minted endpoint. If the CORS fix regresses, this
  // fetch rejects with a TypeError and the test fails.
  const positive = await page.evaluate(async (presignedUrl) => {
    return await new Promise<{ __t: string; ok: boolean; status?: number; error?: string }>((resolve) => {
      const iframe = document.createElement('iframe');
      iframe.style.display = 'none';
      // No allow-same-origin => the frame's origin is opaque (serialized "null").
      iframe.setAttribute('sandbox', 'allow-scripts');
      const html =
        '<!DOCTYPE html><script>(async () => {' +
        '  try {' +
        `    const res = await fetch(${JSON.stringify(presignedUrl)}, {` +
        "      method: 'PUT', mode: 'cors'," +
        "      headers: { 'Content-Type': 'application/octet-stream' }," +
        "      body: new Blob(['opaque-origin-bytes'])," +
        '    });' +
        "    parent.postMessage({ __t: 'cors', ok: res.ok, status: res.status }, '*');" +
        '  } catch (e) {' +
        "    parent.postMessage({ __t: 'cors', ok: false, error: String((e && e.message) || e) }, '*');" +
        '  }' +
        '})();</scr' + 'ipt>';
      iframe.srcdoc = html;
      const onMsg = (e: MessageEvent) => {
        if (e.data && e.data.__t === 'cors') done(e.data);
      };
      let timer: ReturnType<typeof setTimeout>;
      const done = (msg: { __t: string; ok: boolean; status?: number; error?: string }) => {
        window.removeEventListener('message', onMsg);
        clearTimeout(timer);
        resolve(msg);
      };
      window.addEventListener('message', onMsg);
      timer = setTimeout(() => done({ __t: 'cors', ok: false, error: 'timeout' }), 15000);
      document.body.appendChild(iframe);
    });
  }, url);

  expect(positive.ok, `opaque-origin PUT failed: ${positive.error ?? `status ${positive.status}`}`).toBe(true);
  expect(positive.status).toBe(202);

  // Arbitrary-origin refusal is asserted precisely at the HTTP layer in the Go
  // tests (TestCORSOriginOpaqueNull / TestIPFSUploadCORSOpaqueNull refuse an
  // `https://evil.example.com` origin), not here: the sunpeak inspector serves
  // the whole page from an opaque "null" origin, so there is no way to originate
  // a genuinely non-null, non-trusted request from this environment.
});
