package hostenv

import (
	"strings"
)

// A Detector identifies the connected MCP host from wire signals.
// Implementations are registered in a DetectorRegistry and tried in
// priority order. The first detector that reports a match wins.
type Detector interface {
	// Match returns the detected HostType and AuthMethod if the wire
	// signals in req match this detector's host. Returns HostUnknown
	// and an empty AuthMethod if no match.
	Match(req DetectRequest) (HostType, AuthMethod)
}

// openAIDetector matches the OpenAI openai-mcp client. It sends:
//   - User-Agent: "openai-mcp/1.0.0"
//   - clientInfo: {name: "openai-mcp", version: "1.0.0"}
//   - Protocol version 2026-07-28
//   - X-Openai-Session and X-Openai-Subject headers
type openAIDetector struct{}

func (openAIDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// OpenAI is always remote — never co-located stdio.
	if req.CoLocated {
		return HostUnknown, ""
	}
	// Check User-Agent first — most reliable signal for HTTP.
	ua := strings.ToLower(req.UserAgent)
	if strings.Contains(ua, "openai") {
		auth := AuthBearer
		if req.TokenInfo != nil {
			auth = AuthOAuth
		}
		return HostOpenAI, auth
	}
	// Check clientInfo.name — works on any transport.
	if req.ClientInfo != nil {
		name := strings.ToLower(req.ClientInfo.Name)
		if strings.Contains(name, "openai") || strings.Contains(name, "chatgpt") {
			auth := AuthBearer
			if req.TokenInfo != nil {
				auth = AuthOAuth
			}
			return HostOpenAI, auth
		}
	}
	// Check for OpenAI-specific HTTP headers.
	if req.Headers != nil {
		if req.Headers.Get("X-Openai-Session") != "" || req.Headers.Get("X-Openai-Subject") != "" {
			return HostOpenAI, AuthOAuth
		}
	}
	return HostUnknown, ""
}

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
	ua := strings.ToLower(req.UserAgent)
	if strings.Contains(ua, "grok") {
		auth := AuthBearer
		if req.TokenInfo != nil {
			auth = AuthOAuth
		}
		return HostGrok, auth
	}
	return HostUnknown, ""
}


