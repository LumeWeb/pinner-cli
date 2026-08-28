package hostenv

import "strings"

// clientNameKimi is the exact clientInfo.name the Kimi CLI (Moonshot AI)
// sends on initialize (e.g. {name: "kimi-code", version: "0.0.0"}). Kimi is
// a co-located stdio client, so clientInfo.name is the reliable signal
// (stdio carries no User-Agent header).
const clientNameKimi = "kimi-code"

// kimiDetector matches the Kimi CLI's MCP client. Its capability surface is
// identical to the generic stdio profile, so it is registered as a profile
// alias of HostGeneric (see profileAliasTargets). It is its own HostType so
// callers can gate on HostIs(HostKimi) without duplicating a feature set.
type kimiDetector struct{}

func (kimiDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Kimi is a co-located stdio client — never remote or an OpenAI tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameKimi) {
		return HostKimi, AuthNone
	}
	return HostUnknown, ""
}
