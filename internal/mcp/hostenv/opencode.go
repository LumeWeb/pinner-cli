package hostenv

import "strings"

// clientNameOpenCode is the clientInfo.name that the OpenCode MCP client
// sends on initialize (e.g. {name: "opencode", version: "1.18.23"}).
const clientNameOpenCode = "opencode"

// opencodeDetector matches the OpenCode terminal coding agent. OpenCode is a
// local client that connects over co-located stdio, so it sends no HTTP
// headers and no OAuth token; its clientInfo.name is the reliable signal.
// It shares the host filesystem, so it aliases the generic stdio profile
// (source-path/sink-local/co-located) — see profileAliasTargets.
type opencodeDetector struct{}

func (opencodeDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// OpenCode is co-located stdio — never remote HTTP or an OpenAI tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameOpenCode) {
		return HostOpenCode, AuthNone
	}
	return HostUnknown, ""
}
