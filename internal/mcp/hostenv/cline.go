package hostenv

import "strings"

// clientNameCline is the clientInfo.name product token the Cline client sends
// ("@cline/core"). Cline (VS Code extension / CLI) is a co-located stdio
// client, so clientInfo.name is the reliable signal (stdio carries no
// User-Agent header).
const clientNameCline = "cline"

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
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameCline) {
		return HostCline, AuthNone
	}
	return HostUnknown, ""
}
