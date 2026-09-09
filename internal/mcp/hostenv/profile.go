package hostenv

import (
	"net/http"

	"go.lumeweb.com/mcpplane/model"
)

// HostType identifies the connected MCP client platform. It is a type alias
// for mcpplane/model.HostType: the host vocabulary and values are
// byte-identical, so CLI host constants are comparable with the host type
// carried on the shared model.Profile a request travels with.
type HostType = model.HostType

const (
	HostUnknown       HostType = "unknown"
	HostOpenAI        HostType = "openai"
	HostChatGPT       HostType = "chatgpt"
	HostGrok          HostType = "grok"
	HostOpenCode      HostType = "opencode"
	HostKilo          HostType = "kilo"
	HostKiro          HostType = "kiro"
	HostClaude        HostType = "claude"
	HostClaudeDesktop HostType = "claude-desktop"
	HostClaudeCode    HostType = "claude-code"

	// HostStdioApps is a synthetic host representing any co-located stdio
	// client that also renders MCP Apps UI. It is the alias target shared by
	// the concrete stdio hosts that present this surface (Claude Desktop,
	// Goose); it is never detected directly.
	HostStdioApps   HostType = "stdio-apps"
	HostAiderDesk   HostType = "aider-desk"
	HostDevin       HostType = "devin"
	HostCline       HostType = "cline"
	HostCodex       HostType = "codex"
	HostCopilotCLI  HostType = "copilot-cli"
	HostGoose       HostType = "goose"
	HostAntigravity HostType = "antigravity"
	HostKimi        HostType = "kimi"
	HostZed         HostType = "zed"
	HostFX          HostType = "fx"
	HostGeneric     HostType = "generic"
)

// TransportKind is the MCP transport the server runs under. It decides
// which file-input mechanism actually works: only one mechanism is real per
// transport, and the caller never picks it — registration and the resolver do.
// The values are the same string constants used by transfer.TransportKind;
// transfer re-exports these via a type alias so both packages agree.
// It is a type alias for mcpplane/model.TransportKind (identical values) so
// CLI transports are directly comparable against the shared model.Profile.
type TransportKind = model.TransportKind

const (
	// TransportStdio is co-located stdio/local mode.
	TransportStdio TransportKind = "stdio"
	// TransportHTTP is remote HTTP or a real tunnel with a reachable HTTP mux.
	TransportHTTP TransportKind = "http"
	// TransportOpenAI is the embedded OpenAI Secure MCP Tunnel: pure MCP
	// RPC with no reachable HTTP mux.
	TransportOpenAI TransportKind = "openai"
)

// AuthMethod describes how the client authenticated. Type alias for
// mcpplane/model.AuthMethod (identical values).
type AuthMethod = model.AuthMethod

const (
	AuthNone   AuthMethod = "none"
	AuthBearer AuthMethod = "bearer"
	AuthOAuth  AuthMethod = "oauth"
)

// ClientInfo carries the MCP clientInfoImplementation fields from the
// wire (initialize params or per-request _meta). Type alias for
// mcpplane/model.ClientInfo (field-for-field identical).
type ClientInfo = model.ClientInfo

// TokenInfo carries the OAuth bearer token information extracted by the
// SDK's auth middleware. Type alias for mcpplane/model.TokenInfo
// (field-for-field identical).
type TokenInfo = model.TokenInfo

// PlatformProfile is the resolved capability set for a specific host on
// a specific transport. It is the "browser profile" in the caniuse
// analogy: a static declaration of which features a HostType + Transport
// combination supports, overlaid with runtime wire signals.
type PlatformProfile struct {
	HostType   HostType
	Transport  TransportKind
	AuthMethod AuthMethod
	Remote     bool
	Features   FeatureSet

	// Surface declares which Pinner operation domains/tool families this
	// server exposes. It is a server-construction-time property (full for the
	// CLI/local MCP server, restricted to the hosted surface on a
	// Portal-embedded server) rather than a wire signal. A zero Surface means
	// the full surface. The whole profile-aware surface — tool registration,
	// Apps, resources, prompts, and the agent_guide flow DSL — gates on it.
	Surface Surface

	// Hosted reports whether this server is a hosted (Portal-embedded)
	// assembly. Like Surface it is a server-construction-time deployment
	// property (set by the hosted construction path) rather than a wire signal,
	// and is orthogonal to the domain-availability Surface: a local stdio
	// server may use a restricted surface without being hosted. The prompt DSL
	// gates hosted-specific copy on it via HostedIs.
	Hosted bool

	// Raw wire signals, populated by the detector for runtime
	// introspection by tools that need them at call time.
	ClientInfo  *ClientInfo
	ProtocolVer string
	UserAgent   string
	Headers     http.Header
	TokenInfo   *TokenInfo
}

