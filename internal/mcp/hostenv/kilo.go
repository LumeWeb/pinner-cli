package hostenv

import "strings"

// clientNameKilo is the exact clientInfo.name the Kilo Code editor sends
// (e.g. {name: "kilo", version: "7.5.5"}). Kilo Code is a co-located stdio
// client, so clientInfo.name is the reliable signal (stdio carries no
// User-Agent header).
//
// It is matched EXACTLY (case-insensitive), not by substring: "kilo" is a
// short token that can appear inside unrelated names, so only the exact
// product token is accepted (the same hardening the cline detector applies).
const clientNameKilo = "kilo"

// kiloDetector matches the Kilo Code editor's MCP client. Its capability
// surface is identical to the generic stdio profile, so it is registered as a
// profile alias of HostGeneric (see profileAliasTargets). It is its own
// HostType so callers can gate on HostIs(HostKilo) without duplicating a
// feature set.
type kiloDetector struct{}

func (kiloDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Kilo Code is a co-located stdio client — never the OpenAI tunnel and
	// never reachable over the server's HTTP mux.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameKilo) {
		return HostKilo, AuthNone
	}
	return HostUnknown, ""
}
