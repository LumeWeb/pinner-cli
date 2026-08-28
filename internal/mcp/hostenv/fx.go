package hostenv

import "strings"

// clientNameFX is the exact clientInfo.name the fx (fx.sh) agent sends
// (e.g. {name: "fx", version: "0.0.6"}). fx is a co-located stdio client, so
// clientInfo.name is the reliable signal (stdio carries no User-Agent header).
//
// It is matched EXACTLY (case-insensitive), not by substring: "fx" is a short
// token that can appear inside unrelated names, so only the exact product token
// is accepted (the same hardening the kilo detector applies).
const clientNameFX = "fx"

// fxDetector matches the fx (fx.sh) agent. Its capability surface is identical
// to the generic stdio profile, so it is registered as a profile alias of
// HostGeneric (see profileAliasTargets). It is its own HostType so callers can
// gate on HostIs(HostFX) without duplicating a feature set.
type fxDetector struct{}

func (fxDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// fx is a co-located stdio client — never remote or the OpenAI tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameFX) {
		return HostFX, AuthNone
	}
	return HostUnknown, ""
}
