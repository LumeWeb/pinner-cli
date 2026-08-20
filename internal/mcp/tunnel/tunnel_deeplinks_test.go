package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

func TestTunnelDeepLink(t *testing.T) {
	cases := []struct {
		provider string
		missing  string
		want     string
	}{
		{"ngrok", "authtoken", "https://dashboard.ngrok.com/get-started/your-authtoken"},
		{"ngrok", "domain", "https://dashboard.ngrok.com/domains"},
		{"ngrok", "account", "https://dashboard.ngrok.com/signup"},
		{"openai", "tunnel_id", "https://platform.openai.com/settings/organization/tunnels"},
		{"openai", "api_key", "https://platform.openai.com/settings/organization/api-keys"},
		{"openai", "connector", "https://chatgpt.com/#settings/Connectors"},
		// Unknown pairs have no deep link.
		{"ngrok", "bogus", ""},
		{"openai", "bogus", ""},
		{"cloudflared", "authtoken", ""},
		{"bogus", "authtoken", ""},
	}
	for _, c := range cases {
		t.Run(c.provider+"/"+c.missing, func(t *testing.T) {
			assert.Equal(t, c.want, tunnelDeepLink(c.provider, c.missing))
		})
	}
}

func TestOpenTunnelDeepLinkOpensInInteractiveMode(t *testing.T) {
	oldNonInteractive := fieldform.NonInteractive
	defer func() { fieldform.NonInteractive = oldNonInteractive }()
	fieldform.NonInteractive = false

	origOpener := TunnelDeepLinkOpener
	defer func() { TunnelDeepLinkOpener = origOpener }()

	var opened string
	TunnelDeepLinkOpener = func(u string) error {
		opened = u
		return nil
	}

	OpenTunnelDeepLink("ngrok", "authtoken")
	assert.Equal(t, "https://dashboard.ngrok.com/get-started/your-authtoken", opened)
}

func TestOpenTunnelDeepLinkDoesNotOpenInNonInteractive(t *testing.T) {
	oldNonInteractive := fieldform.NonInteractive
	defer func() { fieldform.NonInteractive = oldNonInteractive }()
	fieldform.NonInteractive = true

	origOpener := TunnelDeepLinkOpener
	defer func() { TunnelDeepLinkOpener = origOpener }()

	opened := false
	TunnelDeepLinkOpener = func(u string) error {
		opened = true
		return nil
	}

	OpenTunnelDeepLink("openai", "tunnel_id")
	require.False(t, opened, "non-interactive mode must not spawn a browser")
}

func TestOpenTunnelDeepLinkUnknownPairIsNoop(t *testing.T) {
	oldNonInteractive := fieldform.NonInteractive
	defer func() { fieldform.NonInteractive = oldNonInteractive }()
	fieldform.NonInteractive = false

	origOpener := TunnelDeepLinkOpener
	defer func() { TunnelDeepLinkOpener = origOpener }()

	opened := false
	TunnelDeepLinkOpener = func(u string) error {
		opened = true
		return nil
	}

	OpenTunnelDeepLink("cloudflared", "authtoken")
	require.False(t, opened, "unknown pair should not open anything")
}
