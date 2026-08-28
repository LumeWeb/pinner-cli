package hostenv

import "strings"

// clientNameCline is the exact clientInfo.name the Cline client sends
// ("@cline/core"). Cline (VS Code extension / CLI) is a co-located stdio
// client, so clientInfo.name is the reliable signal (stdio carries no
// User-Agent header).
//
// It is matched EXACTLY (case-insensitive), not by substring: "cline" is a
// broad token that appears inside unrelated names (e.g. "decline-mcp",
// "mycline-tool"), and a substring match would misclassify any co-located
// client carrying one. Like the Devin harness's "rmcp" identity, only the
// exact product token is accepted.
const clientNameCline = "@cline/core"

// clineDetector matches the Cline client. Its capability surface is identical
// to the generic stdio profile, so it is registered as a profile alias of
// HostGeneric (see profileAliasTargets). It is its own HostType so callers can
// gate on HostIs(HostCline) without duplicating a feature set.
type clineDetector struct{}

func (clineDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Cline is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameCline) {
		return HostCline, AuthNone
	}
	return HostUnknown, ""
}
