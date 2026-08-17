package mcp

// This file is a TEMPORARY re-export shim for Stage 0 of the tunnel
// sub-package extraction. It re-exports (via type/const aliases and wrapper
// functions) every symbol that moved from package mcp into
// internal/mcp/tunnel, so existing in-package references (adapter.go,
// service_command.go, service_install_wizard.go, service_install_collect.go,
// and the parent-package tests) keep compiling until later stages rewire the
// call sites directly to tunnel.X. It is deleted in a later stage.

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// Type aliases for the symbols that moved to the tunnel sub-package.
type (
	Tunnel                = tunnel.Tunnel
	TunnelConfig          = tunnel.TunnelConfig
	TunnelProvider        = tunnel.TunnelProvider
	CloudflareTunnelState = tunnel.CloudflareTunnelState
	CloudflaredTunnel     = tunnel.CloudflaredTunnel
)

// Constants that moved with the TunnelProvider enum.
const (
	TunnelProviderOpenAI      = tunnel.TunnelProviderOpenAI
	TunnelProviderNgrok       = tunnel.TunnelProviderNgrok
	TunnelProviderCloudflared = tunnel.TunnelProviderCloudflared
)

// ResolveCredential moved to the tunnel sub-package.
func ResolveCredential(providers ...func() string) string {
	return tunnel.ResolveCredential(providers...)
}

// TunnelCfgCredential moved to the tunnel sub-package.
func TunnelCfgCredential(cfgMgr config.Manager, provider, key string) func() string {
	return tunnel.TunnelCfgCredential(cfgMgr, provider, key)
}

// PersistTunnelCredential moved to the tunnel sub-package.
func PersistTunnelCredential(cfgMgr config.Manager, provider, key, value string) {
	tunnel.PersistTunnelCredential(cfgMgr, provider, key, value)
}

// PrintTunnelDeepLink moved to the tunnel sub-package.
func PrintTunnelDeepLink(provider, missing string) {
	tunnel.PrintTunnelDeepLink(provider, missing)
}

// OpenTunnelDeepLink moved to the tunnel sub-package.
func OpenTunnelDeepLink(provider, missing string) {
	tunnel.OpenTunnelDeepLink(provider, missing)
}

// ResolveNgrokToken moved to the tunnel sub-package.
func ResolveNgrokToken(token string, cfgMgr config.Manager) string {
	return tunnel.ResolveNgrokToken(token, cfgMgr)
}

// ResolveOpenAICredentials moved to the tunnel sub-package.
func ResolveOpenAICredentials(cmd *cli.Command, cfgMgr config.Manager) (tunnelID, apiKey string) {
	return tunnel.ResolveOpenAICredentials(cmd, cfgMgr)
}

// RunEmbeddedOpenAITunnel moved to the tunnel sub-package.
func RunEmbeddedOpenAITunnel(ctx context.Context, server *mcp.Server, tunnelID, apiKey string) error {
	return tunnel.RunEmbeddedOpenAITunnel(ctx, server, tunnelID, apiKey)
}

// OpenAITunnelID moved to the tunnel sub-package (read-only var copy for the
// parent's MatchString checks; never reassigned).
var OpenAITunnelID = tunnel.OpenAITunnelID

// BareHostname moved to the tunnel sub-package.
func BareHostname(h string) string {
	return tunnel.BareHostname(h)
}

// SplitHostPort moved to the tunnel sub-package.
func SplitHostPort(addr string) (string, string, error) {
	return tunnel.SplitHostPort(addr)
}

// LocalURL moved to the tunnel sub-package.
func LocalURL(host, port string) string {
	return tunnel.LocalURL(host, port)
}

// UrlForOrigin moved to the tunnel sub-package.
func UrlForOrigin(localAddr string) (string, error) {
	return tunnel.UrlForOrigin(localAddr)
}

// NewCloudflaredTunnel moved to the tunnel sub-package.
func NewCloudflaredTunnel(cfg TunnelConfig) (Tunnel, error) {
	return tunnel.NewCloudflaredTunnel(cfg)
}

// NewNgrokTunnel moved to the tunnel sub-package.
func NewNgrokTunnel(domain, token string) Tunnel {
	return tunnel.NewNgrokTunnel(domain, token)
}

// NewNgrokTunnelWithConfig moved to the tunnel sub-package.
func NewNgrokTunnelWithConfig(domain, token string, cfgMgr config.Manager) Tunnel {
	return tunnel.NewNgrokTunnelWithConfig(domain, token, cfgMgr)
}

// ResolveNgrokPublicURL moved to the tunnel sub-package.
func ResolveNgrokPublicURL(ctx context.Context, apiKey, prefer string) (string, tunnel.NgrokAccountType, error) {
	return tunnel.ResolveNgrokPublicURL(ctx, apiKey, prefer)
}

// IsStableNgrokDevURL moved to the tunnel sub-package.
func IsStableNgrokDevURL(u string) bool {
	return tunnel.IsStableNgrokDevURL(u)
}

// LoadCloudflareTunnelState moved to the tunnel sub-package.
func LoadCloudflareTunnelState() (*CloudflareTunnelState, error) {
	return tunnel.LoadCloudflareTunnelState()
}

// SaveCloudflareTunnelState moved to the tunnel sub-package.
func SaveCloudflareTunnelState(s *CloudflareTunnelState) error {
	return tunnel.SaveCloudflareTunnelState(s)
}

// NgrokConfigAuthtoken moved to the tunnel sub-package.
func NgrokConfigAuthtoken() string {
	return tunnel.NgrokConfigAuthtoken()
}

// NgrokConfigHasAuthtoken moved to the tunnel sub-package.
func NgrokConfigHasAuthtoken() bool {
	return tunnel.NgrokConfigHasAuthtoken()
}

// HasProviderConfig moved to the tunnel sub-package.
func HasProviderConfig(provider string) bool {
	return tunnel.HasProviderConfig(provider)
}

// ResolveNgrokSDKURL moved to the tunnel sub-package. It is a package-level
// variable in tunnel, so this wrapper delegates at call time (allowing the
// parent tests to stub tunnel.ResolveNgrokSDKURL).
func ResolveNgrokSDKURL(ctx context.Context, token string) (string, error) {
	return tunnel.ResolveNgrokSDKURL(ctx, token)
}

