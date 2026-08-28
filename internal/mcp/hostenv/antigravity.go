package hostenv

import "strings"

// clientNameAntigravity is the clientInfo.name product token the Antigravity
// (Google IDE) MCP client sends (e.g. {name: "antigravity-client",
// version: "v1.0.0"}). Antigravity is a co-located stdio client, so
// clientInfo.name is the reliable signal (stdio carries no User-Agent header).
const clientNameAntigravity = "antigravity"

// antigravityDetector matches the Antigravity (Google IDE) MCP client. Its
// capability surface is identical to the generic stdio profile, so it is
// registered as a profile alias of HostGeneric (see profileAliasTargets). It
// is its own HostType so callers can gate on HostIs(HostAntigravity) without
// duplicating a feature set.
type antigravityDetector struct{}

func (antigravityDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Antigravity is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameAntigravity) {
		return HostAntigravity, AuthNone
	}
	return HostUnknown, ""
}
