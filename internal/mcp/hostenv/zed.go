package hostenv

import "strings"

// clientNameZed is the clientInfo.name the Zed editor's MCP client sends on
// initialize (e.g. {name: "Zed", version: "0.1.0"}). Zed is a co-located
// stdio client, so clientInfo.name is the reliable signal (stdio carries no
// User-Agent header).
const clientNameZed = "zed"

// zedDetector matches the Zed editor's MCP client. Zed shares the host
// filesystem over co-located stdio, so its capability surface is identical to
// the generic stdio profile (source-path/sink-local/co-located); it is
// registered as a profile alias of HostGeneric (see profileAliasTargets). It
// is its own HostType so callers can gate on HostIs(HostZed) without
// duplicating a feature set.
type zedDetector struct{}

func (zedDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Zed is a co-located stdio client — never remote HTTP or an OpenAI tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameZed) {
		return HostZed, AuthNone
	}
	return HostUnknown, ""
}
