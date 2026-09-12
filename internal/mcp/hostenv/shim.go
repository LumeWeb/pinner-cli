// Package hostenv is a compatibility shim carrying the pinner-cli internal
// API on top of the extracted go.lumeweb.com/canimcp module.
//
// The generic host-compatibility model (profile/feature model, wire evidence,
// detectors, registry) lives in canimcp. This package keeps two
// pinner-specific concerns out of the library and local to the CLI:
//
//   - DomainScope: which Pinner operation domains a server registers (domain
//     availability is product policy, not a wire capability fact).
//   - Hosted: whether this server is a Portal-embedded assembly
//     (deployment property, likewise product policy).
//
// It preserves the pre-extraction (chain-era) API surface — PlatformProfile,
// DetectRequest, DetectorRegistry, and the mcpplane/model alias collapse —
// so in-repo callers compile unchanged while the generic model migrates to
// canimcp incrementally. The vocabulary that in-repo callers share with the
// SDK-neutral model layer (Feature, FeatureSet, HostType, TransportKind,
// AuthMethod, ClientInfo, TokenInfo) stays aliased onto mcpplane/model: the
// wire values are byte-identical in both packages, so CLI declarations remain
// assignment-compatible with model.FeatureSet and model.Profile. Everything
// else delegates to canimcp over per-package value conversions, which are
// infallible because the struct shapes are identical.
package hostenv

import (
	"net/http"

	canimcp "go.lumeweb.com/canimcp"
	"go.lumeweb.com/mcpplane/model"
)

// Core model types are aliases onto mcpplane/model so the CLI vocabulary
// stays assignment-compatible with the shared model.Profile /
// model.FeatureSet that travels with requests.

type (
	// HostType identifies the connected MCP client platform.
	HostType = model.HostType
	// TransportKind is the MCP transport the server runs under.
	TransportKind = model.TransportKind
	// AuthMethod describes how the client authenticated.
	AuthMethod = model.AuthMethod
	// Feature is a named capability a host platform may or may not support.
	Feature = model.Feature
	// FeatureSet is the set of features a platform profile supports.
	FeatureSet = model.FeatureSet
	// ClientInfo carries the MCP clientInfoImplementation fields from the wire.
	ClientInfo = model.ClientInfo
	// TokenInfo carries the OAuth bearer token information from the SDK
	// auth middleware.
	TokenInfo = model.TokenInfo
	// Detector identifies the connected MCP host from wire signals.
	Detector = canimcp.Detector
)

// Host types, re-declared with the model.HostType shim vocabulary (a typed
// constant conversion; values are byte-identical to the canimcp ones).
const (
	HostUnknown       = model.HostType(canimcp.HostUnknown)
	HostOpenAI        = model.HostType(canimcp.HostOpenAI)
	HostChatGPT       = model.HostType(canimcp.HostChatGPT)
	HostGrok          = model.HostType(canimcp.HostGrok)
	HostOpenCode      = model.HostType(canimcp.HostOpenCode)
	HostKilo          = model.HostType(canimcp.HostKilo)
	HostKiro          = model.HostType(canimcp.HostKiro)
	HostClaude        = model.HostType(canimcp.HostClaude)
	HostClaudeDesktop = model.HostType(canimcp.HostClaudeDesktop)
	HostClaudeCode    = model.HostType(canimcp.HostClaudeCode)
	HostStdioApps     = model.HostType(canimcp.HostStdioApps)
	HostAiderDesk     = model.HostType(canimcp.HostAiderDesk)
	HostDevin         = model.HostType(canimcp.HostDevin)
	HostCline         = model.HostType(canimcp.HostCline)
	HostCodex         = model.HostType(canimcp.HostCodex)
	HostCopilotCLI    = model.HostType(canimcp.HostCopilotCLI)
	HostGoose         = model.HostType(canimcp.HostGoose)
	HostAntigravity   = model.HostType(canimcp.HostAntigravity)
	HostKimi          = model.HostType(canimcp.HostKimi)
	HostZed           = model.HostType(canimcp.HostZed)
	HostFX            = model.HostType(canimcp.HostFX)
	HostGeneric       = model.HostType(canimcp.HostGeneric)
)

// Transports.
const (
	TransportStdio  = model.TransportStdio
	TransportHTTP   = model.TransportHTTP
	TransportOpenAI = model.TransportOpenAI
)

// Auth methods.
const (
	AuthNone   = model.AuthNone
	AuthBearer = model.AuthBearer
	AuthOAuth  = model.AuthOAuth
)

