package hostenv

import "strings"

// authFromToken derives the wire auth method from the presence of an
// OAuth token. A token implies OAuth; otherwise the client used bearer.
func authFromToken(ti *TokenInfo) AuthMethod {
	if ti != nil {
		return AuthOAuth
	}
	return AuthBearer
}

// matchUserAgent returns host (with a token-derived auth) when req's
// User-Agent contains sub, or HostUnknown if it does not. Shared by
// detectors that identify their host from the User-Agent header.
func matchUserAgent(req DetectRequest, host HostType, sub string) (HostType, AuthMethod) {
	if strings.Contains(strings.ToLower(req.UserAgent), sub) {
		return host, authFromToken(req.TokenInfo)
	}
	return HostUnknown, ""
}

// A Detector identifies the connected MCP host from wire signals.
// Implementations live one per file, each matching a single host
// profile. They are registered in a DetectorRegistry and tried in
// priority order; the first detector that reports a match wins.
type Detector interface {
	// Match returns the detected HostType and AuthMethod if the wire
	// signals in req match this detector's host. Returns HostUnknown
	// and an empty AuthMethod if no match.
	Match(req DetectRequest) (HostType, AuthMethod)
}
