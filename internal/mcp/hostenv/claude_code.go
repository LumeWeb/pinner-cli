package hostenv

import "strings"

// clientNameClaudeCode is the clientInfo.name product token the Claude Code
// client sends ("claude-code", e.g. version 2.1.226). Claude Code is a
// co-located stdio client, so clientInfo.name is the reliable signal (stdio
// carries no User-Agent header).
//
// The match token is the product name "claude-code". It is distinct from
// Claude Desktop's exact "claude-ai" identity and from Claude Web's
// "anthropic" vendor token, so the three Anthropic detectors never agree on
// the same clientInfo.
const clientNameClaudeCode = "claude-code"

// claudeCodeDetector matches Anthropic's Claude Code CLI client. It renders
// MCP Apps UI, so it is registered as a profile alias of HostStdioApps (see
// profileAliasTargets). It is its own HostType so callers can gate on
// HostIs(HostClaudeCode) without duplicating a feature set.
type claudeCodeDetector struct{}

func (claudeCodeDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Claude Code is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameClaudeCode) {
		return HostClaudeCode, AuthNone
	}
	return HostUnknown, ""
}
