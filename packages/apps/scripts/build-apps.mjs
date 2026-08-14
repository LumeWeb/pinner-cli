// Production build: one fully self-contained ESM bundle per MCP app.
//
// Each app bundle must be usable as the body of a single inline <script
// type="module"> in the sandboxed ui:// iframe, which cannot resolve file
// imports. tsdown's multi-entry mode code-splits the shared ext-apps client
// into a separate chunk (breaking self-containment), so we build each entry
// on its own: its entire dependency tree — @modelcontextprotocol/ext-apps
// (App + PostMessageTransport + MCP SDK + zod), robot3, and the app logic —
// is inlined into one file with zero imports.
//
// Output: dist/<app>.js for pin, vault-create, vault-restore, auth-sso.
import { build } from "tsdown";

const APPS = ["pin", "vault-create", "vault-restore", "auth-sso", "vault-browser", "pin-list"];

for (const app of APPS) {
  await build({
    entry: { [app]: `./src/entries/${app}.ts` },
    format: ["esm"],
    platform: "browser",
    target: "es2022",
    minify: true,
    sourcemap: false,
    clean: false, // don't wipe other apps' outputs
    outDir: "dist",
    deps: {
      alwaysBundle: ["@modelcontextprotocol/ext-apps", "@modelcontextprotocol/sdk", "zod", "robot3"],
      onlyBundle: false,
    },
  });
  console.log(`built dist/${app}.js`);
}