// Features, re-declared with the model.Feature shim vocabulary (a typed
// constant conversion; values are byte-identical to the canimcp ones).
const (
	FeatFileHostInput = model.Feature(canimcp.FeatFileHostInput)
	FeatSourcePath    = model.Feature(canimcp.FeatSourcePath)
	FeatSourceMint    = model.Feature(canimcp.FeatSourceMint)
	FeatSourceURL     = model.Feature(canimcp.FeatSourceURL)
	FeatSourceData    = model.Feature(canimcp.FeatSourceData)
	FeatXMcpFile      = model.Feature(canimcp.FeatXMcpFile)
	FeatSinkLocal     = model.Feature(canimcp.FeatSinkLocal)
	FeatSinkDrop      = model.Feature(canimcp.FeatSinkDrop)
	FeatMCPApps       = model.Feature(canimcp.FeatMCPApps)
	FeatElicitation   = model.Feature(canimcp.FeatElicitation)
	FeatRemoteAccess  = model.Feature(canimcp.FeatRemoteAccess)
	FeatCoLocated     = model.Feature(canimcp.FeatCoLocated)
)

// DetectRequest carries the raw wire signals extracted from an MCP request.
// It mirrors canimcp.Evidence field-for-field but carries the model-typed
// ClientInfo/TokenInfo that in-repo constructors populate; convertEvidence
// adapts it to the library evidence at the delegation boundary.
type DetectRequest struct {
	ClientInfo      *ClientInfo
	ProtocolVersion string
	UserAgent       string
	Headers         http.Header
	TokenInfo       *TokenInfo
	CoLocated       bool
	TunnelOpenAI    bool
}

// PlatformProfile is the pre-extraction profile shape: the canimcp core
// profile plus the Pinner-only DomainScope and Hosted deployment properties.
// It is a shim type; behavior lives in canimcp.
type PlatformProfile struct {
	HostType   HostType
	Transport  TransportKind
	AuthMethod AuthMethod
	Remote     bool
	Features   FeatureSet

	// DomainScope and Hosted are Pinner composition-root policies (full for the
	// CLI/local server, restricted/hosted for a Portal-embedded assembly).
	// They are server-construction-time properties, never wire signals; a
	// zero DomainScope means the full surface.
	DomainScope DomainScope
	Hosted      bool

	// Raw wire signals, populated by the detector for runtime
	// introspection by tools that need them at call time.
	ClientInfo  *ClientInfo
	ProtocolVer string
	UserAgent   string
	Headers     http.Header
	TokenInfo   *TokenInfo
}

// convertEvidence adapts the shim DetectRequest to the canimcp evidence.
// The struct shapes are identical, so the conversions are pure re-wrapping.
func convertEvidence(req DetectRequest) canimcp.Evidence {
	return canimcp.Evidence{
		ClientInfo:      toCoreClientInfo(req.ClientInfo),
		ProtocolVersion: req.ProtocolVersion,
		UserAgent:       req.UserAgent,
		Headers:         req.Headers,
		TokenInfo:       toCoreTokenInfo(req.TokenInfo),
		CoLocated:       req.CoLocated,
		TunnelOpenAI:    req.TunnelOpenAI,
	}
}

// toCoreClientInfo rewraps a model.ClientInfo pointer as the canimcp type.
func toCoreClientInfo(c *ClientInfo) *canimcp.ClientInfo {
	if c == nil {
		return nil
	}
	v := canimcp.ClientInfo(*c)
	return &v
}

// toCoreTokenInfo rewraps a model.TokenInfo pointer as the canimcp type.
func toCoreTokenInfo(t *TokenInfo) *canimcp.TokenInfo {
	if t == nil {
		return nil
	}
	v := canimcp.TokenInfo(*t)
	return &v
}

// fromCoreClientInfo is the inverse of toCoreClientInfo.
func fromCoreClientInfo(c *canimcp.ClientInfo) *ClientInfo {
	if c == nil {
		return nil
	}
	v := ClientInfo(*c)
	return &v
}

// fromCoreTokenInfo is the inverse of toCoreTokenInfo.
func fromCoreTokenInfo(t *canimcp.TokenInfo) *TokenInfo {
	if t == nil {
		return nil
	}
	v := TokenInfo(*t)
	return &v
}

