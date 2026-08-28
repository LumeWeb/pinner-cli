package hostenv

import "strings"

// clientNameDevin is the clientInfo.name product token the Devin harness
// sends. Devin's harness is built on the official Rust rmcp MCP SDK and does
// not override its client identity (rmcp's with_client_info), so the SDK's
// own service name, "rmcp", leaks through as clientInfo.name. If a future
// Devin build overrides it to "devin", that is accepted as well.
const clientNameDevin = "rmcp"

// devinDetector matches the Devin harness (Cognition) as a co-located stdio
// client. Its capability surface is identical to the generic stdio profile,
// so it is registered as a profile alias of HostGeneric (see
// profileAliasTargets). It is its own HostType so callers can gate on
// HostIs(HostDevin) without duplicating a feature set.
type devinDetector struct{}

func (devinDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Devin is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo == nil {
		return HostUnknown, ""
	}
	name := strings.ToLower(strings.TrimSpace(req.ClientInfo.Name))
	if strings.Contains(name, clientNameDevin) || strings.Contains(name, "devin") {
		return HostDevin, AuthNone
	}
	return HostUnknown, ""
}
