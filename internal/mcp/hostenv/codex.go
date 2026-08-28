package hostenv

import "strings"

// clientNameCodex is the clientInfo.name product token the Codex client sends
// ("codex-mcp-client"). Codex is a co-located stdio client, so clientInfo.name
// is the reliable signal (stdio carries no User-Agent header). The match token
// is the full product name "codex-mcp" — a bare "codex" substring is too broad
// and would misclassify any unrelated client whose name happens to contain it
// (the same failure class the devin detector was hardened against).
const clientNameCodex = "codex-mcp"

// codexDetector matches the Codex client. Its capability surface is identical
// to the generic stdio profile, so it is registered as a profile alias of
// HostGeneric (see profileAliasTargets). It is its own HostType so callers can
// gate on HostIs(HostCodex) without duplicating a feature set.
type codexDetector struct{}

func (codexDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Codex is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameCodex) {
		return HostCodex, AuthNone
	}
	return HostUnknown, ""
}
