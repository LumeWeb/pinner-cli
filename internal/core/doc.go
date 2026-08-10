// Package core is the domain and behavior layer of pinner-cli. It is the home
// of the reusable service domains (config, vault, auth, pinning, etc.) that
// implement the business logic of the CLI.
//
// Packages under internal/core are deliberately free of CLI and MCP coupling:
// they depend on interfaces and return typed results rather than printing to an
// Output handler. This keeps the core importable by any consumer (the CLI
// command handlers, the MCP adapter, tests, or future applications) without
// pulling in presentation or transport concerns.
package core
