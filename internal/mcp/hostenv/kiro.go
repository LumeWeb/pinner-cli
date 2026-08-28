package hostenv

import "strings"

// clientNameKiro is the exact clientInfo.name the Kiro harness sends on
// initialize (e.g. {name: "Q DEV CLI", version: "1.0.0"}). Kiro is a
// co-located stdio client, so clientInfo.name is the reliable signal (stdio
// carries no User-Agent header).
//
// It is matched EXACTLY (case-insensitive), not by substring: the observed
// name does not contain the "kiro" token, so a bare "kiro" substring would
// never fire; matching the full product token is unambiguous.
const clientNameKiro = "Q DEV CLI"

// kiroDetector matches the Kiro harness. Its capability surface is identical
// to the generic stdio profile, so it is registered as a profile alias of
// HostGeneric (see profileAliasTargets). It is its own HostType so callers can
// gate on HostIs(HostKiro) without duplicating a feature set.
type kiroDetector struct{}

func (kiroDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// Kiro is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameKiro) {
		return HostKiro, AuthNone
	}
	return HostUnknown, ""
}
