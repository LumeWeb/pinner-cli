// Shared app version. The Go renderer prefixes each app module with a
// `window.__PINNER_CLI_VERSION__ = "<version>"` assignment (see
// internal/mcpapp AppVersionGlobal), sourced from the CLI's build version
// (build/build.go, stamped by ldflags). Apps advertise this as their version
// during the ui/initialize handshake instead of carrying a hardcoded per-app
// version. When running outside the rendered doc (e.g. vitest), fall back to a
// dev marker.
export const APP_VERSION: string =
  (globalThis as { __PINNER_CLI_VERSION__?: string }).__PINNER_CLI_VERSION__ ?? "dev";
