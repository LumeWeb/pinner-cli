package hostenv

import (
	"strings"
)

// User-agent and clientInfo substrings sent by the OpenAI/ChatGPT
// clients, plus the OpenAI-specific HTTP headers used for detection.
const (
	userAgentOpenAI     = "openai"
	clientNameOpenAI    = "openai"
	clientNameChatGPT   = "chatgpt"
	headerOpenAISession = "X-Openai-Session"
	headerOpenAISubject = "X-Openai-Subject"
)

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
	if host, auth := matchUserAgent(req, HostOpenAI, userAgentOpenAI); host != HostUnknown {
		return host, auth
	}
	// Check clientInfo.name — works on any transport.
	if req.ClientInfo != nil {
		name := strings.ToLower(req.ClientInfo.Name)
		if strings.Contains(name, clientNameOpenAI) || strings.Contains(name, clientNameChatGPT) {
			return HostOpenAI, authFromToken(req.TokenInfo)
		}
	}
	// Check for OpenAI-specific HTTP headers.
	if req.Headers != nil {
		if req.Headers.Get(headerOpenAISession) != "" || req.Headers.Get(headerOpenAISubject) != "" {
			return HostOpenAI, AuthOAuth
		}
	}
	return HostUnknown, ""
}
