package hostenv

import "strings"

// clientNameClaudeDesktop is the exact clientInfo name Claude Desktop sends
// ("claude-ai", version 0.1.0). Matched case-insensitively and to the exact
// name — never a bare "claude" substring, which would be ambiguous with the
// Web client's "Anthropic/ClaudeAI".
const clientNameClaudeDesktop = "claude-ai"

// claudeDesktopDetector matches Anthropic's Claude Desktop client over
// stdio (co-located). It sends:
//   - clientInfo: {name: "claude-ai", version: "0.1.0"}
//   - Protocol version 2025-11-25
//   - No HTTP headers and no OAuth token (local stdio transport)
//
// Claude Desktop is always co-located — never remote HTTP or the OpenAI
// tunnel. It is disjoint from Claude Web (HTTP-only, matched by
// claudeDetector on the "anthropic" vendor token), so Desktop is resolved to
// its own HostClaudeDesktop profile with full local file access rather than
// Web's base64-only, network-restricted surface.
type claudeDesktopDetector struct{}

func (claudeDesktopDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Claude Desktop is always co-located stdio — never the OpenAI tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameClaudeDesktop) {
		return HostClaudeDesktop, AuthNone
	}
	return HostUnknown, ""
}
