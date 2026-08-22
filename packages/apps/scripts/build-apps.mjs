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

const APPS = ["pin", "vault-create", "vault-restore", "auth-sso", "vault-browser", "pin-list", "auth-status", "account-password", "account-email", "ipfs-upload", "vault-upload", "ipfs-download", "vault-download"];

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
      // Force-bundle EVERY runtime dependency. The bundle is served as a single
      // inline <script type="module"> in a sandboxed iframe that cannot resolve
      // bare file imports, so nothing may be left external. @uppy/core and
      // @uppy/xhr-upload power the upload apps' out-of-band XHR uploader; if
      // they are not inlined they'd ship as `import ... from "@uppy/core"`,
      // which the browser cannot resolve and kills the app ("Failed to resolve
      // module specifier"). Always keep this list in sync with the runtime
      // dependencies in package.json.
      alwaysBundle: [
        "@modelcontextprotocol/ext-apps",
        "@modelcontextprotocol/sdk",
        "zod",
        "robot3",
        "@uppy/core",
        "@uppy/xhr-upload",
      ],
      onlyBundle: false,
    },
  });
  console.log(`built dist/${app}.js`);
}
