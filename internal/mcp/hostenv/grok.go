package hostenv

import "strings"

// userAgentGrokProduct is the product token xAI Grok connectors place in the
// User-Agent (grok-connectors-manager/...). Matching the full product name —
// rather than the 3-letter substring "grok" — prevents an unrelated client
// whose User-Agent merely happens to contain "grok" from being classified as
// Grok.
const userAgentGrokProduct = "grok-connectors-manager"

// clientNameGrokShell is the product token the xAI Grok Shell co-located
// client sends in clientInfo.name (e.g. "grok-shell-pinner"). Grok Shell is a
// co-located stdio client, so clientInfo.name is the reliable signal (stdio
// carries no User-Agent header). The match token is the full product name
// "grok-shell" — a bare "grok" substring is too broad and would misclassify any
// unrelated stdio client whose name happens to contain it.
const clientNameGrokShell = "grok-shell"

// grokDetector matches xAI Grok hosts on both supported transports:
//
//   - Remote HTTP connector: User-Agent "grok-connectors-manager/0.1.0",
//     clientInfo NOT sent (nil); a future build may send an exact clientInfo
//     name "grok". Protocol version 2025-11-25, OAuth bearer token.
//   - Co-located stdio Grok Shell: clientInfo name "grok-shell-pinner" and
//     kin, no auth.
type grokDetector struct{}

func (grokDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Grok cannot use the OpenAI-specific tunnel transport on any host.
	if req.TunnelOpenAI {
		return HostUnknown, ""
	}
	// Co-located stdio: the Grok Shell client. Match the full "grok-shell"
	// product token rather than a bare "grok" substring, so an unrelated
	// co-located client whose name merely contains "grok" is not classified
	// as Grok.
	if req.CoLocated {
		if req.ClientInfo != nil && strings.Contains(strings.ToLower(strings.TrimSpace(req.ClientInfo.Name)), clientNameGrokShell) {
			return HostGrok, AuthNone
		}
		return HostUnknown, ""
	}
	// Remote HTTP: the grok-connectors-manager User-Agent.
	if uaMatch(req.UserAgent, userAgentGrokProduct) {
		return HostGrok, authFromToken(req.TokenInfo)
	}
	// A Grok connector may start sending clientInfo. Accept an exact
	// (case-insensitive) name match as the primary signal so detection does
	// not depend solely on the User-Agent substring.
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), "grok") {
		return HostGrok, authFromToken(req.TokenInfo)
	}
	return HostUnknown, ""
}

// uaMatch reports whether the User-Agent contains the product token,
// case-insensitively.
func uaMatch(userAgent, product string) bool {
	return strings.Contains(strings.ToLower(userAgent), product)
}
