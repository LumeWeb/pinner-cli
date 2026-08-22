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
