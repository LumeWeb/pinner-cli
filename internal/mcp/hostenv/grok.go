package hostenv

import "strings"

// userAgentGrokProduct is the product token xAI Grok connectors place in the
// User-Agent (grok-connectors-manager/...). Matching the full product name —
// rather than the 3-letter substring "grok" — prevents an unrelated client
// whose User-Agent merely happens to contain "grok" from being classified as
// Grok.
const userAgentGrokProduct = "grok-connectors-manager"

// grokDetector matches xAI Grok connectors. Grok sends:
//   - User-Agent: "grok-connectors-manager/0.1.0"
//   - clientInfo: NOT sent (nil); if a future build sends clientInfo, the
//     exact name "grok" is accepted as the primary signal.
//   - Protocol version 2025-11-25
//   - OAuth bearer token
type grokDetector struct{}

func (grokDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Grok is always remote — never co-located stdio, and cannot use
	// the OpenAI-specific tunnel transport.
	if req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if uaMatch(req.UserAgent, userAgentGrokProduct) {
		return HostGrok, authFromToken(req.TokenInfo)
	}
	// A future Grok connector may start sending clientInfo. Accept an exact
	// (case-insensitive) name match as the primary signal so detection does
	// not depend solely on the User-Agent substring.
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), "grok") {
		return HostGrok, authFromToken(req.TokenInfo)
	}
	return HostUnknown, ""
}

// uaMatch reports whether the User-Agent contains the product token,
// case-insensitively. It mirrors the older single-substring helper while
// making the token an exact product name rather than a generic fragment.
func uaMatch(userAgent, product string) bool {
	return strings.Contains(strings.ToLower(userAgent), product)
}
