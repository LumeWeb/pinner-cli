package mcp

import (
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// resolveOpenAICredentials returns the OpenAI Secure MCP Tunnel ID and
// control-plane API key, auto-detected across flag, env var, and (last resort)
// the pinner config manager. The user is only asked for a value when none of
// the sources provides it (that prompting happens in the wizard); this function
// is the read-only detection chain used at runtime.
//
// Detection order for the tunnel ID:
//
//	--tunnel-id / MCP_TUNNEL_ID
//	-> CONTROL_PLANE_TUNNEL_ID        (provider-standard env var)
//	-> config manager openai.tunnel_id (last-resort store)
//
// Detection order for the API key:
//
//	CONTROL_PLANE_API_KEY             (provider-standard env var)
//	-> OPENAI_API_KEY                 (fallback env var)
//	-> config manager openai.api_key   (last-resort store)
//
// OpenAI has no on-disk config-file source in this tree, so the chain is
// flag/env/config-manager only. A nil config manager (tests, unwired wizard
// deps) simply drops the last-resort source.
func resolveOpenAICredentials(cmd *cli.Command, cfgMgr config.Manager) (tunnelID, apiKey string) {
	tunnelID = ResolveCredential(
		func() string { return cmd.String("tunnel-id") },
		func() string { return os.Getenv("CONTROL_PLANE_TUNNEL_ID") },
		tunnelCfgCredential(cfgMgr, "openai", "tunnel_id"),
	)
	apiKey = ResolveCredential(
		func() string { return os.Getenv("CONTROL_PLANE_API_KEY") },
		func() string { return os.Getenv("OPENAI_API_KEY") },
		tunnelCfgCredential(cfgMgr, "openai", "api_key"),
	)
	return tunnelID, apiKey
}