// convertFeatureSet copies a canimcp FeatureSet into the model vocabulary.
func convertFeatureSet(fs canimcp.FeatureSet) FeatureSet {
	out := make(FeatureSet, len(fs))
	for k, v := range fs {
		out[model.Feature(k)] = v
	}
	return out
}

// toCoreFeatureSet copies the model feature vocabulary into canimcp.
func toCoreFeatureSet(fs FeatureSet) canimcp.FeatureSet {
	out := make(canimcp.FeatureSet, len(fs))
	for k, v := range fs {
		out[canimcp.Feature(k)] = v
	}
	return out
}

// core converts the shim profile to the canimcp core profile, dropping the
// Pinner-only DomainScope/Hosted fields.
func (p PlatformProfile) core() canimcp.Profile {
	return canimcp.Profile{
		HostType:    canimcp.HostType(p.HostType),
		Transport:   canimcp.TransportKind(p.Transport),
		AuthMethod:  canimcp.AuthMethod(p.AuthMethod),
		Remote:      p.Remote,
		Features:    toCoreFeatureSet(p.Features),
		ClientInfo:  toCoreClientInfo(p.ClientInfo),
		ProtocolVer: p.ProtocolVer,
		UserAgent:   p.UserAgent,
		Headers:     p.Headers,
		TokenInfo:   toCoreTokenInfo(p.TokenInfo),
	}
}

// shimFromCore builds a shim profile from a canimcp core profile, with the
// Pinner-only DomainScope/Hosted left at their zero values (full surface, not
// hosted) — the same defaults Detect has always produced.
func shimFromCore(c canimcp.Profile) PlatformProfile {
	return PlatformProfile{
		HostType:    model.HostType(c.HostType),
		Transport:   model.TransportKind(c.Transport),
		AuthMethod:  model.AuthMethod(c.AuthMethod),
		Remote:      c.Remote,
		Features:    convertFeatureSet(c.Features),
		ClientInfo:  fromCoreClientInfo(c.ClientInfo),
		ProtocolVer: c.ProtocolVer,
		UserAgent:   c.UserAgent,
		Headers:     c.Headers,
		TokenInfo:   fromCoreTokenInfo(c.TokenInfo),
	}
}

// Has reports whether the profile supports the given feature.
func (p PlatformProfile) Has(f Feature) bool { return p.Features.Has(f) }

// IsTransport reports whether the profile's transport matches t.
func (p PlatformProfile) IsTransport(t TransportKind) bool { return p.Transport == t }

// IsHost reports whether the profile's host type matches h.
func (p PlatformProfile) IsHost(h HostType) bool { return p.HostType == h }

// CloneFeatures returns a shallow copy of this profile with a cloned
// FeatureSet. Callers that overlay runtime flags MUST use this before
// mutating Features — the FeatureSet in a static profile is a shared map.
func (p PlatformProfile) CloneFeatures() PlatformProfile {
	p.Features = p.Features.Clone()
	return p
}

// Shared adapts this CLI profile to the SDK-neutral model.Profile carried
// on model.ToolRequest / model.RequestCaps. It copies every shared field;
// the CLI-only DomainScope field (which domain surfaces this server exposes) is
// deliberately not representable there: it is a server-construction-time
// property, only read from CLI-side PlatformProfile values, never from the
// per-request model profile.
func (p PlatformProfile) Shared() model.Profile {
	return model.Profile{
		HostType:    p.HostType,
		Transport:   p.Transport,
		AuthMethod:  p.AuthMethod,
		Remote:      p.Remote,
		Features:    p.Features,
		Hosted:      p.Hosted,
		ClientInfo:  p.ClientInfo,
		ProtocolVer: p.ProtocolVer,
		UserAgent:   p.UserAgent,
		Headers:     p.Headers,
		TokenInfo:   p.TokenInfo,
	}
}

// FromShared reconstructs the CLI PlatformProfile view from the SDK-neutral
// model.Profile. DomainScope is zero (meaning "full surface") because the model
// profile cannot carry it — only use this where the consumer is
// surface-independent (feature/transport/host-gated description and schema
// resolution, which never gate on DomainScope).
func FromShared(sp model.Profile) PlatformProfile {
	return PlatformProfile{
		HostType:    sp.HostType,
		Transport:   sp.Transport,
		AuthMethod:  sp.AuthMethod,
		Remote:      sp.Remote,
		Features:    sp.Features,
		Hosted:      sp.Hosted,
		ClientInfo:  sp.ClientInfo,
		ProtocolVer: sp.ProtocolVer,
		UserAgent:   sp.UserAgent,
		Headers:     sp.Headers,
		TokenInfo:   sp.TokenInfo,
	}
}

