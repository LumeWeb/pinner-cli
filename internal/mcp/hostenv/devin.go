package hostenv

import "strings"

// clientNameDevin is the exact clientInfo.name the Devin harness sends.
// Devin's harness is built on the official Rust rmcp MCP SDK and does not
// override its client identity (rmcp's with_client_info), so the SDK's own
// default service name, "rmcp", leaks through as clientInfo.name.
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
	// "rmcp" is the generic Rust MCP SDK default service name shared by every
	// rmcp-based client, so only an EXACT match is accepted — a substring
	// match would misclassify any other Rust MCP client as Devin. A "devin"
	// identity (should a future Devin build override its clientInfo) is
	// Devin-specific and safe to match anywhere.
	if name == clientNameDevin || strings.Contains(name, "devin") {
		return HostDevin, AuthNone
	}
	return HostUnknown, ""
}
