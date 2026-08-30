# internal/mcp/tunnel — tunnel provider subsystem

`internal/mcp/tunnel` is a dedicated sub-package (package `tunnel`) that owns the MCP
server's tunnel domain: exposing a locally bound HTTP MCP server to the public internet
through a third-party tunnel provider. It is kept as its own package (alongside
`internal/mcp/install` and the OAuth storage migrated into `internal/mcp/auth`)
so the tunnel runtime can be reasoned about, tested, and reused in isolation from
the flat `internal/mcp` command surface.

## Scope

This package contains the **pure tunnel domain**:

- `Tunnel` interface + `tunnelBase` helpers (`tunnel.go`)
- Shared types: `TunnelConfig`, `TunnelProvider` enum (`types.go`)
- Provider runtime implementations:
  - ngrok — `ngrok.go`, `ngrok_api.go` (REST reserved-domains), `ngrok_sdk_url.go`
    (embedded-agent dev-domain resolution)
  - Cloudflare — `cloudflared.go`, `cloudflared_embedded.go`, `cloudflare_tunnel_state.go`
  - OpenAI Secure MCP Tunnel — `openai_embedded.go`, `openai_creds.go`
- Credential resolution — `tunnel_creds.go` (`ResolveCredential`, ngrok config probes)
- Provider deep-link pages — `tunnel_deeplinks.go`

## What stays in the parent (`internal/mcp`, package `mcp`)

The provider **registry** (`TunnelProviderSpec`, `TunnelFor`, `RegisterTunnelProvider`,
`providers`) lives in the parent package, NOT here. Reason: `TunnelProviderSpec`'s
`Fields`/`Finalize`/`ConfigSeeded` are typed against the parent package's wizard-facing
types (`fieldform.Prompter`, `*ServiceInstallState`, defined in
`service_install_wizard.go`), and this package must not import the parent (Go
import cycle — `internal/mcp` imports `internal/mcp/tunnel`). The parent owns the
registration `init()` (`tunnel_providers.go`), the per-provider install configurers
(`service_install_configurers.go`), and the service/install commands that consume this
package directly via `tunnel.*` (the parent no longer carries a re-export shim).

## Import constraints (must not regress)

- This package MUST NOT import `go.lumeweb.com/pinner-cli/internal/mcp` (cycle).
  Verify: `go list -deps go.lumeweb.com/pinner-cli/internal/mcp/tunnel | grep -E 'pinner-cli/internal/mcp($|/)' | grep -v 'internal/mcp/tunnel'` → must print nothing.
- Allowed imports: stdlib, urfave/cli/v3, `github.com/modelcontextprotocol/go-sdk/mcp`,
  ngrok SDK, cloudflare libs, openai tunnel-client, pterm,
  `go.lumeweb.com/pinner-cli/internal/core/config`, `internal/cli/wizard`, `internal/urlopen`.

## Boundary

Export only what the parent invokes. Today the parent reaches this package for: the
`Tunnel`/`TunnelConfig`/`TunnelProvider` types, provider constructors
(`NewNgrokTunnel`, `NewNgrokTunnelWithConfig`, `NewCloudflaredTunnel`), state load/save
(`LoadCloudflareTunnelState`, `SaveCloudflareTunnelState`), credential and deep-link
helpers (`ResolveCredential`, `PersistTunnelCredential`, `OpenTunnelDeepLink`, ...),
the embedded OpenAI runtime (`RunEmbeddedOpenAITunnel`, `ResolveOpenAICredentials`), and
ngrok account/URL resolution (`ResolveNgrokPublicURL`, `IsStableNgrokDevURL`, ...).
Parent-package callers reference these directly as `tunnel.*`; the temporary
re-export bridge the parent used during the sub-package extraction was removed.

## Testing / verification

- `go build ./...` and `go vet ./internal/mcp/...` must stay clean.
- `go test ./internal/mcp/tunnel/...` exercises the tunnel runtime in isolation.
- `go test -race ./internal/mcp ./internal/mcp/tunnel` covers the provider lifecycles.
- Run `make jsbuild cssbuild` (or `make build`) before Go build/test — the parent
  `internal/mcp` embeds generated MCP-app assets this package's tests transitively need.