// Predicate is a boolean test over a resolved PlatformProfile. It stays
// shim-local because DomainScope/Hosted gating needs the shim shape; generic
// predicate helpers below mirror the canimcp ones exactly.
type Predicate func(PlatformProfile) bool

// HostIs matches profiles connected from the given host.
func HostIs(h HostType) Predicate {
	return func(p PlatformProfile) bool { return p.HostType == h }
}

// Not negates a predicate.
func Not(p Predicate) Predicate {
	return func(prof PlatformProfile) bool { return !p(prof) }
}

// And returns a predicate that passes only when every given predicate passes.
func And(preds ...Predicate) Predicate {
	return func(prof PlatformProfile) bool {
		for _, p := range preds {
			if !p(prof) {
				return false
			}
		}
		return true
	}
}

// TransportIs matches profiles running on the given transport.
func TransportIs(t TransportKind) Predicate {
	return func(p PlatformProfile) bool { return p.Transport == t }
}

// Static profiles re-exported from canimcp. The shim copies carry zero-valued
// DomainScope/Hosted, matching the original declarations.
var (
	// ProfileOpenAITunnel is the OpenAI/ChatGPT embedded tunnel.
	ProfileOpenAITunnel = shimFromCore(canimcp.ProfileOpenAITunnel)
	// ProfileOpenAIHTTP is OpenAI's openai-mcp client connecting over HTTP.
	ProfileOpenAIHTTP = shimFromCore(canimcp.ProfileOpenAIHTTP)
	// ProfileGrokHTTP is xAI Grok connectors over HTTP + OAuth.
	ProfileGrokHTTP = shimFromCore(canimcp.ProfileGrokHTTP)
	// ProfileGrokStdio is co-located stdio Grok Shell.
	ProfileGrokStdio = shimFromCore(canimcp.ProfileGrokStdio)
	// ProfileClaudeHTTP is Claude web over HTTP.
	ProfileClaudeHTTP = shimFromCore(canimcp.ProfileClaudeHTTP)
	// ProfileStdioMCPApps is the synthetic stdio host that renders MCP Apps.
	ProfileStdioMCPApps = shimFromCore(canimcp.ProfileStdioMCPApps)
	// ProfileStdioGeneric is the fallback stdio profile.
	ProfileStdioGeneric = shimFromCore(canimcp.ProfileStdioGeneric)
	// ProfileHTTPGeneric is the fallback HTTP profile.
	ProfileHTTPGeneric = shimFromCore(canimcp.ProfileHTTPGeneric)
)

// ProfileForTransport returns the generic profile for a transport. Used at
// server startup when only the transport is known (no per-request detection
// has run yet).
func ProfileForTransport(t TransportKind) PlatformProfile {
	return shimFromCore(canimcp.ProfileForTransport(canimcp.TransportKind(t)))
}

// DetectorRegistry is the shim over the canimcp registry. Detect returns the
// pre-extraction PlatformProfile with zero-valued DomainScope/Hosted.
type DetectorRegistry struct {
	core *canimcp.DetectorRegistry
}

// NewRegistry returns a DetectorRegistry with the default set of host
// detectors in priority order.
func NewRegistry() *DetectorRegistry {
	return &DetectorRegistry{core: canimcp.NewRegistry()}
}

// Core exposes the underlying canimcp registry. The mcpplane SDK's shared
// caps builder (sdk.RequestCapsOptions.Registry) accepts the core registry
// directly, so composition roots share one detector set between the shim's
// per-request Detect path and the SDK's RequestCaps pipeline.
func (r *DetectorRegistry) Core() *canimcp.DetectorRegistry {
	if r == nil {
		return nil
	}
	return r.core
}

// Detect resolves a DetectRequest to a PlatformProfile.
func (r *DetectorRegistry) Detect(req DetectRequest) PlatformProfile {
	return shimFromCore(r.core.Detect(convertEvidence(req)))
}

// DetectFromHTTPRequest builds a DetectRequest from an HTTP request and the
// server's transport flags, then calls Detect.
func (r *DetectorRegistry) DetectFromHTTPRequest(h http.Header, coLocated, tunnelOpenAI bool, tokenInfo *TokenInfo) PlatformProfile {
	return shimFromCore(r.core.DetectFromHTTPRequest(h, coLocated, tunnelOpenAI, toCoreTokenInfo(tokenInfo)))
}
