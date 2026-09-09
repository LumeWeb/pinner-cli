// Shared app version. The renderer prefixes each app module with a
// `window.__MCPCANVAS_VERSION__ = "<version>"` assignment
// (mcpcanvas.VersionGlobal, threaded through internal/mcpapp's render
// delegate from the CLI's build version — build/build.go, stamped by
// ldflags; non-semver values such as "develop" are normalized to "1.0.0").
// Apps advertise this as their version during the ui/initialize handshake
// instead of carrying a hardcoded per-app version. When running outside the
// rendered doc (e.g. vitest), fall back to a dev marker.
export const APP_VERSION: string =
  (globalThis as { __MCPCANVAS_VERSION__?: string }).__MCPCANVAS_VERSION__ ?? "dev";
