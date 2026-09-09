//go:build !no_tunnel

package tunnel

import (
	ngrok "go.lumeweb.com/tunneler/ngrok"

	"go.lumeweb.com/pinner/core/config"
)

// NewNgrokTunnel returns a tunnel powered by the embedded ngrok SDK. token is
// the account authtoken (may be empty if already configured via
// `ngrok config add-authtoken`, the NGROK_AUTHTOKEN environment variable, or
// the pinner config manager). domain, when set, is a custom hostname; ngrok
// requires a paid account for custom domains.
//
// It delegates to the extracted tunneler ngrok provider.
func NewNgrokTunnel(domain, token string) Tunnel {
	return ngrok.NewNgrokTunnel(domain, token)
}

// NewNgrokTunnelWithConfig returns an ngrok tunnel that consults cfgMgr (when
// non-nil) as the last-resort credential store for the ngrok authtoken. The
// manager is adapted to the tunneler library's TunnelCredentialStore.
func NewNgrokTunnelWithConfig(domain, token string, cfgMgr config.Manager) Tunnel {
	return ngrok.NewNgrokTunnelWithStore(domain, token, storeFrom(cfgMgr))
}
