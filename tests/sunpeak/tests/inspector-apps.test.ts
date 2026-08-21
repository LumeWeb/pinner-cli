import { test, expect } from 'sunpeak/test';

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
