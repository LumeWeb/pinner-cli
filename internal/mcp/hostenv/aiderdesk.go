package hostenv

import "strings"

// clientNameAiderDesk is the clientInfo.name product token the aider-desk
// client sends ("aider-desk-client"). Aider-desk is a co-located stdio
// client, so clientInfo.name is the reliable signal (stdio carries no
// User-Agent header).
const clientNameAiderDesk = "aider-desk"

// aiderDeskDetector matches the aider-desk client. Its capability surface is
// identical to the generic stdio profile, so it is registered as a profile
// alias of HostGeneric (see profileAliasTargets). It is its own HostType so
// callers can gate on HostIs(HostAiderDesk) without duplicating a feature
// set.
type aiderDeskDetector struct{}

func (aiderDeskDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// aider-desk is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.Contains(strings.ToLower(req.ClientInfo.Name), clientNameAiderDesk) {
		return HostAiderDesk, AuthNone
	}
	return HostUnknown, ""
}
