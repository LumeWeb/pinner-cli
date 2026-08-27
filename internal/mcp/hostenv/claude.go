package hostenv

import "strings"

// User-agent, clientInfo, and header substrings sent by Anthropic's Claude
// Web client. The clientInfo match keys on the "anthropic" vendor token
// (Web sends "Anthropic/ClaudeAI"); Claude Desktop sends the distinct name
// "claude-ai" (no "anthropic") and is matched by claudeDesktopDetector, so
// the two detectors never disagree on the same clientInfo.
const (
	userAgentClaude         = "claude-user"
	clientNameAnthropic     = "anthropic"
	headerXAnthropicClient  = "X-Anthropic-Client"
	headerXAnthropicVersion = "X-Anthropic-Version"
)

// claudeDetector matches Anthropic's Claude Web client over HTTP. It sends:
//   - User-Agent: "Claude-User"
//   - clientInfo: {name: "Anthropic/ClaudeAI", version: "1.0.0"}
//   - X-Anthropic-Client header (e.g. "ClaudeAI")
//   - Protocol version 2026-07-28, OAuth bearer token
//
// Claude Web is always remote — never co-located stdio, and it does not use
// the OpenAI tunnel. The User-Agent match is intentionally narrow
// ("claude-user") so a generic HTTP client whose UA merely contains "claude"
// is not misclassified; co-located Claude Desktop is handled by its own
// detector (claudeDesktopDetector) instead.
type claudeDetector struct{}

func (claudeDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Claude Web is always remote — never co-located stdio or the OpenAI tunnel.
	if req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	// Check User-Agent first — most reliable signal for HTTP.
	if host, auth := matchUserAgent(req, HostClaude, userAgentClaude); host != HostUnknown {
		return host, auth
	}
	// Check clientInfo.name — match the "anthropic" vendor token
	// ("Anthropic/ClaudeAI"). Deliberately NOT a bare "claude" substring,
	// which would also match Desktop's "claude-ai" identity.
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameAnthropic) {
		return HostClaude, authFromToken(req.TokenInfo)
	}
	// Check for Anthropic-specific HTTP headers.
	if req.Headers != nil {
		if req.Headers.Get(headerXAnthropicClient) != "" || req.Headers.Get(headerXAnthropicVersion) != "" {
			return HostClaude, authFromToken(req.TokenInfo)
		}
	}
	return HostUnknown, ""
}
