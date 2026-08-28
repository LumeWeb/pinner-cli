package hostenv

import (
	"net/http"
)

// DetectorRegistry holds an ordered list of host detectors and resolves
// a wire request to a PlatformProfile. Detectors are tried in priority
// order; the first match wins. When no detector matches, the registry
// falls back to a generic profile based on the transport.
type DetectorRegistry struct {
	detectors []Detector
}

// NewRegistry returns a DetectorRegistry with the default set of host
// detectors in priority order.
func NewRegistry() *DetectorRegistry {
	return &DetectorRegistry{
		detectors: []Detector{
			aiderDeskDetector{},
			devinDetector{},
			clineDetector{},
			openAIDetector{},
			grokDetector{},
			claudeDetector{},
			claudeDesktopDetector{},
		},
	}
}

// Detect resolves a DetectRequest to a PlatformProfile. It:
//  1. Determines the transport from CoLocated/TunnelOpenAI flags.
//  2. Tries registered detectors to identify the host.
//  3. Looks up the pre-declared profile for the host + transport pair.
//  4. Overlays runtime wire signals (headers, tokenInfo, etc.).
func (r *DetectorRegistry) Detect(req DetectRequest) PlatformProfile {
	transport := detectTransport(req)

	hostType := HostUnknown
	authMethod := AuthNone

	for _, d := range r.detectors {
		h, a := d.Match(req)
		if h != HostUnknown {
			hostType = h
			authMethod = a
			break
		}
	}

	if hostType == HostUnknown {
		// No detector matched — degrade to generic by transport.
		if req.TunnelOpenAI {
			hostType = HostChatGPT
			authMethod = AuthNone
		}
	}

	profile := resolveProfile(hostType, transport, authMethod)

	// A matched detector also reports the per-request wire auth (AuthOAuth when
	// TokenInfo is present, else AuthBearer). resolveProfile only selects a
	// static variant and does not consume it, so on response-auth transports
	// (HTTP) copy the detected auth onto the returned profile when a host was
	// matched — consumers of profile.AuthMethod should see the actual wire
	// auth, not the declared default (e.g. ProfileOpenAIHTTP/ProfileGrokHTTP
	// always declare AuthOAuth even for token-less bearer requests).
	//
	// The overlay is intentionally limited to TransportHTTP. The OpenAI secure
	// tunnel is an AuthNone transport by design: even a detector-matched tunnel
	// request with a token must keep its static AuthNone (see
	// ProfileOpenAITunnel), so a token cannot flip a tunnel profile to
	// OAuth/bearer.
	if hostType != HostUnknown && profile.Transport == TransportHTTP {
		profile.AuthMethod = authMethod
	}

	// Overlay runtime wire signals.
	profile.ClientInfo = req.ClientInfo
	profile.ProtocolVer = req.ProtocolVersion
	profile.UserAgent = req.UserAgent
	profile.Headers = req.Headers
	profile.TokenInfo = req.TokenInfo

	return profile
}

// DetectFromHTTPRequest is a convenience that builds a DetectRequest
// from an HTTP request and the server's transport flags, then calls
// Detect.
func (r *DetectorRegistry) DetectFromHTTPRequest(h http.Header, coLocated, tunnelOpenAI bool, tokenInfo *TokenInfo) PlatformProfile {
	req := DetectRequest{
		UserAgent:    h.Get("User-Agent"),
		Headers:      h,
		TokenInfo:    tokenInfo,
		CoLocated:    coLocated,
		TunnelOpenAI: tunnelOpenAI,
	}
	return r.Detect(req)
}

// detectTransport determines the TransportKind from the launch flags.
func detectTransport(req DetectRequest) TransportKind {
	if req.CoLocated {
		return TransportStdio
	}
	if req.TunnelOpenAI {
		return TransportOpenAI
	}
	return TransportHTTP
}
