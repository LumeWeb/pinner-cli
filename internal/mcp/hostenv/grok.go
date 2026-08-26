package hostenv

// userAgentGrok is the substring present in xAI Grok connector requests.
const userAgentGrok = "grok"

// grokDetector matches xAI Grok connectors. Grok sends:
//   - User-Agent: "grok-connectors-manager/0.1.0"
//   - clientInfo: NOT sent (nil)
//   - Protocol version 2025-11-25
//   - OAuth bearer token
type grokDetector struct{}

func (grokDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Grok is always remote — never co-located stdio, and cannot use
	// the OpenAI-specific tunnel transport.
	if req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	return matchUserAgent(req, HostGrok, userAgentGrok)
}