// Predicate is a boolean test over a resolved PlatformProfile. Builders use it
// for gates that cannot be expressed as a Feature — e.g. a decision specific to
// one host. The platform DSL (toolforge.DescBuilder and the guide builders)
// accept predicates where a feature is not the right abstraction.
type Predicate func(PlatformProfile) bool

// HostIs matches profiles connected from the given host. It is a convenience
// constructor so call sites read as prose (WhenHost(hostenv.HostGrok, ...))
// rather than spelling out the closure.
func HostIs(h HostType) Predicate {
	return func(p PlatformProfile) bool { return p.HostType == h }
}

// Not negates a predicate. Builders use it for the "unless host" style gates.
func Not(p Predicate) Predicate {
	return func(prof PlatformProfile) bool { return !p(prof) }
}

// And returns a predicate that passes only when every given predicate passes.
// Builders use it to gate a rule on a conjunction that no single gate
// expresses (e.g. "this host AND this deployment mode").
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

// TransportIs matches profiles running on the given transport. It is a
// convenience constructor so description DSL call sites read as prose
// (WhenTransport(hostenv.TransportOpenAI, ...)) rather than spelling out a
// closure. It lets a segment gate on the transport alone — e.g. url/data
// source-mode copy for upload_file, which only actually accept those modes on
// the OpenAI tunnel transport even when a host profile also declares
// FeatSourceData/FeatSourceURL to register the separate upload_data/upload_url
// tools.
func TransportIs(t TransportKind) Predicate {
	return func(p PlatformProfile) bool { return p.Transport == t }
}

// Has reports whether the profile supports the given feature.
func (p PlatformProfile) Has(f Feature) bool {
	return p.Features.Has(f)
}

// IsTransport reports whether the profile's transport matches t.
func (p PlatformProfile) IsTransport(t TransportKind) bool {
	return p.Transport == t
}

// IsHost reports whether the profile's host type matches h.
func (p PlatformProfile) IsHost(h HostType) bool {
	return p.HostType == h
}

// CloneFeatures returns a shallow copy of this profile with a cloned
// FeatureSet. Callers that overlay runtime flags (e.g. setting
// FeatFileHostInput based on whether a relay handler is wired) MUST use
// this before mutating Features — the FeatureSet in a static profile
// is a shared map.
func (p PlatformProfile) CloneFeatures() PlatformProfile {
	p.Features = p.Features.Clone()
	return p
}

// Shared adapts this CLI profile to the SDK-neutral model.Profile carried
// on model.ToolRequest / model.RequestCaps. It copies every shared field;
// the CLI-only Surface field (which domain surfaces this server exposes) is
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

// fromShared is the inverse of Shared (SharedToModel): it reconstructs a
// CLI PlatformProfile view from the SDK-neutral model profile. Surface is
// zero (meaning "full surface") because the model profile cannot carry it —
// only use this where the consumer is surface-independent (feature/
// transport/host-gated description and schema resolution, which never gate
// on Surface).
func fromShared(sp model.Profile) PlatformProfile {
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

// FromShared exposes fromShared for consumers across package boundaries that
// must adapt an SDK-neutral model.Profile (e.g. a DescFunc receiving the
// shared profile at resolution time) back to the CLI profile view. The
// reconstructed profile has a zero Surface; callers whose gate reads Surface
// must take a CLI PlatformProfile directly instead.
func FromShared(sp model.Profile) PlatformProfile {
	return fromShared(sp)
}
