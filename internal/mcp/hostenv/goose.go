package hostenv

import "strings"

// clientNameGoose is the clientInfo.name product token the Goose agent sends
// ("goose-app"). Goose is a co-located stdio client, so clientInfo.name is the
// reliable signal (stdio carries no User-Agent header).
const clientNameGoose = "goose-app"

// gooseDetector matches the Goose agent. Goose is a co-located stdio client
// whose capability surface is the shared stdio+MCP-Apps profile (source-path /
// sink-local plus MCP Apps UI), so it is registered as a profile alias of
// HostStdioApps — the same target as Claude Desktop (see profileAliasTargets).
// It is its own HostType so callers can gate on HostIs(HostGoose) without
// duplicating a feature set.
type gooseDetector struct{}

func (gooseDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Goose is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameGoose) {
		return HostGoose, AuthNone
	}
	return HostUnknown, ""
}
